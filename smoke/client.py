from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass


class SmokeError(RuntimeError):
    pass


def http_request(base_url, method, path, *, payload=None, headers=None, expected_status=(200,), timeout_seconds=10):
    url = urllib.parse.urljoin(base_url.rstrip("/") + "/", path.lstrip("/"))
    body = None
    request_headers = {"Accept": "application/json"}
    if payload is not None:
        body = json.dumps(payload).encode("utf-8")
        request_headers["Content-Type"] = "application/json"
    if headers:
        request_headers.update(headers)

    req = urllib.request.Request(url, data=body, headers=request_headers, method=method)

    try:
        # Treat expected non-2xx responses the same as success paths so the
        # caller only has to reason about status codes in one place.
        with urllib.request.urlopen(req, timeout=timeout_seconds) as resp:
            status = resp.status
            raw = resp.read()
            content_type = resp.headers.get("Content-Type", "")
    except urllib.error.HTTPError as exc:
        status = exc.code
        raw = exc.read()
        content_type = exc.headers.get("Content-Type", "")
    except urllib.error.URLError as exc:
        raise SmokeError(f"{method} {path} failed: {exc.reason}") from exc

    if status not in expected_status:
        body_text = raw.decode("utf-8", errors="replace").strip()
        raise SmokeError(f"{method} {path} returned {status}, expected {expected_status}: {body_text}")

    if not raw:
        return None
    if "application/json" in content_type:
        return json.loads(raw)
    return raw.decode("utf-8", errors="replace")


# Poll until a condition becomes truthy while preserving the last useful error
# for the final timeout message.
def wait_for(description, timeout_seconds, interval_seconds, fn):
    deadline = time.monotonic() + timeout_seconds
    attempt = 0
    last_error = None

    while time.monotonic() < deadline:
        attempt += 1
        try:
            value = fn()
            if value:
                return value
        except Exception as exc:  # noqa: BLE001 - smoke diagnostics should keep polling.
            last_error = exc
        print(f"[wait] {description} (attempt {attempt})")
        time.sleep(interval_seconds)

    message = f"timed out waiting for {description}"
    if last_error is not None:
        message = f"{message}: {last_error}"
    raise SmokeError(message)


@dataclass
class SmokeClient:
    base_url: str
    email: str
    password: str
    timeout_seconds: int = 10

    def request(self, method, path, payload=None, headers=None, expected_status=(200,)):
        return http_request(
            self.base_url,
            method,
            path,
            payload=payload,
            headers=headers,
            expected_status=expected_status,
            timeout_seconds=self.timeout_seconds,
        )

    def login(self):
        response = self.request(
            "POST",
            "/api/auth/login",
            payload={"email": self.email, "password": self.password},
            expected_status=(200,),
        )
        token = response.get("token")
        if not token:
            raise SmokeError("login response did not contain a token")
        return token

    def wait_for_health(self, timeout_seconds=90, interval_seconds=2):
        wait_for(
            "server health",
            timeout_seconds,
            interval_seconds,
            lambda: self.request("GET", "/healthz", expected_status=(200,)) is None or True,
        )

    def get_status(self, token):
        return self.request("GET", "/status", headers=self.auth_headers(token), expected_status=(200,))

    def list_incidents(self, token):
        incidents = self.request("GET", "/api/incidents", headers=self.auth_headers(token), expected_status=(200,))
        if incidents is None:
            return []
        return incidents

    def list_checks(self, token):
        checks = self.request("GET", "/api/checks", headers=self.auth_headers(token), expected_status=(200,))
        if checks is None:
            return []
        return checks

    def create_check(self, token, check):
        self.request("POST", "/api/checks", payload=check, headers=self.auth_headers(token), expected_status=(201,))

    def delete_check(self, token, check_id):
        encoded = urllib.parse.quote(check_id, safe="")
        self.request("DELETE", f"/api/checks/{encoded}", headers=self.auth_headers(token), expected_status=(204,))

    def delete_check_if_present(self, token, check_id):
        checks = self.list_checks(token)
        if any(check.get("id") == check_id for check in checks):
            self.delete_check(token, check_id)

    @staticmethod
    def auth_headers(token):
        return {"Authorization": f"Bearer {token}"}


# MockClient drives the controllable target service used by the E2E quorum
# smoke scenario.
@dataclass
class MockClient:
    base_url: str
    timeout_seconds: int = 10

    def request(self, method, path, payload=None, headers=None, expected_status=(200,)):
        return http_request(
            self.base_url,
            method,
            path,
            payload=payload,
            headers=headers,
            expected_status=expected_status,
            timeout_seconds=self.timeout_seconds,
        )

    def get_state(self):
        return self.request("GET", "/state", expected_status=(200, 503))

    def set_state(self, status):
        self.request("POST", "/state", payload={"status": status}, expected_status=(204,))

    def list_webhooks(self):
        payloads = self.request("GET", "/webhook", expected_status=(200,))
        if payloads is None:
            return []
        return payloads

    def clear_webhooks(self):
        self.request("DELETE", "/webhook", expected_status=(204,))
