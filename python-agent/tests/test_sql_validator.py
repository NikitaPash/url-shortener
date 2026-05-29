import pytest
from app.agent.sql_validator import validate_sql

USER_ID = "test-user-123"


def test_valid_select():
    sql = f"SELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}' LIMIT 10"
    valid, cleaned, err = validate_sql(sql, USER_ID)
    assert valid is True
    assert err == ""


def test_rejects_drop():
    sql = "DROP TABLE shortener.clicks"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_rejects_insert():
    sql = f"INSERT INTO shortener.clicks VALUES (1, 2, 3)"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False


def test_rejects_missing_user_id():
    sql = "SELECT count() FROM shortener.clicks LIMIT 10"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "user_id" in err


def test_adds_limit_when_missing():
    sql = f"SELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}'"
    valid, cleaned, _ = validate_sql(sql, USER_ID)
    assert valid is True
    assert "LIMIT" in cleaned.upper()


def test_caps_excessive_limit():
    sql = f"SELECT * FROM shortener.clicks WHERE user_id = '{USER_ID}' LIMIT 999999"
    valid, cleaned, _ = validate_sql(sql, USER_ID)
    assert valid is True
    assert "LIMIT 1000" in cleaned


def test_rejects_multiple_statements():
    sql = "SELECT 1; DROP TABLE clicks"
    valid, _, _ = validate_sql(sql, USER_ID)
    assert valid is False


def test_rejects_comment():
    sql = f"SELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}' -- injected"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_strips_markdown_fences():
    sql = f"```sql\nSELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}' LIMIT 10\n```"
    valid, cleaned, _ = validate_sql(sql, USER_ID)
    assert valid is True
    assert "```" not in cleaned


def test_rejects_non_select():
    sql = f"UPDATE shortener.clicks SET short_id = 'x' WHERE user_id = '{USER_ID}'"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False


def test_rejects_empty():
    valid, _, err = validate_sql("", USER_ID)
    assert valid is False
    assert "Empty" in err


def test_rejects_union():
    sql = (
        f"SELECT short_id FROM shortener.clicks WHERE user_id = '{USER_ID}' "
        "UNION ALL SELECT short_id FROM shortener.clicks LIMIT 10"
    )
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_rejects_table_function():
    sql = f"SELECT * FROM url('http://evil/x', CSV) WHERE user_id = '{USER_ID}' LIMIT 10"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_rejects_into_outfile():
    sql = f"SELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}' INTO OUTFILE '/tmp/x' LIMIT 10"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_rejects_settings_override():
    sql = (
        f"SELECT count() FROM shortener.clicks WHERE user_id = '{USER_ID}' "
        "LIMIT 10 SETTINGS additional_table_filters = {}"
    )
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "Forbidden" in err


def test_rejects_user_id_substring_without_equality_filter():
    # The id is present but not used as the row filter — must be rejected.
    sql = f"SELECT count() FROM shortener.clicks WHERE short_id = '{USER_ID}' LIMIT 10"
    valid, _, err = validate_sql(sql, USER_ID)
    assert valid is False
    assert "user_id" in err
