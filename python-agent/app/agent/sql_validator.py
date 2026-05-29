import re

from app.config import settings

# Denylist of forbidden SQL constructs. This is defense-in-depth layered on top
# of the SELECT-only and per-user-filter checks below — it is NOT a complete
# sandbox. Executing LLM-generated SQL as raw text is inherently risky; a denylist
# can never be exhaustive. The robust backstop is a dedicated read-only ClickHouse
# user constrained by a row policy (see README/REPORT recommendation).
FORBIDDEN_PATTERNS = [
    r'\b(CREATE|DROP|ALTER|TRUNCATE)\b',
    r'\b(INSERT|UPDATE|DELETE|REPLACE|MERGE)\b',
    r'\b(GRANT|REVOKE|ATTACH|DETACH|RENAME|OPTIMIZE|KILL|SYSTEM)\b',
    # UNION can append a second SELECT that escapes the per-user filter.
    r'\bUNION\b',
    # A trailing SETTINGS clause could override the per-user row filter we inject.
    r'\bSETTINGS\b',
    # SELECT ... INTO OUTFILE writes files on the server.
    r'\b(INTO|OUTFILE)\b',
    # Table functions that read external data / enable SSRF or exfiltration.
    r'\b(URL|FILE|S3|S3CLUSTER|REMOTE|REMOTESECURE|MYSQL|POSTGRESQL|JDBC|ODBC|HDFS|HIVE|INPUT|GENERATERANDOM)\s*\(',
    r'--',
    r'/\*',
    r';\s*\w',
]


def validate_sql(sql: str, user_id: str) -> tuple[bool, str, str]:
    """Validate and sanitize LLM-generated SQL before execution.

    Returns:
        (is_valid, cleaned_sql, error_message)
    """
    if not sql or not sql.strip():
        return False, "", "Empty SQL generated"

    sql = sql.strip()
    sql = re.sub(r'^```(sql)?\s*', '', sql)
    sql = re.sub(r'\s*```$', '', sql)
    sql = sql.strip().rstrip(';')

    upper_sql = sql.upper()

    for pattern in FORBIDDEN_PATTERNS:
        if re.search(pattern, upper_sql):
            return False, "", f"Forbidden SQL pattern detected: {pattern}"

    if not upper_sql.lstrip().startswith('SELECT'):
        return False, "", "Only SELECT queries are allowed"

    if 'CLICKS' not in upper_sql:
        return False, "", "Query must reference the clicks table"

    # Require an explicit equality predicate on the authenticated user's own id,
    # not merely that the id appears somewhere in the text. Without this, a
    # prompt-injected query could keep the id in a harmless position while
    # reading other users' rows.
    user_filter = re.compile(rf"\buser_id\s*=\s*'{re.escape(user_id)}'", re.IGNORECASE)
    if not user_filter.search(sql):
        return False, "", "Query must filter by the authenticated user_id"

    if 'LIMIT' not in upper_sql:
        sql = sql + f" LIMIT {settings.max_query_rows}"
    else:
        limit_match = re.search(r'LIMIT\s+(\d+)', upper_sql)
        if limit_match:
            limit_val = int(limit_match.group(1))
            if limit_val > settings.max_query_rows:
                sql = re.sub(
                    r'LIMIT\s+\d+',
                    f'LIMIT {settings.max_query_rows}',
                    sql,
                    flags=re.IGNORECASE,
                )

    return True, sql + ";", ""
