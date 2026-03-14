from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

import pytest

from smoke.client import MockClient, SmokeClient, wait_for
from smoke.stack import ComposeStack
from smoke.support.cleanup import CleanupScope


SMOKE_DIR = Path(__file__).resolve().parent
DEFAULT_COMPOSE_FILE = SMOKE_DIR / "fixtures" / "docker-compose.yml"


@dataclass
class ProbeController:
    stack: ComposeStack
    server: SmokeClient
    stopped: set[str]

    def stop(self, probe_id: str):
        self.stack.stop_service(probe_id)
        self.stopped.add(probe_id)

    def start(self, probe_id: str):
        self.stack.start_service(probe_id)
        self.stopped.discard(probe_id)

    def restore(self, probe_id: str):
        self.start(probe_id)
        self.wait_until_online(probe_id)

    def wait_until_online(self, probe_id: str, timeout_seconds: int = 90):
        cleanup_token = self.server.login()
        wait_for(
            f"{probe_id} to come back online in /status",
            timeout_seconds=timeout_seconds,
            interval_seconds=3,
            fn=lambda: probe_online(self.server, cleanup_token, probe_id),
        )


@pytest.fixture(scope="session")
def stack(request) -> ComposeStack:
    repo_root = SMOKE_DIR.parent
    stack = ComposeStack(DEFAULT_COMPOSE_FILE.resolve(), repo_root)
    cleanup = CleanupScope()

    # Scenarios that control probe containers must target the exact same Compose
    # file and project as the session-owned stack.
    os.environ["SMOKE_COMPOSE_FILE"] = stack.compose_file
    os.environ["SMOKE_COMPOSE_PROJECT"] = stack.project_name

    try:
        with cleanup.preserve_primary_error():
            stack.up()
            yield stack
    finally:
        cleanup.run("docker compose down -v", stack.down)
        cleanup.finish_for_session(request, prefix="[stack] cleanup failed: ")


@pytest.fixture(scope="session")
def server(stack: ComposeStack) -> SmokeClient:
    base_url = f"http://localhost:{os.environ.get('SMOKE_HTTP_PORT', '18080')}"
    return SmokeClient(
        base_url=base_url,
        email="smoke@wacht.local",
        password="smoke-password",
    )


@pytest.fixture(scope="session")
def mock(stack: ComposeStack) -> MockClient:
    base_url = f"http://localhost:{os.environ.get('SMOKE_MOCK_PORT', '19090')}"
    return MockClient(base_url=base_url)


@pytest.fixture
def probes(request, stack: ComposeStack, server: SmokeClient):
    cleanup = CleanupScope()
    controller = ProbeController(stack=stack, server=server, stopped=set())
    try:
        with cleanup.preserve_primary_error():
            yield controller
    finally:
        if controller.stopped:
            for probe_id in sorted(controller.stopped):
                cleanup.run(
                    f"restart {probe_id}",
                    lambda probe_id=probe_id: controller.restore(probe_id),
                )
            cleanup.finish_for_test(request, prefix="[cleanup] ")


@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport(item, call):
    outcome = yield
    report = outcome.get_result()
    setattr(item, f"rep_{report.when}", report)


@pytest.fixture(autouse=True)
def dump_compose_logs_on_failure(request, stack: ComposeStack):
    yield
    report = getattr(request.node, "rep_call", None)
    if report is not None and report.failed:
        stack.logs()


def probe_online(server: SmokeClient, token: str, probe_id: str):
    status = server.get_status(token)
    probes = {probe["probe_id"]: probe for probe in status.get("probes", [])}
    probe = probes.get(probe_id)
    if probe is None or not probe.get("online", False):
        return None
    return probe
