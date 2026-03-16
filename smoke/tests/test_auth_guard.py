from __future__ import annotations

import json
import uuid

from smoke.client import SmokeClient, SmokeError


# Prove the session guard rejects missing bearer tokens and the admin guard
# rejects a real non-admin session.
def test_auth_guard(server):
    server.wait_for_health()

    missing_token_routes = ("/status", "/api/checks", "/api/auth/me")
    missing_token_bodies = {}
    for path in missing_token_routes:
        body = server.request("GET", path, expected_status=(401,))
        assert_body(f"missing token {path}", body, "unauthorized\n")
        missing_token_bodies[path] = body.strip()

    admin_token = server.login()
    email = f"smoke-auth-guard-{uuid.uuid4().hex[:12]}@wacht.local"
    chosen_password = f"smoke-setup-{uuid.uuid4().hex[:12]}"

    server.request_access(email)
    pending = server.list_signup_requests(admin_token)
    request_id = pending_request_id(pending, email)
    if request_id is None:
        raise SmokeError(f"expected pending signup request for {email}, got {pending!r}")

    approval = server.approve_signup_request(admin_token, request_id)
    if approval.get("email") != email:
        raise SmokeError(f"expected approved signup email {email!r}, got {approval.get('email')!r}")
    setup_token = approval.get("setup_token")
    if not setup_token:
        raise SmokeError(f"expected setup_token in signup approval response, got {approval!r}")

    normal_user = SmokeClient(
        base_url=server.base_url,
        email=email,
        password=chosen_password,
        timeout_seconds=server.timeout_seconds,
    )
    setup = normal_user.setup_password(setup_token, chosen_password)
    setup_token_value = setup.get("token")
    if not setup_token_value:
        raise SmokeError(f"expected setup-password token in response, got {setup!r}")

    identity = normal_user.request(
        "GET",
        "/api/auth/me",
        headers=normal_user.auth_headers(setup_token_value),
        expected_status=(200,),
    )
    normal_token = normal_user.login()
    identity = normal_user.request(
        "GET",
        "/api/auth/me",
        headers=normal_user.auth_headers(normal_token),
        expected_status=(200,),
    )
    if identity.get("email") != email:
        raise SmokeError(f"expected /api/auth/me email {email!r}, got {identity.get('email')!r}")
    if identity.get("is_admin") is not False:
        raise SmokeError(f"expected approved signup user to be non-admin, got {identity!r}")

    forbidden = normal_user.request(
        "GET",
        "/api/admin/signup-requests",
        headers=normal_user.auth_headers(normal_token),
        expected_status=(403,),
    )
    assert_body("non-admin admin route", forbidden, "forbidden\n")

    print(
        json.dumps(
            {
                "missing_token_routes": missing_token_bodies,
                "setup_password_email": setup.get("email"),
                "approved_user": identity,
                "non_admin_admin_route": forbidden.strip(),
            },
            indent=2,
        )
    )


def pending_request_id(requests, email):
    for request in requests or []:
        if request.get("email") == email:
            return request.get("id")
    return None


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
