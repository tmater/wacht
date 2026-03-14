from __future__ import annotations

import json

from client import SmokeError


# Prove explicit logout deletes the current session token without breaking
# normal re-authentication for the same seeded user.
def run(server, mock):
    del mock  # This scenario only exercises the server's auth endpoints.
    server.wait_for_health()

    login = server.request(
        "POST",
        "/api/auth/login",
        payload={"email": server.email, "password": server.password},
        expected_status=(200,),
    )
    token = login.get("token")
    if not token:
        raise SmokeError("expected login to return a token before logout")

    status_before = server.get_status(token)
    identity_before = server.request(
        "GET",
        "/api/auth/me",
        headers=server.auth_headers(token),
        expected_status=(200,),
    )

    server.request(
        "POST",
        "/api/auth/logout",
        headers=server.auth_headers(token),
        expected_status=(204,),
    )

    rejected_routes = {}
    for path in ("/status", "/api/auth/me"):
        body = server.request(
            "GET",
            path,
            headers=server.auth_headers(token),
            expected_status=(401,),
        )
        assert_body(f"logged out token {path}", body, "unauthorized\n")
        rejected_routes[path] = body.strip()

    relogin = server.request(
        "POST",
        "/api/auth/login",
        payload={"email": server.email, "password": server.password},
        expected_status=(200,),
    )
    new_token = relogin.get("token")
    if not new_token:
        raise SmokeError("expected login after logout to return a fresh token")
    if new_token == token:
        raise SmokeError("expected login after logout to return a different session token")

    status_after = server.get_status(new_token)

    print(
        json.dumps(
            {
                "login_email": login["email"],
                "identity_before_logout": identity_before,
                "checks_visible_before_logout": len(status_before.get("checks", [])),
                "rejected_logged_out_routes": rejected_routes,
                "relogin_email": relogin["email"],
                "checks_visible_after_relogin": len(status_after.get("checks", [])),
            },
            indent=2,
        )
    )


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
