from __future__ import annotations

import json
import time
import uuid

from client import SmokeError, wait_for


# Prove a real outage observed by the real probes opens exactly one incident,
# and that recovery resolves that same incident again.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-quorum-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/state",
        "interval": 1,
    }
    server.create_check(token, payload)

    try:
        wait_for(
            "quorum check to become healthy before the outage",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: healthy_status(server, token, check_id),
        )

        mock.set_state("down")

        opened = wait_for(
            "real probes to open an incident after the target goes down",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident(server, token, check_id),
        )

        # Keep the target down briefly to catch duplicate incident rows while
        # the outage is still ongoing.
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            incidents = incidents_for_check(server, token, check_id)
            if len(incidents) != 1:
                raise SmokeError(f"expected exactly 1 open incident during outage, got {len(incidents)}")
            if incidents[0].get("resolved_at") is not None:
                raise SmokeError("incident resolved while the target was still down")
            time.sleep(1)

        mock.set_state("up")

        resolved = wait_for(
            "real probes to resolve the incident after recovery",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident(server, token, check_id),
        )

        print(json.dumps({"opened": opened, "resolved": resolved}, indent=2))
    finally:
        mock.set_state("up")
        server.delete_check_if_present(token, check_id)


def healthy_status(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "up":
        return None
    if status.get("incident_since") is not None:
        return None
    return status


def open_incident(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "down":
        return None
    if status.get("incident_since") is None:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is not None:
        return None
    return {"status": status, "incidents": incidents}


def resolved_incident(server, token, check_id):
    status = status_for_check(server, token, check_id)
    if status is None:
        return None
    if status.get("status") != "up":
        return None
    if status.get("incident_since") is not None:
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None
    if incidents[0].get("resolved_at") is None:
        return None
    return {"status": status, "incidents": incidents}


def status_for_check(server, token, check_id):
    checks = server.get_status(token).get("checks", [])
    return next((check for check in checks if check.get("check_id") == check_id), None)


def incidents_for_check(server, token, check_id):
    return [incident for incident in server.list_incidents(token) if incident.get("check_id") == check_id]
