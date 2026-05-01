from __future__ import annotations

import json
import re
import socket
import tempfile
import uuid
from pathlib import Path

from smoke.client import SmokeClient, SmokeError, wait_for
from smoke.stack import ComposeStack
from smoke.support.cleanup import CleanupScope
from smoke.support.quorum import healthy_status


REPO_ROOT = Path(__file__).resolve().parents[2]
ROOT_COMPOSE_FILE = REPO_ROOT / "docker-compose.yml"
CONFIG_DIR = REPO_ROOT / "config"
SEED_EMAIL = "release-admin@wacht.local"
SEED_PASSWORD = "release-password"


# Prove the documented self-host install path works with the real root
# docker-compose.yml and shipped config shape, not only the dedicated smoke
# fixture stack.
def test_release_install_bootstrap_path():
    web_port = reserve_tcp_port()
    project_name = f"wacht-release-{uuid.uuid4().hex[:8]}"
    cleanup = CleanupScope()
    failed = True

    with tempfile.TemporaryDirectory(prefix="wacht-release-smoke-") as temp_dir:
        config_dir = prepare_release_configs(Path(temp_dir))
        stack = ComposeStack(
            ROOT_COMPOSE_FILE.resolve(),
            REPO_ROOT,
            env_overrides={
                "COMPOSE_PROJECT_NAME": project_name,
                "SERVER_CONFIG_PATH": str((config_dir / "server.yaml").resolve()),
                "PROBE_1_CONFIG_PATH": str((config_dir / "probe-1.yaml").resolve()),
                "PROBE_2_CONFIG_PATH": str((config_dir / "probe-2.yaml").resolve()),
                "PROBE_3_CONFIG_PATH": str((config_dir / "probe-3.yaml").resolve()),
                "WACHT_WEB_PORT": str(web_port),
            },
        )
        server = SmokeClient(
            base_url=f"http://127.0.0.1:{web_port}",
            email=SEED_EMAIL,
            password=SEED_PASSWORD,
        )

        try:
            with cleanup.preserve_primary_error():
                stack.up()
                server.wait_for_health(timeout_seconds=120)

                seed_token = server.login()
                identity = server.get_me(seed_token)
                if identity.get("email") != SEED_EMAIL:
                    raise SmokeError(f"expected seeded admin email {SEED_EMAIL!r}, got {identity.get('email')!r}")
                if identity.get("is_admin") is not True:
                    raise SmokeError(f"expected seeded admin user, got {identity!r}")

                updated_password = f"{SEED_PASSWORD}-{uuid.uuid4().hex[:8]}"
                server.request(
                    "PUT",
                    "/api/auth/change-password",
                    payload={
                        "current_password": SEED_PASSWORD,
                        "new_password": updated_password,
                    },
                    headers=server.auth_headers(seed_token),
                    expected_status=(204,),
                )

                rejected_old_login = server.request(
                    "POST",
                    "/api/auth/login",
                    payload={"email": SEED_EMAIL, "password": SEED_PASSWORD},
                    expected_status=(401,),
                )
                assert_body("old seed password login after onboarding change", rejected_old_login, "invalid credentials\n")

                updated_user = SmokeClient(
                    base_url=server.base_url,
                    email=SEED_EMAIL,
                    password=updated_password,
                    timeout_seconds=server.timeout_seconds,
                )
                updated_token = updated_user.login()

                check_name = f"release-install-{uuid.uuid4().hex[:8]}"
                updated_user.create_check(
                    updated_token,
                    {
                        "name": check_name,
                        "type": "http",
                        "target": "http://server:8080/healthz",
                        "interval": 1,
                    },
                )

                status = wait_for(
                    "release-install check to become healthy through the packaged stack",
                    timeout_seconds=120,
                    interval_seconds=3,
                    fn=lambda: healthy_status(updated_user, updated_token, check_name),
                )

                print(
                    json.dumps(
                        {
                            "web_port": web_port,
                            "seed_identity": identity,
                            "check_status": status,
                        },
                        indent=2,
                    )
                )
                failed = False
        finally:
            if failed:
                cleanup.run("docker compose logs", stack.logs)
            cleanup.run("docker compose down -v", stack.down)
            cleanup.finish()


def prepare_release_configs(temp_dir: Path) -> Path:
    config_dir = temp_dir / "config"
    config_dir.mkdir(parents=True, exist_ok=True)

    server_text = (CONFIG_DIR / "server.yaml").read_text(encoding="utf-8")
    server_text = server_text.replace("secret: changeme-probe-1", "secret: release-secret-1")
    server_text = server_text.replace("secret: changeme-probe-2", "secret: release-secret-2")
    server_text = server_text.replace("secret: changeme-probe-3", "secret: release-secret-3")
    server_text = server_text.replace("email: admin@wacht.local", f"email: {SEED_EMAIL}")
    server_text = server_text.replace("password: changeme", f"password: {SEED_PASSWORD}")
    server_text = re.sub(r"\nchecks:\n[\s\S]*\Z", "\nchecks: []\n", server_text)
    (config_dir / "server.yaml").write_text(server_text, encoding="utf-8")

    for probe_id in ("1", "2", "3"):
        probe_text = (CONFIG_DIR / f"probe-{probe_id}.yaml").read_text(encoding="utf-8")
        probe_text = probe_text.replace(f"secret: changeme-probe-{probe_id}", f"secret: release-secret-{probe_id}")
        (config_dir / f"probe-{probe_id}.yaml").write_text(probe_text, encoding="utf-8")

    return config_dir


def reserve_tcp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def assert_body(label, body, expected):
    if body != expected:
        raise SmokeError(f"expected {label} response {expected!r}, got {body!r}")
