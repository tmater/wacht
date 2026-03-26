from __future__ import annotations

import pytest


# The fixture-stack smoke suite has a package-level autouse fixture in
# smoke/conftest.py that always boots the dedicated smoke topology. Release
# install tests need to manage the root docker-compose.yml stack themselves, so
# override that fixture here to keep this subtree isolated.
@pytest.fixture(autouse=True)
def dump_compose_logs_on_failure(request):
    yield
