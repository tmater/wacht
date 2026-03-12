from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass


class SmokeError(RuntimeError):
    pass


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
        url = urllib.parse.urljoin(self.base_url.rstrip("/") + "/", path.lstrip("/"))
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
            with urllib.request.urlopen(req, timeout=self.timeout_seconds) as resp:
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

    def list_checks(self, token):
        return self.request("GET", "/api/checks", headers=self.auth_headers(token), expected_status=(200,))

    def create_check(self, token, check):
        self.request("POST", "/api/checks", payload=check, headers=self.auth_headers(token), expected_status=(201,))

    def delete_check(self, token, check_id):
        encoded = urllib.parse.quote(check_id, safe="")
        self.request("DELETE", f"/api/checks/{encoded}", headers=self.auth_headers(token), expected_status=(204,))

    @staticmethod
    def auth_headers(token):
        return {"Authorization": f"Bearer {token}"}
