from __future__ import annotations

import json
import urllib.parse
import uuid

from smoke.client import SmokeError
from smoke.support.cleanup import CleanupScope


# Prove the authenticated check management API supports the full
# create-read-update-delete lifecycle against the packaged stack.
def test_crud(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()

    # Use a unique ID so reruns against a reused stack do not collide.
    check_id = f"smoke-crud-{uuid.uuid4().hex[:8]}"
    payload = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "interval": 1,
    }
    updated = {
        "id": check_id,
        "type": " TCP ",
        "target": " mock:9090 ",
        "webhook": " http://mock:9090/webhook ",
        "interval": 7,
    }
    cleanup = CleanupScope()

    server.create_check(token, payload)
    try:
        with cleanup.preserve_primary_error():
            checks = server.list_checks(token)

            created = next((check for check in checks if check.get("id") == check_id), None)
            if created is None:
                raise SmokeError(f"created check {check_id} not returned by GET /api/checks")
            if created.get("type") != "http":
                raise SmokeError(f"created check {check_id} has unexpected type {created.get('type')!r}")
            if created.get("target") != payload["target"]:
                raise SmokeError(f"created check {check_id} has unexpected target {created.get('target')!r}")

            encoded = urllib.parse.quote(check_id, safe="")
            server.request(
                "PUT",
                f"/api/checks/{encoded}",
                payload=updated,
                headers=server.auth_headers(token),
                expected_status=(204,),
            )

            checks = server.list_checks(token)
            matches = [check for check in checks if check.get("id") == check_id]
            if len(matches) != 1:
                raise SmokeError(f"expected exactly 1 updated check row for {check_id}, got {len(matches)}")

            got = matches[0]
            assert_check_field(got, "type", "tcp")
            assert_check_field(got, "target", "mock:9090")
            assert_check_field(got, "webhook", "http://mock:9090/webhook")
            assert_check_field(got, "interval", 7)

            print(
                json.dumps(
                    {
                        "check_id": check_id,
                        "created": payload,
                        "updated": got,
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run(f"delete check {check_id}", lambda: server.delete_check_if_present(token, check_id))
        cleanup.finish()

    checks = server.list_checks(token)
    if any(check.get("id") == check_id for check in checks):
        raise SmokeError(f"deleted check {check_id} is still returned by GET /api/checks")


def assert_check_field(check, field, expected):
    actual = check.get(field)
    if actual != expected:
        raise SmokeError(f"expected updated check {field}={expected!r}, got {actual!r}: {check}")
