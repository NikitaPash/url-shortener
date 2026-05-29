import clickhouse_connect
from opentelemetry import trace

from app.config import settings

tracer = trace.get_tracer("db.clickhouse")
_client = None


def get_client():
    """Lazy-initialize a ClickHouse HTTP client."""
    global _client
    if _client is None:
        _client = clickhouse_connect.get_client(
            host=settings.clickhouse_host,
            port=settings.clickhouse_port,
            database=settings.clickhouse_database,
            username=settings.clickhouse_user,
            password=settings.clickhouse_password,
        )
    return _client


def _user_row_filter(user_id: str) -> str:
    """Build an additional_table_filters value that constrains every read of the
    clicks table to the authenticated user — including subqueries, and regardless
    of what the query text says. user_id is a server-verified UUID; quotes are
    doubled per ClickHouse string-literal rules as defense in depth."""
    table = f"{settings.clickhouse_database}.clicks"
    predicate = f"user_id = '{user_id}'".replace("'", "''")
    return f"{{'{table}': '{predicate}'}}"


def execute_query(sql: str, user_id: str) -> dict:
    """Execute a validated SQL query scoped to user_id, returning columns + rows.

    Returns:
        {"columns": [...], "data": [[...], ...], "row_count": N}
    """
    with tracer.start_as_current_span("clickhouse.execute_query") as span:
        span.set_attribute("sql", sql[:200])

        client = get_client()
        # Engine-level guardrails: even if a query slips past validation it cannot
        # return an unbounded result set, run indefinitely, or read another user's
        # rows (additional_table_filters is AND-ed into every clicks-table read).
        query_settings = {
            "max_result_rows": settings.max_query_rows,
            "result_overflow_mode": "break",
            "max_execution_time": settings.query_timeout_seconds,
            "additional_table_filters": _user_row_filter(user_id),
        }
        result = client.query(sql, settings=query_settings)

        columns = list(result.column_names)
        data = []
        for row in result.result_rows:
            data.append([
                str(val) if not isinstance(val, (int, float, str, bool, type(None))) else val
                for val in row
            ])

        span.set_attribute("row_count", len(data))
        return {
            "columns": columns,
            "data": data,
            "row_count": len(data),
        }
