from __future__ import annotations

import json
import sys
import uuid

from client import SmokeClient, SmokeError


# Prove the public signup request path feeds the admin approval queue, that an
# approved request turns into a usable non-admin account, and that rejection
# removes a pending request cleanly.
def run(server, mock):
    del mock  # This scenario only exercises the server's auth endpoints.
    server.wait_for_health()
    admin_token = server.login()
    approved_email = f"smoke-signup-approved-{uuid.uuid4().hex[:12]}@wacht.local"
    rejected_email = f"smoke-signup-rejected-{uuid.uuid4().hex[:12]}@wacht.local"
    tracked_emails = {approved_email, rejected_email}

    try:
        for email in (approved_email, rejected_email):
            server.request(
                "POST",
                "/api/auth/request-access",
                payload={"email": email},
                expected_status=(200,),
            )

        pending_before = list_pending_requests(server, admin_token)
        approved_request = require_pending_request(pending_before, approved_email)
        rejected_request = require_pending_request(pending_before, rejected_email)

        approval = server.request(
            "POST",
            f"/api/admin/signup-requests/{approved_request['id']}/approve",
            headers=server.auth_headers(admin_token),
            expected_status=(200,),
        )
        if approval.get("email") != approved_email:
            raise SmokeError(f"expected approved signup email {approved_email!r}, got {approval.get('email')!r}")
        temp_password = approval.get("temp_password")
        if not temp_password:
            raise SmokeError(f"expected temp_password in signup approval response, got {approval!r}")

        approved_user = SmokeClient(
            base_url=server.base_url,
            email=approved_email,
            password=temp_password,
            timeout_seconds=server.timeout_seconds,
        )
        approved_token = approved_user.login()
        identity = approved_user.request(
            "GET",
            "/api/auth/me",
            headers=approved_user.auth_headers(approved_token),
            expected_status=(200,),
        )
        if identity.get("email") != approved_email:
            raise SmokeError(f"expected approved user email {approved_email!r}, got {identity.get('email')!r}")
        if identity.get("is_admin") is not False:
            raise SmokeError(f"expected approved signup user to be non-admin, got {identity!r}")

        server.request(
            "DELETE",
            f"/api/admin/signup-requests/{rejected_request['id']}",
            headers=server.auth_headers(admin_token),
            expected_status=(204,),
        )

        pending_after = list_pending_requests(server, admin_token)
        leftovers = [request for request in pending_after if request.get("email") in tracked_emails]
        if leftovers:
            raise SmokeError(f"expected processed signup requests to leave the pending list, got {leftovers!r}")

        rejected_approval = server.request(
            "POST",
            f"/api/admin/signup-requests/{rejected_request['id']}/approve",
            headers=server.auth_headers(admin_token),
            expected_status=(404,),
        )
        assert_body("approve rejected signup request", rejected_approval, "request not found or already processed\n")

        print(
            json.dumps(
                {
                    "approved_request": approved_request,
                    "approval": {
                        "email": approval["email"],
                        "temp_password_length": len(temp_password),
                    },
                    "approved_identity": identity,
                    "rejected_request": rejected_request,
                    "rejected_approval": rejected_approval.strip(),
                    "pending_after": len(pending_after),
                },
                indent=2,
            )
        )
    finally:
        primary_error = sys.exc_info()[0] is not None
        try:
            cleanup_pending_requests(server, admin_token, tracked_emails)
        except Exception as exc:  # noqa: BLE001 - keep the primary smoke failure.
            if primary_error:
                print(f"[cleanup] {exc}", file=sys.stderr)
            else:
                raise


def list_pending_requests(server, admin_token):
    pending = server.request(
        "GET",
        "/api/admin/signup-requests",
        headers=server.auth_headers(admin_token),
        expected_status=(200,),
    )
    if pending is None:
        return []
    return pending


def require_pending_request(requests, email):
    for request in requests:
        if request.get("email") != email:
            continue
        if request.get("id") is None:
            raise SmokeError(f"expected signup request id for {email!r}, got {request!r}")
        if not request.get("requested_at"):
            raise SmokeError(f"expected requested_at for signup request {email!r}, got {request!r}")
        return request
    raise SmokeError(f"expected pending signup request for {email!r}, got {requests!r}")


def cleanup_pending_requests(server, admin_token, emails):
    for request in list_pending_requests(server, admin_token):
        if request.get("email") not in emails:
            continue
        server.request(
            "DELETE",
            f"/api/admin/signup-requests/{request['id']}",
            headers=server.auth_headers(admin_token),
            expected_status=(204,),
        )


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
