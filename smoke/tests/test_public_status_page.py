from __future__ import annotations

import json
import uuid

from smoke.client import SmokeError, wait_for
from smoke.support.cleanup import CleanupScope


def test_public_status_page(server, mock):
    server.wait_for_health()
    mock.set_state("up")
    token = server.login()
    me = server.get_me(token)
    slug = me.get("public_status_slug")
    if not slug:
        raise SmokeError("expected /api/auth/me to return public_status_slug")

    check_name = f"smoke-public-{uuid.uuid4().hex[:8]}"
    payload = {
        "name": check_name,
        "type": "http",
        "target": "http://mock:9090/http/state",
        "interval": 1,
    }
    cleanup = CleanupScope()
    server.create_check(token, payload)

    try:
        with cleanup.preserve_primary_error():
            pending = wait_for(
                "new check to appear on the public status page as pending or up",
                timeout_seconds=30,
                interval_seconds=2,
                fn=lambda: public_status_for_check(server, slug, check_name, allowed_statuses={"pending", "up"}),
            )

            healthy = wait_for(
                "public status page to show the check as up",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: public_status_for_check(server, slug, check_name, allowed_statuses={"up"}),
            )

            mock.set_state("down")

            opened = wait_for(
                "public status page to show the outage",
                timeout_seconds=60,
                interval_seconds=2,
                fn=lambda: public_status_for_check(server, slug, check_name, allowed_statuses={"down"}, require_incident_since=True),
            )

            print(json.dumps({"pending": pending, "healthy": healthy, "opened": opened}, indent=2))
    finally:
        cleanup.run("restore mock HTTP state", lambda: mock.set_state("up"))
        cleanup.run(f"delete check {check_name}", lambda: server.delete_check_if_present(token, check_name))
        cleanup.finish()


def public_status_for_check(server, slug, check_name, *, allowed_statuses, require_incident_since=False):
    payload = server.get_public_status(slug)
    checks = {check["check_name"]: check for check in payload.get("checks", [])}
    check = checks.get(check_name)
    if check is None:
        return None
    if check.get("status") not in allowed_statuses:
        return None
    if require_incident_since and check.get("incident_since") is None:
        return None
    return check
