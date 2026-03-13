from __future__ import annotations

import json
import uuid

from client import SmokeClient, SmokeError


# Prove a real authenticated user can change their password, that the old
# credentials stop working immediately, and that the new credentials do work.
# Restore the original seeded password before returning so later scenarios can
# still reuse the same smoke user.
def run(server, mock):
    del mock  # This scenario only exercises the server's auth endpoints.
    server.wait_for_health()
    token = server.login()
    original_password = server.password
    updated_password = f"smoke-password-change-{uuid.uuid4().hex[:12]}"
    updated_user = SmokeClient(
        base_url=server.base_url,
        email=server.email,
        password=updated_password,
        timeout_seconds=server.timeout_seconds,
    )
    password_changed = False
    restored = False

    try:
        server.request(
            "PUT",
            "/api/auth/change-password",
            payload={
                "current_password": original_password,
                "new_password": updated_password,
            },
            headers=server.auth_headers(token),
            expected_status=(204,),
        )
        password_changed = True

        rejected_old_login = server.request(
            "POST",
            "/api/auth/login",
            payload={"email": server.email, "password": original_password},
            expected_status=(401,),
        )
        assert_body("old password login after change", rejected_old_login, "invalid credentials\n")

        updated_login = updated_user.request(
            "POST",
            "/api/auth/login",
            payload={"email": updated_user.email, "password": updated_password},
            expected_status=(200,),
        )
        updated_token = updated_login.get("token")
        if not updated_token:
            raise SmokeError("expected updated password login to return a token")
        if updated_login.get("email") != server.email:
            raise SmokeError(f"expected updated login email {server.email!r}, got {updated_login.get('email')!r}")

        identity = updated_user.request(
            "GET",
            "/api/auth/me",
            headers=updated_user.auth_headers(updated_token),
            expected_status=(200,),
        )
        if identity.get("email") != server.email:
            raise SmokeError(f"expected /api/auth/me email {server.email!r}, got {identity.get('email')!r}")

        updated_user.request(
            "PUT",
            "/api/auth/change-password",
            payload={
                "current_password": updated_password,
                "new_password": original_password,
            },
            headers=updated_user.auth_headers(updated_token),
            expected_status=(204,),
        )
        restored = True

        rejected_updated_login = updated_user.request(
            "POST",
            "/api/auth/login",
            payload={"email": updated_user.email, "password": updated_password},
            expected_status=(401,),
        )
        assert_body("updated password login after restore", rejected_updated_login, "invalid credentials\n")

        restored_login = server.request(
            "POST",
            "/api/auth/login",
            payload={"email": server.email, "password": original_password},
            expected_status=(200,),
        )
        restored_token = restored_login.get("token")
        if not restored_token:
            raise SmokeError("expected restored original password login to return a token")

        print(
            json.dumps(
                {
                    "updated_password_login_email": updated_login["email"],
                    "identity": identity,
                    "rejected_old_password_login": rejected_old_login.strip(),
                    "rejected_updated_password_login_after_restore": rejected_updated_login.strip(),
                    "restored_password_login_email": restored_login["email"],
                },
                indent=2,
            )
        )
    finally:
        if password_changed and not restored:
            try:
                server.request(
                    "PUT",
                    "/api/auth/change-password",
                    payload={
                        "current_password": updated_password,
                        "new_password": original_password,
                    },
                    headers=server.auth_headers(token),
                    expected_status=(204,),
                )
            except Exception as exc:  # noqa: BLE001 - keep the primary smoke failure.
                print(f"[cleanup] failed to restore seeded password: {exc}")


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
