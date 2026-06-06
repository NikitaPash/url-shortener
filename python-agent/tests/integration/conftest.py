"""Fixtures for the agent's L1 integration tests against the REAL ClickHouse +
Redis brought up by docker-compose.test.yml (`make test-infra-up`).

These prove the two security-critical boundaries that unit tests with fakes
structurally cannot: the engine-level per-user row filter and read-only
enforcement on the ClickHouse ``analyst`` user (§6.7), and the cross-service JWT
logout denylist in Redis (§6.8).

Every test is tagged ``@pytest.mark.integration`` (module-level ``pytestmark`` in
each test file) and is excluded from the default ``pytest`` run. Each fixture
skips cleanly with an actionable reason if the harness is down, so the suite is
safe to run without infra up.
"""
import os
import uuid
from datetime import datetime, timezone

import clickhouse_connect
import pytest
import redis as sync_redis

import app.db.clickhouse as ch_module
import app.db.redis_client as redis_module
from app.config import settings
from tests.conftest import JWT_SECRET

# Harness connection params — match the hardcoded literals in docker-compose.test.yml.
# Overridable via env so the same tests can point at a different harness if needed.
CH_HOST = os.getenv("TEST_CLICKHOUSE_HOST", "localhost")
CH_PORT = int(os.getenv("TEST_CLICKHOUSE_PORT", "8123"))  # the agent talks HTTP
CH_DB = "shortener"
CH_WRITE_USER = os.getenv("TEST_CLICKHOUSE_WRITE_USER", "default")
CH_WRITE_PASSWORD = os.getenv("TEST_CLICKHOUSE_WRITE_PASSWORD", "testpass")
CH_ANALYST_USER = "analyst"
CH_ANALYST_PASSWORD = os.getenv("TEST_CLICKHOUSE_ANALYST_PASSWORD", "testpass_ro")

# 127.0.0.1 (not "localhost"): on Docker Desktop / Windows, "localhost" can
# resolve to IPv6 ::1, which Docker black-holes (the port is published on IPv4
# 127.0.0.1). redis.asyncio doesn't fall back to IPv4 the way sync redis-py does,
# so it would hang until the socket timeout — pin IPv4 explicitly.
REDIS_HOST = os.getenv("TEST_REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.getenv("TEST_REDIS_PORT", "6379"))

# Column order of shortener.clicks (clickhouse/init.sql), used when seeding.
CLICK_COLUMNS = [
    "timestamp", "short_id", "user_id", "ip", "user_agent",
    "referrer", "country", "device", "browser", "is_bot",
]


def new_user_id() -> str:
    """A fresh UUID user id, so seeded rows never bleed across tests/runs."""
    return str(uuid.uuid4())


# ── ClickHouse ────────────────────────────────────────────────────────────────

@pytest.fixture(scope="session")
def ch_write_client():
    """Write-capable ClickHouse client (the ``default`` user) used only to SEED
    test rows — the analyst is read-only and cannot insert. Skips the suite if the
    harness ClickHouse is unreachable."""
    try:
        client = clickhouse_connect.get_client(
            host=CH_HOST, port=CH_PORT, database=CH_DB,
            username=CH_WRITE_USER, password=CH_WRITE_PASSWORD,
        )
        client.command("SELECT 1")
    except Exception as exc:  # pragma: no cover - environment-dependent
        pytest.skip(
            f"ClickHouse harness unreachable at {CH_HOST}:{CH_PORT} "
            f"(start it with `make test-infra-up`): {exc}"
        )
    yield client
    client.close()


@pytest.fixture
def seed_clicks(ch_write_client):
    """Return a helper that inserts ``n`` click rows for a (user_id, short_id).
    Tests pass fresh UUID user ids so assertions can filter precisely."""
    def _seed(user_id, short_id, n=1, *, device="desktop", browser="chrome", is_bot=0):
        now = datetime.now(timezone.utc)
        rows = [
            [now, short_id, user_id, "203.0.113.5", "curl/8.4.0", "",
             "UA", device, browser, is_bot]
            for _ in range(n)
        ]
        ch_write_client.insert(f"{CH_DB}.clicks", rows, column_names=CLICK_COLUMNS)

    return _seed


@pytest.fixture
def analyst_clickhouse():
    """Point app.db.clickhouse at the read-only ``analyst`` user on the harness and
    reset its cached client, so execute_query()/get_client() behave EXACTLY as in
    production (readonly=2 profile + engine-level additional_table_filters). The
    original settings are restored afterwards. Skips if the analyst can't connect."""
    saved = {
        "host": settings.clickhouse_host,
        "port": settings.clickhouse_port,
        "database": settings.clickhouse_database,
        "user": settings.clickhouse_user,
        "password": settings.clickhouse_password,
    }

    def _restore():
        settings.clickhouse_host = saved["host"]
        settings.clickhouse_port = saved["port"]
        settings.clickhouse_database = saved["database"]
        settings.clickhouse_user = saved["user"]
        settings.clickhouse_password = saved["password"]
        ch_module._client = None

    settings.clickhouse_host = CH_HOST
    settings.clickhouse_port = CH_PORT
    settings.clickhouse_database = CH_DB
    settings.clickhouse_user = CH_ANALYST_USER
    settings.clickhouse_password = CH_ANALYST_PASSWORD
    ch_module._client = None  # force a reconnect as analyst

    try:
        ch_module.get_client().command("SELECT 1")
    except Exception as exc:  # pragma: no cover - environment-dependent
        _restore()
        pytest.skip(
            f"ClickHouse `analyst` user unreachable at {CH_HOST}:{CH_PORT} "
            f"(start it with `make test-infra-up`): {exc}"
        )

    yield

    if ch_module._client is not None:
        try:
            ch_module._client.close()
        except Exception:
            pass
    _restore()


# ── Redis ─────────────────────────────────────────────────────────────────────

@pytest.fixture(scope="session")
def redis_client():
    """Sync Redis client to the harness, used to write/clean denylist keys exactly
    as the Go API would on logout. Skips the suite if Redis is unreachable."""
    client = sync_redis.Redis(
        host=REDIS_HOST, port=REDIS_PORT, decode_responses=True,
        socket_connect_timeout=2, socket_timeout=2,
    )
    try:
        client.ping()
    except Exception as exc:  # pragma: no cover - environment-dependent
        pytest.skip(
            f"Redis harness unreachable at {REDIS_HOST}:{REDIS_PORT} "
            f"(start it with `make test-infra-up`): {exc}"
        )
    yield client
    client.close()


@pytest.fixture
async def agent_redis():
    """Point app.db.redis_client at the harness Redis (and set the JWT secret the
    test-minted tokens are signed with), resetting the cached async client so
    get_user_id_from_request() consults the REAL denylist. Restores afterwards.

    Async so teardown can ``await close_redis()`` *inside the test's event loop* —
    otherwise the cached connection is GC'd after pytest-asyncio closes the loop,
    raising a noisy "Event loop is closed" from redis.asyncio.__del__."""
    saved = {
        "addr": settings.redis_addr,
        "password": settings.redis_password,
        "db": settings.redis_db,
        "jwt_secret": settings.jwt_secret,
    }
    settings.redis_addr = f"{REDIS_HOST}:{REDIS_PORT}"
    settings.redis_password = ""
    settings.redis_db = 0
    settings.jwt_secret = JWT_SECRET
    redis_module._client = None
    yield
    await redis_module.close_redis()  # graceful aclose within the live loop
    settings.redis_addr = saved["addr"]
    settings.redis_password = saved["password"]
    settings.redis_db = saved["db"]
    settings.jwt_secret = saved["jwt_secret"]
