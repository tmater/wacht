from __future__ import annotations

import uuid

from client import SmokeError


# Prove the authenticated check management API works against the packaged stack.
def run(client):
    client.wait_for_health()
    token = client.login()

    # Use a unique ID so reruns against a reused stack do not collide.
    check_id = f"smoke-crud-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/up",
        "interval": 1,
    }

    client.create_check(token, payload)
    try:
        checks = client.list_checks(token)

        created = next((check for check in checks if check.get("id") == check_id), None)
        if created is None:
            raise SmokeError(f"created check {check_id} not returned by GET /api/checks")
        if created.get("type") != "http":
            raise SmokeError(f"created check {check_id} has unexpected type {created.get('type')!r}")
        if created.get("target") != payload["target"]:
            raise SmokeError(f"created check {check_id} has unexpected target {created.get('target')!r}")
    finally:
        client.delete_check_if_present(token, check_id)

    checks = client.list_checks(token)
    if any(check.get("id") == check_id for check in checks):
        raise SmokeError(f"deleted check {check_id} is still returned by GET /api/checks")
