"""§6.7 — Python agent ↔ ClickHouse (L1, security-critical).

Proves the engine-level guarantees that make it safe to run LLM-generated SQL:
every read of ``clicks`` is constrained to the authenticated user by
``additional_table_filters`` (even when the SQL omits or forges the filter), the
``analyst`` user physically cannot write, and result-row caps are enforceable
under its readonly=2 profile.

No Gemini is involved: validate_sql / execute_query / a raw analyst client are
driven directly with seeded SQL, against the real ClickHouse from
docker-compose.test.yml. Each test seeds fresh UUID user ids so rows never bleed.
"""
import pytest

from app.agent.sql_validator import validate_sql
from app.db.clickhouse import execute_query, get_client
from tests.integration.conftest import CH_DB, new_user_id

pytestmark = pytest.mark.integration


def test_row_filter_returns_only_callers_rows(analyst_clickhouse, seed_clicks):
    """6.7.1 — additional_table_filters scopes every read to the caller, even when
    the SQL carries NO user filter at all. This deliberately bypasses validate_sql
    to exercise the engine-level backstop, not the text check."""
    user_a, user_b = new_user_id(), new_user_id()
    base = new_user_id()[:8]
    seed_clicks(user_a, "it_iso_" + base + "_a", 3)
    seed_clicks(user_b, "it_iso_" + base + "_b", 2)

    result = execute_query(f"SELECT count() AS c FROM {CH_DB}.clicks", user_a)
    assert int(result["data"][0][0]) == 3  # only A's rows, B's are invisible


def test_forged_other_user_filter_returns_nothing(analyst_clickhouse, seed_clicks):
    """6.7.1 — SQL that explicitly targets ANOTHER user's id returns zero rows: the
    injected filter is AND-ed in, so ``user_id='B' AND user_id='A'`` is empty."""
    user_a, user_b = new_user_id(), new_user_id()
    base = new_user_id()[:8]
    seed_clicks(user_a, "it_iso_" + base + "_a", 3)
    seed_clicks(user_b, "it_iso_" + base + "_b", 4)

    forged = f"SELECT count() AS c FROM {CH_DB}.clicks WHERE user_id = '{user_b}'"
    result = execute_query(forged, user_a)
    assert int(result["data"][0][0]) == 0


def test_validated_or_injection_is_neutralized(analyst_clickhouse, seed_clicks):
    """6.7.4 — an OR-injection that PASSES validate_sql (it keeps the caller's own
    equality predicate, so the text check is satisfied) still cannot read another
    user's rows: the engine filter AND-s ``user_id='A'`` over the whole query."""
    user_a, user_b = new_user_id(), new_user_id()
    base = new_user_id()[:8]
    seed_clicks(user_a, "it_iso_" + base + "_a", 3)
    seed_clicks(user_b, "it_iso_" + base + "_b", 5)

    injected = (
        f"SELECT count() AS c FROM {CH_DB}.clicks "
        f"WHERE user_id = '{user_a}' OR user_id = '{user_b}'"
    )
    is_valid, cleaned, err = validate_sql(injected, user_a)
    assert is_valid, f"expected the OR-injection to pass text validation, got: {err}"

    result = execute_query(cleaned, user_a)
    # 3 (A only), NOT 8 (A+B): the engine row filter neutralised the OR.
    assert int(result["data"][0][0]) == 3


def test_analyst_user_cannot_write(analyst_clickhouse):
    """6.7.2 — the analyst connects under readonly=2: an INSERT is refused at the
    engine regardless of any SQL-text validation. This is the robust backstop the
    denylist validator explicitly defers to."""
    client = get_client()  # analyst client, wired by the fixture
    with pytest.raises(Exception) as exc:
        client.command(
            f"INSERT INTO {CH_DB}.clicks "
            "(timestamp, short_id, user_id, ip, user_agent, referrer, country, device, browser, is_bot) "
            "VALUES (now64(3), 'x', 'x', '0.0.0.0', 'ua', '', 'UA', 'desktop', 'chrome', 0)"
        )
    assert "readonly" in str(exc.value).lower()


def test_result_row_cap_is_enforceable(analyst_clickhouse, seed_clicks):
    """6.7.3 — the analyst session honours result-row caps. We assert with
    ``result_overflow_mode='throw'`` for a DETERMINISTIC result (production uses
    'break', which truncates silently at block granularity — not reliably testable
    by row count on a tiny dataset). The point proven is that readonly=2 PERMITS
    setting these caps and the engine enforces them."""
    user_a = new_user_id()
    seed_clicks(user_a, "it_cap_" + new_user_id()[:8], 5)

    client = get_client()
    with pytest.raises(Exception) as exc:
        client.query(
            f"SELECT short_id FROM {CH_DB}.clicks WHERE user_id = '{user_a}'",
            settings={"max_result_rows": 1, "result_overflow_mode": "throw"},
        )
    # ClickHouse raises e.g. "Limit for result exceeded, max rows: 1.00 ...".
    assert "exceed" in str(exc.value).lower()
