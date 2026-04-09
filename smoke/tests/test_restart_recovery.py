from __future__ import annotations

import json
import time
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import incidents_for_check, status_for_check


PROBE_IDS = ("probe-1", "probe-2", "probe-3")
BOOT_DOWNTIME_SECONDS = 12


# Prove an already-open incident survives a server restart even when probes are
# stopped before the restart, so the recovered runtime cannot rely on fresh
# post-boot probe writes to reconstruct the down state. The authenticated and
# public status views must stay aligned with the unresolved incident row.
def test_restart_recovery_preserves_open_incident(server, mock, probes, stack):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    slug = public_status_slug(server, token)
    check_id = f"smoke-restart-recovery-{uuid.uuid4().hex[:8]}"
    cleanup = CleanupScope()

    server.create_check(
        token,
        {
            "id": check_id,
            "type": "http",
            "target": "http://mock:9090/http/state",
            "interval": 1,
        },
    )

    try:
        with cleanup.preserve_primary_error():
            healthy = wait_for(
                "restart-recovery check to become healthy on authenticated and public status views",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_everywhere(server, token, slug, check_id),
            )

            mock.set_state("down")

            opened = wait_for(
                "restart-recovery outage to open one incident across authenticated and public status views",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident_everywhere(server, token, slug, check_id),
            )

            for probe_id in PROBE_IDS:
                probes.stop(probe_id)

            stack.stop_service("server")
            stack.start_service("server")
            server.wait_for_health()
            token = server.login()

            recovered = wait_for(
                "restarted server to keep the incident visible without fresh probe writes",
                timeout_seconds=20,
                interval_seconds=2,
                fn=lambda: unresolved_incident_everywhere(server, token, slug, check_id),
            )

            print(json.dumps({"healthy": healthy, "opened": opened, "recovered": recovered}, indent=2))
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run("restart stopped probes", lambda: restore_stopped_probes(probes))
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()


# Prove a long enough server downtime is reconciled immediately at boot: stale
# probe heartbeats must come back as offline, stale check evidence must no
# longer count, and both status surfaces must show the degraded runtime before
# any fresh probe writes can arrive.
def test_restart_recovery_expires_stale_runtime_on_boot(server, mock, probes, stack):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    slug = public_status_slug(server, token)
    check_id = f"smoke-restart-expiry-{uuid.uuid4().hex[:8]}"
    cleanup = CleanupScope()

    server.create_check(
        token,
        {
            "id": check_id,
            "type": "http",
            "target": "http://mock:9090/http/state",
            "interval": 1,
        },
    )

    try:
        with cleanup.preserve_primary_error():
            healthy = wait_for(
                "boot-expiry check to become healthy on authenticated and public status views",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_everywhere(server, token, slug, check_id),
            )

            stack.stop_service("server")
            for probe_id in PROBE_IDS:
                probes.stop(probe_id)

            time.sleep(BOOT_DOWNTIME_SECONDS)

            stack.start_service("server")
            server.wait_for_health()
            token = server.login()

            expired = wait_for(
                "boot sweeps to expire stale probes and check evidence before any fresh probe writes",
                timeout_seconds=20,
                interval_seconds=2,
                fn=lambda: expired_runtime_everywhere(server, token, slug, check_id),
            )

            print(json.dumps({"healthy": healthy, "expired": expired}, indent=2))
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run("restart stopped probes", lambda: restore_stopped_probes(probes))
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()


def public_status_slug(server, token):
    me = server.get_me(token)
    slug = me.get("public_status_slug")
    if not slug:
        raise SmokeError("expected /api/auth/me to return public_status_slug")
    return slug


def healthy_everywhere(server, token, slug, check_id):
    authenticated = status_for_check(server, token, check_id)
    if authenticated is None:
        return None
    if authenticated.get("status") != "up":
        return None
    if authenticated.get("incident_since") is not None:
        return None

    public = public_status_for_check(server, slug, check_id)
    if public is None:
        return None
    if public.get("status") != "up":
        return None
    if public.get("incident_since") is not None:
        return None

    return {"status": authenticated, "public": public}


def open_incident_everywhere(server, token, slug, check_id):
    authenticated = status_for_check(server, token, check_id)
    if authenticated is None:
        return None
    if authenticated.get("status") != "down":
        return None
    incident_since = authenticated.get("incident_since")
    if incident_since is None:
        return None

    public = public_status_for_check(server, slug, check_id)
    if public is None:
        return None
    if public.get("status") != "down":
        return None
    if public.get("incident_since") != incident_since:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    incident = incidents[0]
    if incident.get("resolved_at") is not None:
        return None
    if incident.get("started_at") != incident_since:
        return None

    return {"status": authenticated, "public": public, "incidents": incidents}


def unresolved_incident_everywhere(server, token, slug, check_id):
    authenticated = status_for_check(server, token, check_id)
    if authenticated is None:
        return None
    if authenticated.get("status") not in {"down", "error"}:
        return None
    incident_since = authenticated.get("incident_since")
    if incident_since is None:
        return None

    public = public_status_for_check(server, slug, check_id)
    if public is None:
        return None
    if public.get("status") != authenticated.get("status"):
        return None
    if public.get("incident_since") != incident_since:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    incident = incidents[0]
    if incident.get("resolved_at") is not None:
        return None
    if incident.get("started_at") != incident_since:
        return None

    return {"status": authenticated, "public": public, "incidents": incidents}


def expired_runtime_everywhere(server, token, slug, check_id):
    authenticated = status_for_check(server, token, check_id)
    if authenticated is None:
        return None
    if authenticated.get("status") != "error":
        return None
    if authenticated.get("incident_since") is not None:
        return None

    public = public_status_for_check(server, slug, check_id)
    if public is None:
        return None
    if public.get("status") != "error":
        return None
    if public.get("incident_since") is not None:
        return None

    probes = probes_by_id(server, token)
    for probe_id in PROBE_IDS:
        probe = probes.get(probe_id)
        if probe is None:
            return None
        if probe.get("status") != "offline":
            return None
        if probe.get("online", True):
            return None
        if probe.get("last_seen_at") is None:
            return None

    incidents = incidents_for_check(server, token, check_id)
    if incidents:
        return None

    return {
        "status": authenticated,
        "public": public,
        "probes": [probes[probe_id] for probe_id in PROBE_IDS],
    }


def public_status_for_check(server, slug, check_id):
    checks = server.get_public_status(slug).get("checks", [])
    return next((check for check in checks if check.get("check_id") == check_id), None)


def probes_by_id(server, token):
    probes = server.get_status(token).get("probes", [])
    return {probe["probe_id"]: probe for probe in probes}


def restore_stopped_probes(probes):
    for probe_id in sorted(probes.stopped.copy()):
        probes.start(probe_id)
        probes.wait_until_online(probe_id)
