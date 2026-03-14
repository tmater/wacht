from __future__ import annotations

import json
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, open_incident, resolved_incident
from smoke.support.webhook import assert_deliveries_stable, deliveries_for_check, expected_deliveries

# Prove a real outage triggers one delivered webhook on incident open and one
# more on recovery by posting to the smoke mock's local webhook sink.
def test_webhook(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    mock.clear_webhooks()
    token = server.login()
    check_id = f"smoke-webhook-{uuid.uuid4().hex[:8]}"
    webhook_url = "http://mock:9090/webhook"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "webhook": webhook_url,
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
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
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run("clear mock webhooks", mock.clear_webhooks)
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()
