from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path


SMOKE_DIR = Path(__file__).resolve().parent
# Keep the harness runnable as `python3 smoke/run.py` without packaging it as a
# proper module.
if str(SMOKE_DIR) not in sys.path:
    sys.path.insert(0, str(SMOKE_DIR))

from client import SmokeClient, SmokeError  # noqa: E402
from scenarios import crud, startup  # noqa: E402
from stack import ComposeStack  # noqa: E402


# Scenarios stay small and focused; the runner just selects them and manages
# stack lifecycle plus shared client configuration.
SCENARIOS = {
    "startup": startup.run,
    "crud": crud.run,
}


def parse_args():
    default_port = os.environ.get("SMOKE_HTTP_PORT", "18080")
    parser = argparse.ArgumentParser(description="Run Wacht smoke tests.")
    parser.add_argument(
        "--scenario",
        choices=("all", *SCENARIOS.keys()),
        action="append",
        help="Scenario to run. Defaults to all.",
    )
    parser.add_argument(
        "--compose-file",
        default=str(SMOKE_DIR / "fixtures" / "docker-compose.yml"),
        help="Docker Compose file to use for the smoke stack.",
    )
    parser.add_argument(
        "--base-url",
        default=f"http://localhost:{default_port}",
        help="Base URL for the Wacht server.",
    )
    parser.add_argument("--email", default="smoke@wacht.local", help="Seeded smoke test user email.")
    parser.add_argument("--password", default="smoke-password", help="Seeded smoke test user password.")
    parser.add_argument("--skip-stack", action="store_true", help="Reuse an already running stack.")
    parser.add_argument("--keep-up", action="store_true", help="Keep the stack running after the smoke run.")
    return parser.parse_args()


def selected_scenarios(args):
    requested = args.scenario or ["all"]
    if "all" in requested:
        return list(SCENARIOS.items())
    return [(name, SCENARIOS[name]) for name in requested]


def main():
    args = parse_args()
    repo_root = SMOKE_DIR.parent
    stack = ComposeStack(Path(args.compose_file).resolve(), repo_root)
    client = SmokeClient(base_url=args.base_url, email=args.email, password=args.password)
    started_stack = False

    try:
        if not args.skip_stack:
            stack.up()
            started_stack = True

        for name, scenario in selected_scenarios(args):
            print(f"[scenario] {name}")
            scenario(client)
            print(f"[scenario] {name} passed")
        return 0
    except SmokeError as exc:
        # On failure, dump Compose logs before teardown so the CI job keeps the
        # useful diagnosis next to the scenario error.
        print(f"[smoke] failed: {exc}", file=sys.stderr)
        if started_stack:
            stack.logs()
        return 1
    finally:
        if started_stack and not args.keep_up:
            stack.down()


if __name__ == "__main__":
    raise SystemExit(main())
