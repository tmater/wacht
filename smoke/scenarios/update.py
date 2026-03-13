from __future__ import annotations

import json
import urllib.parse
import uuid

from client import SmokeError


# Prove the authenticated update API mutates an existing check in place and the
# canonical definition is returned by GET /api/checks afterwards.
def run(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    check_id = f"smoke-update-{uuid.uuid4().hex[:8]}"
    initial = {
        "id": check_id,
        "type": "http",
        "target": "http://mock:9090/state",
        "interval": 1,
    }
    updated = {
        "id": check_id,
        "type": " TCP ",
        "target": " mock:9090 ",
        "webhook": " http://mock:9090/webhook ",
        "interval": 7,
    }

    server.create_check(token, initial)
    try:
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
                    "initial": initial,
                    "updated": got,
                },
                indent=2,
            )
        )
    finally:
        server.delete_check_if_present(token, check_id)


def assert_check_field(check, field, expected):
    actual = check.get(field)
    if actual != expected:
        raise SmokeError(f"expected updated check {field}={expected!r}, got {actual!r}: {check}")
