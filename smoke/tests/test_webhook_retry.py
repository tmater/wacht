from __future__ import annotations

import json
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status, incidents_for_check, open_incident
from smoke.support.webhook import assert_deliveries_stable, deliveries_for_check


# Prove a transient webhook failure is retried by the real sender until the
# "down" alert is eventually delivered and reflected in the incidents API.
def test_webhook_retry(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    mock.clear_webhooks()
    mock.configure_webhook(fail_next=1)
    token = server.login()
    check_id = f"smoke-webhook-retry-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "webhook": "http://mock:9090/webhook",
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
            wait_for(
                "webhook retry check to become healthy before the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: healthy_status(server, token, check_id),
            )

            if deliveries_for_check(mock, check_id):
                raise SmokeError(f"expected 0 webhook deliveries before outage for {check_id}")

            mock.set_state("down")

            opened = wait_for(
                "real probes to open an incident for the webhook retry scenario",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: open_incident(server, token, check_id),
            )

            delivered = wait_for(
                "webhook retry delivery to succeed after one forced failure",
                timeout_seconds=45,
                interval_seconds=2,
                fn=lambda: delivered_down_notification(server, mock, token, check_id),
            )

            assert_deliveries_stable(mock, check_id, expected=1, statuses=["down"], seconds=4)

            print(
                json.dumps(
                    {
                        "opened": opened,
                        "delivered": delivered,
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run("reset webhook control", lambda: mock.configure_webhook(fail_next=0))
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run("clear mock webhooks", mock.clear_webhooks)
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()


def delivered_down_notification(server, mock, token, check_id):
    deliveries = deliveries_for_check(mock, check_id)
    if len(deliveries) != 1:
        return None
    if deliveries[0].get("status") != "down":
        return None

    incidents = incidents_for_check(server, token, check_id)
    if len(incidents) != 1:
        return None

    down_notification = incidents[0].get("down_notification")
    if not isinstance(down_notification, dict):
        return None
    if down_notification.get("state") != "delivered":
        return None

    attempts = down_notification.get("attempts")
    if not isinstance(attempts, int) or attempts < 2:
        return None

    return {
        "deliveries": deliveries,
        "down_notification": down_notification,
    }
