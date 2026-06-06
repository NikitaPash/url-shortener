from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    clickhouse_host: str = "localhost"
    clickhouse_port: int = 8123
    clickhouse_database: str = "shortener"
    clickhouse_user: str = "default"
    clickhouse_password: str = ""

    gemini_api_key: str = ""
    gemini_model: str = "gemini-2.5-flash-lite"

    jwt_secret: str = "local-dev-secret-change-in-production"

    # Same Redis instance/DB the Go API writes its JWT logout denylist to.
    redis_addr: str = "localhost:6379"
    redis_password: str = ""
    redis_db: int = 0

    max_query_rows: int = 1000
    # Hard ceiling on how long a single analytics query may run, in seconds.
    query_timeout_seconds: int = 10

    jaeger_endpoint: str = "localhost:4318"
    # Export spans to the OTLP collector (Jaeger). Disabled in tests so the batch
    # exporter's worker thread isn't left retrying against an absent collector.
    otel_enabled: bool = True
    metrics_port: int = 9093


settings = Settings()
