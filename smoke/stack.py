from __future__ import annotations

import os
import subprocess


# ComposeStack owns the smoke stack lifecycle so scenario code only deals with
# HTTP behavior, not Docker CLI details.
class ComposeStack:
    def __init__(self, compose_file, repo_root):
        self.compose_file = str(compose_file)
        self.repo_root = str(repo_root)
        self.docker = os.environ.get("DOCKER", "docker")
        self.project_name = os.environ.get("SMOKE_COMPOSE_PROJECT", "wacht-smoke")

    def up(self):
        self._run("compose", "-f", self.compose_file, "up", "-d", "--build")

    def down(self):
        self._run("compose", "-f", self.compose_file, "down", "-v")

    def logs(self):
        self._run("compose", "-f", self.compose_file, "logs", check=False)

    def _run(self, *args, check=True):
        cmd = [self.docker, *args]
        print(f"[stack] {' '.join(cmd)}")
        env = os.environ.copy()
        # A dedicated project name prevents the smoke stack from colliding with
        # the normal local dev stack.
        env["COMPOSE_PROJECT_NAME"] = self.project_name
        subprocess.run(cmd, cwd=self.repo_root, env=env, check=check)
