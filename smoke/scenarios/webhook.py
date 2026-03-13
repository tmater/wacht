from __future__ import annotations

import json
import time
import uuid

from client import SmokeError, wait_for
from scenarios.quorum import healthy_status, open_incident, resolved_incident

# Prove a real outage triggers one delivered webhook on incident open and one
# more on recovery by posting to the smoke mock's local webhook sink.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    mock.clear_webhooks()
    token = server.login()
    check_id = f"smoke-webhook-{uuid.uuid4().hex[:8]}"
    webhook_url = "http://mock:9090/webhook"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/state",
        "webhook": webhook_url,
        "interval": 1,
    }
    server.create_check(token, payload)

    try:
        wait_for(
            "webhook check to become healthy before the outage",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: healthy_status(server, token, check_id),
        )

        if deliveries_for_check(mock, check_id):
            raise SmokeError(f"expected 0 webhook deliveries before outage for {check_id}")

        mock.set_state("down")

        opened = wait_for(
            "real probes to open an incident for the webhook scenario",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: open_incident(server, token, check_id),
        )

        first_attempts = wait_for(
            "exactly one delivered webhook after the down transition",
            timeout_seconds=30,
            interval_seconds=2,
            fn=lambda: expected_deliveries(mock, check_id, expected=1, statuses=["down"]),
        )

        assert_deliveries_stable(mock, check_id, expected=1, statuses=["down"], seconds=4)

        mock.set_state("up")

        resolved = wait_for(
            "real probes to resolve the incident for the webhook scenario",
            timeout_seconds=60,
            interval_seconds=2,
            fn=lambda: resolved_incident(server, token, check_id),
        )

        second_attempts = wait_for(
            "exactly two delivered webhooks after recovery",
            timeout_seconds=30,
            interval_seconds=2,
            fn=lambda: expected_deliveries(mock, check_id, expected=2, statuses=["down", "up"]),
        )

        assert_deliveries_stable(mock, check_id, expected=2, statuses=["down", "up"], seconds=4)

        print(
            json.dumps(
                {
                    "opened": opened,
                    "resolved": resolved,
                    "attempts_after_down": first_attempts,
                    "attempts_after_recovery": second_attempts,
                },
                indent=2,
            )
        )
    finally:
        mock.set_state("up")
        mock.clear_webhooks()
        server.delete_check_if_present(token, check_id)


def expected_deliveries(mock, check_id, expected, statuses):
    deliveries = deliveries_for_check(mock, check_id)
    if len(deliveries) != expected:
        return None
    if delivery_statuses(deliveries) != statuses:
        return None
    assert_delivery_payloads(deliveries, check_id)
    return deliveries


def assert_deliveries_stable(mock, check_id, expected, statuses, seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        deliveries = deliveries_for_check(mock, check_id)
        if len(deliveries) != expected:
            raise SmokeError(f"expected {expected} webhook deliveries for {check_id}, got {len(deliveries)}")
        if delivery_statuses(deliveries) != statuses:
            raise SmokeError(f"expected webhook statuses {statuses} for {check_id}, got {delivery_statuses(deliveries)}")
        assert_delivery_payloads(deliveries, check_id)
        time.sleep(1)


def deliveries_for_check(mock, check_id):
    return [payload for payload in mock.list_webhooks() if payload.get("check_id") == check_id]


def delivery_statuses(deliveries):
    return [payload.get("status") for payload in deliveries]


def assert_delivery_payloads(deliveries, check_id):
    for payload in deliveries:
        if payload.get("check_id") != check_id:
            raise SmokeError(f"expected webhook check_id {check_id}, got {payload}")
        if payload.get("target") != "http://mock:9090/state":
            raise SmokeError(f"expected webhook target to be the smoke mock state endpoint, got {payload}")
        if payload.get("probes_total") != 3:
            raise SmokeError(f"expected webhook probes_total=3 for {check_id}, got {payload}")
