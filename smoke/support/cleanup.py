from __future__ import annotations

import sys
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass, field


@dataclass
class CleanupScope:
    primary_error: BaseException | None = None
    errors: list[Exception] = field(default_factory=list)

    @contextmanager
    def preserve_primary_error(self) -> Iterator[None]:
        try:
            yield
        except BaseException as exc:
            self.primary_error = exc
            raise

    @property
    def has_primary_error(self) -> bool:
        return self.primary_error is not None

    def run(self, label: str, fn: Callable[[], object]) -> None:
        try:
            fn()
        except Exception as exc:
            wrapped = RuntimeError(f"{label}: {exc}")
            wrapped.__cause__ = exc
            self.errors.append(wrapped)

    def finish(self, *, preserve: bool | None = None, prefix: str = "[cleanup] ") -> None:
        if not self.errors:
            return

        if preserve is None:
            preserve = self.has_primary_error

        if preserve:
            for exc in self.errors:
                print(f"{prefix}{exc}", file=sys.stderr)
            return

        if len(self.errors) == 1:
            raise self.errors[0]

        raise ExceptionGroup("multiple cleanup failures", self.errors)

    def finish_for_test(self, request: object, *, prefix: str = "[cleanup] ") -> None:
        self.finish(preserve=self.has_primary_error or _request_failed(request), prefix=prefix)

    def finish_for_session(self, request: object, *, prefix: str = "[cleanup] ") -> None:
        testsfailed = getattr(getattr(request, "session", None), "testsfailed", 0)
        self.finish(preserve=self.has_primary_error or testsfailed > 0, prefix=prefix)


def _request_failed(request: object) -> bool:
    node = getattr(request, "node", None)
    for phase in ("setup", "call"):
        report = getattr(node, f"rep_{phase}", None)
        if getattr(report, "failed", False):
            return True
    return False
