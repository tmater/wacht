from __future__ import annotations

import json
import time
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import incidents_for_check


DEGRADED_PROBE_ID = "probe-3"
ONLINE_PROBE_IDS = ("probe-1", "probe-2")


# Prove one missing probe shows up as offline in /status while the remaining
# two probes still form a majority that can open and resolve one incident.
def test_probe_degradation(server, mock, probes):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_name = f"smoke-probe-degradation-{uuid.uuid4().hex[:8]}"
    probe_stopped = False
    cleanup = CleanupScope()

    server.create_check(
        token,
        {
            "name": check_name,
            "type": "http",
            "target": "http://mock:9090/http/state",
            "interval": 1,
        },
    )

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "probe degradation check to become healthy with all three probes online",
                timeout_seconds=90,
                interval_seconds=3,
                fn=lambda: healthy_snapshot(server, token, check_name),
            )

            probes.stop(DEGRADED_PROBE_ID)
            probe_stopped = True

            degraded = wait_for(
                "probe-3 to show offline in /status while the check stays healthy",
                timeout_seconds=140,
                interval_seconds=5,
                fn=lambda: degraded_snapshot(server, token, check_name),
            )

            mock.set_state("down")

            opened = wait_for(
                "the remaining two probes to open one incident during the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident_with_degraded_probe(server, token, check_name),
            )

            assert_incident_stable(server, token, check_name, seconds=4)

            mock.set_state("up")

            resolved = wait_for(
                "the remaining two probes to resolve the incident after recovery",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: resolved_incident_with_degraded_probe(server, token, check_name),
            )

            print(
                json.dumps(
                    {
                        "check_name": check_name,
                        "degraded": degraded,
                        "opened": opened,
                        "resolved": resolved,
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        if probe_stopped:
            cleanup.run(f"restart {DEGRADED_PROBE_ID}", lambda: restore_probe(probes, DEGRADED_PROBE_ID))
        cleanup.run(f"delete check {check_name}", lambda: server.delete_check_if_present(token, check_name))
        cleanup.finish()


def healthy_snapshot(server, token, check_name):
    snapshot = status_snapshot(server, token, check_name)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "up":
        return None
    if "incident_since" in check:
        return None

    for probe_id in (*ONLINE_PROBE_IDS, DEGRADED_PROBE_ID):
        probe = snapshot["probes"].get(probe_id)
        if probe is None:
            return None
        if not probe.get("online", False):
            return None

    return snapshot


def degraded_snapshot(server, token, check_name):
    snapshot = status_snapshot(server, token, check_name)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "up":
        return None
    if "incident_since" in check:
        return None
    if not degraded_probe_visible(snapshot["probes"]):
        return None
    return snapshot


def open_incident_with_degraded_probe(server, token, check_name):
    snapshot = status_snapshot(server, token, check_name)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "down":
        return None
    if check.get("incident_since") is None:
        return None
    if not degraded_probe_visible(snapshot["probes"]):
        return None

    incidents = incidents_for_check(server, token, check_name)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is not None:
        return None

    return {"status": check, "probes": list(snapshot["probes"].values()), "incidents": incidents}


def resolved_incident_with_degraded_probe(server, token, check_name):
    snapshot = status_snapshot(server, token, check_name)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "up":
        return None
    if "incident_since" in check:
        return None
    if not degraded_probe_visible(snapshot["probes"]):
        return None

    incidents = incidents_for_check(server, token, check_name)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is None:
        return None

    return {"status": check, "probes": list(snapshot["probes"].values()), "incidents": incidents}


def degraded_probe_visible(probes):
    degraded = probes.get(DEGRADED_PROBE_ID)
    if degraded is None:
        return False
    if degraded.get("online", True):
        return False
    if degraded.get("last_seen_at") is None:
        return False

    for probe_id in ONLINE_PROBE_IDS:
        probe = probes.get(probe_id)
        if probe is None:
            return False
        if not probe.get("online", False):
            return False

    return True


def status_snapshot(server, token, check_name):
    status = server.get_status(token)
    checks = {check["check_name"]: check for check in status.get("checks", [])}
    probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}
    return {"check": checks.get(check_name), "probes": probes}


def assert_incident_stable(server, token, check_name, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        opened = open_incident_with_degraded_probe(server, token, check_name)
        if opened is None:
            raise SmokeError(f"expected exactly 1 open incident for degraded-probe check {check_name} while down")
        time.sleep(1)


def restore_probe(probes, probe_id):
    probes.start(probe_id)
    probes.wait_until_online(probe_id)
