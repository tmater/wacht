from __future__ import annotations

import json
import uuid

from smoke.client import SmokeClient, SmokeError
from smoke.support.cleanup import CleanupScope


# Prove the public signup request path feeds the admin approval queue, that an
# approved request turns into a usable non-admin account, and that rejection
# removes a pending request cleanly.
def test_signup_approval(server):
    server.wait_for_health()
    admin_token = server.login()
    approved_email = f"smoke-signup-approved-{uuid.uuid4().hex[:12]}@wacht.local"
    rejected_email = f"smoke-signup-rejected-{uuid.uuid4().hex[:12]}@wacht.local"
    chosen_password = f"smoke-setup-{uuid.uuid4().hex[:12]}"
    tracked_emails = {approved_email, rejected_email}
    cleanup = CleanupScope()

    try:
        with cleanup.preserve_primary_error():
            for email in (approved_email, rejected_email):
                server.request_access(email)

            pending_before = server.list_signup_requests(admin_token)
            approved_request = require_pending_request(pending_before, approved_email)
            rejected_request = require_pending_request(pending_before, rejected_email)

            approval = server.approve_signup_request(admin_token, approved_request["id"])
            if approval.get("email") != approved_email:
                raise SmokeError(f"expected approved signup email {approved_email!r}, got {approval.get('email')!r}")
            setup_token = approval.get("setup_token")
            if not setup_token:
                raise SmokeError(f"expected setup_token in signup approval response, got {approval!r}")

            server.request_access(approved_email)
            pending_after_duplicate_request = server.list_signup_requests(admin_token)
            if any(request.get("email") == approved_email for request in pending_after_duplicate_request):
                raise SmokeError(f"expected approved signup to stay out of the pending queue, got {pending_after_duplicate_request!r}")

            approved_user = SmokeClient(
                base_url=server.base_url,
                email=approved_email,
                password=chosen_password,
                timeout_seconds=server.timeout_seconds,
            )
            setup = approved_user.setup_password(setup_token, chosen_password)
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

            server.reject_signup_request(admin_token, rejected_request["id"])
            server.request_access(rejected_email)

            pending_after = server.list_signup_requests(admin_token)
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
                            "setup_token_length": len(setup_token),
                            "expires_at": approval.get("expires_at"),
                        },
                        "setup_password_email": setup.get("email"),
                        "approved_identity": identity,
                        "rejected_request": rejected_request,
                        "rejected_approval": rejected_approval.strip(),
                        "pending_after": len(pending_after),
                    },
                    indent=2,
                )
            )
    finally:
        cleanup.run(
            "delete pending signup requests",
            lambda: cleanup_pending_requests(server, admin_token, tracked_emails),
        )
        cleanup.finish()


def list_pending_requests(server, admin_token):
    return server.list_signup_requests(admin_token)


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
        server.reject_signup_request(admin_token, request["id"])


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
