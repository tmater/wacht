from __future__ import annotations

import json
import time
import uuid

from client import SmokeError, wait_for
from scenarios.quorum import incidents_for_check, status_for_check
from scenarios.webhook import deliveries_for_check


OBSERVATION_SECONDS = 12


# Prove a flapping target does not open an incident or fire a webhook while it
# oscillates during an observation window.
def run(server, mock):
    server.wait_for_health()
    mock.clear_webhooks()
    token = server.login()
    check_id = f"smoke-flapping-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/flap",
        "webhook": "http://mock:9090/webhook",
        "interval": 1,
    }
    server.create_check(token, payload)

    try:
        wait_for(
            "flapping check to receive its first probe result",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: status_for_check(server, token, check_id),
        )

        snapshot = assert_no_alerts_during_flapping(server, mock, token, check_id, seconds=OBSERVATION_SECONDS)

        print(json.dumps(snapshot, indent=2))
    finally:
        mock.clear_webhooks()
        server.delete_check_if_present(token, check_id)


def assert_no_alerts_during_flapping(server, mock, token, check_id, seconds):
    deadline = time.monotonic() + seconds
    status_checks = 0

    while time.monotonic() < deadline:
        status_checks += 1
        status = status_for_check(server, token, check_id)
        if status is None:
            raise SmokeError(f"flapping check {check_id} disappeared from /status")
        if status.get("status") != "up":
            raise SmokeError(f"expected flapping check {check_id} to remain up, got {status}")
        if status.get("incident_since") is not None:
            raise SmokeError(f"expected no open incident for flapping check {check_id}, got {status}")

        incidents = incidents_for_check(server, token, check_id)
        if incidents:
            raise SmokeError(f"expected no incident rows for flapping check {check_id}, got {incidents}")

        deliveries = deliveries_for_check(mock, check_id)
        if deliveries:
            raise SmokeError(f"expected 0 webhook deliveries for flapping check {check_id}, got {deliveries}")

        time.sleep(1)

    return {
        "check_id": check_id,
        "observation_seconds": seconds,
        "status_checks": status_checks,
        "webhook_deliveries": 0,
        "incidents": 0,
    }
