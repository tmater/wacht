from __future__ import annotations

import json

from smoke.client import SmokeError


# Prove the seeded smoke user can bootstrap the dashboard identity endpoint.
def test_authenticated_identity(server):
    server.wait_for_health()

    login = server.request(
        "POST",
        "/api/auth/login",
        payload={"email": server.email, "password": server.password},
        expected_status=(200,),
    )
    token = login.get("token")
    if not token:
        raise SmokeError("expected login to return a token for the seeded smoke user")
    if login.get("email") != server.email:
        raise SmokeError(f"expected login email {server.email!r}, got {login.get('email')!r}")

    identity = server.request(
        "GET",
        "/api/auth/me",
        headers=server.auth_headers(token),
        expected_status=(200,),
    )
    if identity.get("email") != server.email:
        raise SmokeError(f"expected /api/auth/me email {server.email!r}, got {identity.get('email')!r}")
    if identity.get("is_admin") is not True:
        raise SmokeError(f"expected seeded smoke user to be admin, got {identity!r}")

    print(
        json.dumps(
            {
                "login_email": login["email"],
                "identity": identity,
            },
            indent=2,
        )
    )
