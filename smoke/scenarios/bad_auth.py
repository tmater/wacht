from __future__ import annotations

import json
import uuid

from client import SmokeError


# Prove one representative auth failure is rejected cleanly and does not mutate
# the stored password or invalidate the existing session.
def run(server, mock):
    del mock  # This scenario only exercises the server's auth endpoints.
    server.wait_for_health()
    token = server.login()
    attempted_password = f"smoke-bad-auth-{uuid.uuid4().hex[:12]}"

    rejected_change = server.request(
        "PUT",
        "/api/auth/change-password",
        payload={
            "current_password": "definitely-the-wrong-password",
            "new_password": attempted_password,
        },
        headers=server.auth_headers(token),
        expected_status=(401,),
    )
    assert_body("wrong current password", rejected_change, "current password is incorrect\n")

    # The rejected mutation should not break the already authenticated session.
    server.get_status(token)

    original_login = server.request(
        "POST",
        "/api/auth/login",
        payload={"email": server.email, "password": server.password},
        expected_status=(200,),
    )
    original_token = original_login.get("token")
    if not original_token:
        raise SmokeError("expected original password login to return a token after rejected password change")
    if original_login.get("email") != server.email:
        raise SmokeError(f"expected login email {server.email!r}, got {original_login.get('email')!r}")

    rejected_login = server.request(
        "POST",
        "/api/auth/login",
        payload={"email": server.email, "password": attempted_password},
        expected_status=(401,),
    )
    assert_body("rejected new password", rejected_login, "invalid credentials\n")

    print(
        json.dumps(
            {
                "change_password_rejection": rejected_change.strip(),
                "original_password_login_email": original_login["email"],
                "rejected_new_password_login": rejected_login.strip(),
            },
            indent=2,
        )
    )


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
