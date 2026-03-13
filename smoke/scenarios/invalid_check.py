from __future__ import annotations

import json
import uuid

from client import SmokeError


# Prove representative validation failures are rejected before they can create
# persisted checks.
def run(server, mock):
    del mock  # This scenario only exercises the server's check validation API.
    server.wait_for_health()
    token = server.login()

    invalid_target_id = f"smoke-invalid-target-{uuid.uuid4().hex[:8]}"
    invalid_webhook_id = f"smoke-invalid-webhook-{uuid.uuid4().hex[:8]}"

    invalid_target = server.request(
        "POST",
        "/api/checks",
        payload={
            "id": invalid_target_id,
            "type": "http",
            "target": "http:///missing-host",
            "interval": 1,
        },
        headers=server.auth_headers(token),
        expected_status=(400,),
    )
    assert_body("invalid target", invalid_target, "http target: host is required\n")

    invalid_webhook = server.request(
        "POST",
        "/api/checks",
        payload={
            "id": invalid_webhook_id,
            "type": "http",
            "target": "http://mock:9090/http/state",
            "webhook": "http://user:pass@example.com/webhook",
            "interval": 1,
        },
        headers=server.auth_headers(token),
        expected_status=(400,),
    )
    assert_body("invalid webhook", invalid_webhook, "webhook: userinfo is not allowed\n")

    checks = server.list_checks(token)
    created = [check for check in checks if check.get("id") in {invalid_target_id, invalid_webhook_id}]
    if created:
        raise SmokeError(f"rejected checks should not be persisted, but GET /api/checks returned {created!r}")

    print(
        json.dumps(
            {
                "invalid_target": {
                    "check_id": invalid_target_id,
                    "response": invalid_target.strip(),
                },
                "invalid_webhook": {
                    "check_id": invalid_webhook_id,
                    "response": invalid_webhook.strip(),
                },
                "persisted_matches": 0,
            },
            indent=2,
        )
    )


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
