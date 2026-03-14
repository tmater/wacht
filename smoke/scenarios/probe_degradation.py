from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import uuid
from pathlib import Path

from client import SmokeError, wait_for


SMOKE_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = SMOKE_DIR.parent
DEGRADED_PROBE_ID = "probe-3"
ONLINE_PROBE_IDS = ("probe-1", "probe-2")


# Prove one missing probe shows up as offline in /status while the remaining
# two probes still form a majority that can open and resolve one incident.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-probe-degradation-{uuid.uuid4().hex[:8]}"
    probe_stopped = False

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
        wait_for(
            "probe degradation check to become healthy with all three probes online",
            timeout_seconds=90,
            interval_seconds=3,
            fn=lambda: healthy_snapshot(server, token, check_id),
        )

        stop_probe_service(DEGRADED_PROBE_ID)
        probe_stopped = True

        degraded = wait_for(
            "probe-3 to show offline in /status while the check stays healthy",
            timeout_seconds=140,
            interval_seconds=5,
            fn=lambda: degraded_snapshot(server, token, check_id),
        )

        mock.set_state("down")

        opened = wait_for(
            "the remaining two probes to open one incident during the outage",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident_with_degraded_probe(server, token, check_id),
        )

        assert_incident_stable(server, token, check_id, seconds=4)

        mock.set_state("up")

        resolved = wait_for(
            "the remaining two probes to resolve the incident after recovery",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident_with_degraded_probe(server, token, check_id),
        )

        print(
            json.dumps(
                {
                    "check_id": check_id,
                    "degraded": degraded,
                    "opened": opened,
                    "resolved": resolved,
                },
                indent=2,
            )
        )
    finally:
        cleanup_error = None
        mock.set_state("up")

        if probe_stopped:
            try:
                start_probe_service(DEGRADED_PROBE_ID)
                wait_for_probe_online(server, token, DEGRADED_PROBE_ID)
            except SmokeError as exc:
                cleanup_error = exc

        try:
            server.delete_check_if_present(token, check_id)
        except SmokeError as exc:
            if cleanup_error is None:
                cleanup_error = exc
            else:
                print(f"[cleanup] {exc}", file=sys.stderr)

        if cleanup_error is not None:
            if sys.exc_info()[0] is not None:
                print(f"[cleanup] {cleanup_error}", file=sys.stderr)
            else:
                raise cleanup_error


def healthy_snapshot(server, token, check_id):
    snapshot = status_snapshot(server, token, check_id)
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


def degraded_snapshot(server, token, check_id):
    snapshot = status_snapshot(server, token, check_id)
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


def open_incident_with_degraded_probe(server, token, check_id):
    snapshot = status_snapshot(server, token, check_id)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "down":
        return None
    if check.get("incident_since") is None:
        return None
    if not degraded_probe_visible(snapshot["probes"]):
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is not None:
        return None

    return {"status": check, "probes": list(snapshot["probes"].values()), "incidents": incidents}


def resolved_incident_with_degraded_probe(server, token, check_id):
    snapshot = status_snapshot(server, token, check_id)
    check = snapshot["check"]
    if check is None:
        return None
    if check.get("status") != "up":
        return None
    if "incident_since" in check:
        return None
    if not degraded_probe_visible(snapshot["probes"]):
        return None

    incidents = incidents_for_check(server, token, check_id)
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


def status_snapshot(server, token, check_id):
    status = server.get_status(token)
    checks = {check["check_id"]: check for check in status.get("checks", [])}
    probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}
    return {"check": checks.get(check_id), "probes": probes}


def incidents_for_check(server, token, check_id):
    return [incident for incident in server.list_incidents(token) if incident.get("check_id") == check_id]


def assert_incident_stable(server, token, check_id, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        opened = open_incident_with_degraded_probe(server, token, check_id)
        if opened is None:
            raise SmokeError(f"expected exactly 1 open incident for degraded-probe check {check_id} while down")
        time.sleep(1)


def stop_probe_service(probe_id):
    compose("stop", probe_id)


def start_probe_service(probe_id):
    compose("start", probe_id)


def wait_for_probe_online(server, token, probe_id):
    wait_for(
        f"{probe_id} to come back online in /status",
        timeout_seconds=90,
        interval_seconds=3,
        fn=lambda: probe_online_snapshot(server, token, probe_id),
    )


def probe_online_snapshot(server, token, probe_id):
    status = server.get_status(token)
    probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}
    probe = probes.get(probe_id)
    if probe is None:
        return None
    if not probe.get("online", False):
        return None
    return probe


def compose(*args):
    compose_file = os.environ.get("SMOKE_COMPOSE_FILE", str(SMOKE_DIR / "fixtures" / "docker-compose.yml"))
    project_name = os.environ.get("SMOKE_COMPOSE_PROJECT", "wacht-smoke")
    docker = os.environ.get("DOCKER", "docker")
    cmd = [docker, "compose", "-f", compose_file, *args]
    env = os.environ.copy()
    env["COMPOSE_PROJECT_NAME"] = project_name
    print(f"[probe-control] {' '.join(cmd)}")
    try:
        subprocess.run(cmd, cwd=REPO_ROOT, env=env, check=True)
    except subprocess.CalledProcessError as exc:
        raise SmokeError(f"docker compose {' '.join(args)} failed with exit code {exc.returncode}") from exc
