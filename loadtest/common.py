"""Shared building blocks for the load-test and showcase scripts.

This module is intentionally dependency-light: the standard library plus
``httpx`` (declared by each entry script via PEP 723 inline metadata, so
``uv run loadtest/<script>.py`` provisions it automatically).

It centralises three things that both scripts need:

* **Realistic traffic data** — a weighted user-agent mix, a public-IP pool that
  spans all five RIRs (so GeoIP resolves ~50 countries), referrers and target
  URLs. Keeping this here means the benchmark and the demo paint the same
  picture in ClickHouse and Grafana.
* **A typed API client** (:class:`ApiClient`) that speaks every endpoint of the
  service the same way a real client would, including the ``X-Real-IP`` rotation
  trick that lets us drive the Go API directly (port 8080) without tripping its
  per-IP rate limiter.
* **Presentation helpers** — percentiles, number formatting, coloured banners —
  so both tools produce output that reads well in a terminal and in a report.

Why talk to the Go API on :8080 directly rather than through nginx (:80)?
nginx overwrites ``X-Real-IP`` with the connecting socket address, and the
rate limiter is keyed per client IP. Hitting the API directly lets us spread
each request across a unique IP (no throttling) and choose public vs. private
IPs to control the country mix. The AI agent is the one exception — it is only
reachable through nginx — so :class:`ApiClient` routes that single call through
the gateway.
"""

from __future__ import annotations

import os
import random
import subprocess
import sys
import threading
import time
from dataclasses import dataclass

import httpx

# ---------------------------------------------------------------------------
# Targets (override via environment)
# ---------------------------------------------------------------------------
GO_API = os.environ.get("GO_API", "http://localhost:8080")  # direct to go-api
GATEWAY = os.environ.get("GATEWAY", "http://localhost")  # nginx (AI agent, SPA)
APP_URL = f"{GATEWAY}/app/"
PROM_URL = os.environ.get("PROM_URL", "http://localhost:9090")
CLICKHOUSE_CONTAINER = os.environ.get("CLICKHOUSE_CONTAINER", "shortener-clickhouse")
CLICKHOUSE_PASSWORD = os.environ.get("CLICKHOUSE_PASSWORD", "")  # `default` user pw for docker-exec queries

# ---------------------------------------------------------------------------
# Traffic shape — shared so the benchmark and the demo populate the same
# dimensions (country / device / browser / referrer) in ClickHouse.
# ---------------------------------------------------------------------------
TARGET_URLS = [
    "https://github.com/NikitaPash/url-shortener",
    "https://en.wikipedia.org/wiki/URL_shortening",
    "https://news.ycombinator.com/",
    "https://www.reddit.com/r/programming",
    "https://stackoverflow.com/questions",
    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
    "https://example.com/landing-page",
    "https://docs.python.org/3/",
    "https://go.dev/doc/",
    "https://clickhouse.com/docs",
    "https://grafana.com/tutorials/",
    "https://www.nginx.com/blog/",
]

# First octets spanning all five RIRs — resolves to ~50 countries via GeoLite2.
PUBLIC_OCTETS = [
    3, 4, 8, 9, 12, 13, 17, 18, 23, 24, 34, 40, 45, 52, 63, 64, 66, 68, 72, 74, 96, 104, 108,
    2, 5, 31, 46, 51, 62, 80, 81, 82, 83, 84, 86, 87, 88, 90, 91, 92, 93, 94, 95,
    141, 151, 185, 188, 193, 194, 195, 212, 213, 217,
    1, 14, 27, 36, 39, 42, 43, 49, 58, 59, 60, 61, 103, 106, 110, 111, 112, 113, 114, 116, 118,
    119, 120, 121, 122, 123, 124, 125, 126, 180, 182, 183, 202, 203, 210, 211, 218, 219, 220,
    177, 179, 181, 186, 187, 189, 190, 191, 200, 201,
    41, 102, 105, 154, 196, 197,
]

# (user_agent, weight) — the consumer parses these via mssola/useragent, so the
# browser/device columns in ClickHouse end up with realistic diversity.
USER_AGENTS: list[tuple[str, int]] = [
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", 28),
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", 8),
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0", 10),
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 14.2; rv:121.0) Gecko/20100101 Firefox/121.0", 4),
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", 6),
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", 6),
    ("Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36", 12),
    ("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", 14),
    ("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", 4),
    ("Mozilla/5.0 (Linux; Android 13; SM-G998B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0 Mobile Safari/537.36", 3),
    ("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", 3),
    ("Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", 2),
]
_UA_VALUES = [ua for ua, _ in USER_AGENTS]
_UA_WEIGHTS = [w for _, w in USER_AGENTS]

REFERERS = [
    "", "", "", "",  # direct traffic dominates
    "https://news.ycombinator.com/",
    "https://t.co/",
    "https://www.google.com/",
    "https://www.reddit.com/",
    "https://www.facebook.com/",
    "https://www.linkedin.com/feed/",
    "https://twitter.com/",
    "https://www.producthunt.com/",
]

PUBLIC_IP_RATIO = 0.70  # fraction of clicks with a public IP -> a real country


# ---------------------------------------------------------------------------
# Random traffic helpers
# ---------------------------------------------------------------------------
def rand_public_ip() -> str:
    return f"{random.choice(PUBLIC_OCTETS)}.{random.randint(1, 254)}.{random.randint(0, 255)}.{random.randint(1, 254)}"


def rand_private_ip() -> str:
    return f"10.{random.randint(0, 255)}.{random.randint(0, 255)}.{random.randint(1, 254)}"


def rand_client_ip() -> str:
    return rand_public_ip() if random.random() < PUBLIC_IP_RATIO else rand_private_ip()


def rand_user_agent() -> str:
    return random.choices(_UA_VALUES, weights=_UA_WEIGHTS, k=1)[0]


def rand_referer() -> str:
    return random.choice(REFERERS)


def click_headers() -> dict[str, str]:
    """A full set of headers for one realistic redirect request."""
    h = {"User-Agent": rand_user_agent(), "X-Real-IP": rand_client_ip()}
    ref = rand_referer()
    if ref:
        h["Referer"] = ref
    return h


# ---------------------------------------------------------------------------
# API client — one method per endpoint, mirroring a real consumer.
# ---------------------------------------------------------------------------
@dataclass
class Persona:
    name: str
    email: str
    password: str
    token: str = ""
    user_id: str = ""
    link_ids: list[str] | None = None

    def __post_init__(self) -> None:
        if self.link_ids is None:
            self.link_ids = []


class ApiClient:
    """Synchronous client used for setup, smoke-tests and the demo.

    ``follow_redirects=False`` so the redirect endpoint's 302 is observable.
    A fresh ``X-Real-IP`` is attached to most calls to dodge the per-IP rate
    limiter; pass ``fixed_ip`` to deliberately exercise the limiter.
    """

    def __init__(self, base_url: str = GO_API, gateway: str = GATEWAY, timeout: float = 15.0):
        self.base_url = base_url.rstrip("/")
        self.gateway = gateway.rstrip("/")
        self._c = httpx.Client(timeout=timeout, follow_redirects=False)

    def close(self) -> None:
        self._c.close()

    def __enter__(self) -> "ApiClient":
        return self

    def __exit__(self, *_exc) -> None:
        self.close()

    # -- infra --------------------------------------------------------------
    def healthy(self) -> bool:
        try:
            return self._c.get(f"{self.base_url}/healthz", timeout=5).status_code == 200
        except httpx.HTTPError:
            return False

    def ready(self) -> bool:
        try:
            return self._c.get(f"{self.base_url}/readyz", timeout=5).status_code == 200
        except httpx.HTTPError:
            return False

    # -- auth ---------------------------------------------------------------
    def register(self, email: str, password: str, ip: str | None = None) -> int:
        r = self._c.post(
            f"{self.base_url}/auth/register",
            json={"email": email, "password": password},
            headers={"X-Real-IP": ip or rand_public_ip()},
        )
        return r.status_code

    def login(self, email: str, password: str, ip: str | None = None) -> httpx.Response:
        return self._c.post(
            f"{self.base_url}/auth/login",
            json={"email": email, "password": password},
            headers={"X-Real-IP": ip or rand_public_ip()},
        )

    def register_and_login(self, email: str, password: str) -> str:
        """Idempotent: register (ignore 409) then log in; returns the JWT."""
        self.register(email, password)
        r = self.login(email, password)
        r.raise_for_status()
        return r.json()["token"]

    def logout(self, token: str) -> int:
        return self._c.post(
            f"{self.base_url}/auth/logout",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
        ).status_code

    # -- links --------------------------------------------------------------
    def shorten(self, token: str, url: str, alias: str = "") -> httpx.Response:
        body: dict = {"url": url}
        if alias:
            body["custom_alias"] = alias
        return self._c.post(
            f"{self.base_url}/api/shorten",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
            json=body,
        )

    def shorten_id(self, token: str, url: str, alias: str = "") -> str:
        r = self.shorten(token, url, alias)
        r.raise_for_status()
        return r.json()["id"]

    def list_links(self, token: str, limit: int = 20, offset: int = 0) -> httpx.Response:
        return self._c.get(
            f"{self.base_url}/api/links",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
            params={"limit": limit, "offset": offset},
        )

    def set_active(self, token: str, link_id: str, active: bool) -> int:
        return self._c.patch(
            f"{self.base_url}/api/links/{link_id}",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
            json={"is_active": active},
        ).status_code

    def delete_link(self, token: str, link_id: str) -> int:
        return self._c.delete(
            f"{self.base_url}/api/links/{link_id}",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
        ).status_code

    def redirect(self, link_id: str, headers: dict | None = None) -> tuple[int, str]:
        r = self._c.get(
            f"{self.base_url}/{link_id}",
            headers=headers or {"X-Real-IP": rand_client_ip()},
        )
        return r.status_code, r.headers.get("Location", "")

    def analytics(self, token: str, link_id: str, **params) -> tuple[int, dict]:
        r = self._c.get(
            f"{self.base_url}/api/links/{link_id}/analytics",
            headers={**_auth(token), "X-Real-IP": rand_public_ip()},
            params=params,
        )
        try:
            return r.status_code, r.json()
        except ValueError:
            return r.status_code, {"raw": r.text}

    # -- AI agent (only reachable through nginx) ----------------------------
    def ai_query(self, token: str, question: str) -> tuple[int, dict]:
        r = self._c.post(
            f"{self.gateway}/api/query",
            headers=_auth(token),
            json={"question": question},
            timeout=35.0,
        )
        try:
            return r.status_code, r.json()
        except ValueError:
            return r.status_code, {"detail": r.text}


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


# ---------------------------------------------------------------------------
# Rate-limited concurrency (used by the demo's traffic generator)
# ---------------------------------------------------------------------------
class RateGate:
    """Token-bucket-ish pacing shared across threads to hold a target RPS."""

    def __init__(self, rps: float):
        self._interval = 1.0 / rps if rps > 0 else 0.0
        self._lock = threading.Lock()
        self._next = time.monotonic()

    def wait(self) -> None:
        if self._interval <= 0:
            return
        with self._lock:
            now = time.monotonic()
            t = self._next if self._next > now else now
            self._next = t + self._interval
        delay = t - time.monotonic()
        if delay > 0:
            time.sleep(delay)


# ---------------------------------------------------------------------------
# ClickHouse / Prometheus inspection
# ---------------------------------------------------------------------------
def clickhouse(query: str) -> str:
    """Run a query inside the ClickHouse container; never raises."""
    cmd = ["docker", "exec", CLICKHOUSE_CONTAINER, "clickhouse-client"]
    if CLICKHOUSE_PASSWORD:
        cmd += ["--password", CLICKHOUSE_PASSWORD]
    cmd += ["-q", query]
    try:
        out = subprocess.run(
            cmd, capture_output=True, text=True, timeout=30, check=False,
        )
        return (out.stdout or out.stderr).strip()
    except Exception as e:  # noqa: BLE001 - diagnostics only
        return f"(clickhouse query skipped: {e})"


def prom_scalar(expr: str) -> str:
    try:
        r = httpx.get(f"{PROM_URL}/api/v1/query", params={"query": expr}, timeout=10)
        res = r.json()["data"]["result"]
        return res[0]["value"][1] if res else "0"
    except Exception as e:  # noqa: BLE001 - diagnostics only
        return f"(skipped: {e})"


def prom_float(expr: str, default: float = 0.0) -> float:
    try:
        return float(prom_scalar(expr))
    except (TypeError, ValueError):
        return default


# ---------------------------------------------------------------------------
# Statistics
# ---------------------------------------------------------------------------
def percentile(sorted_values: list[float], q: float) -> float:
    """Linear-interpolated percentile. ``q`` in [0, 1]. Input must be sorted."""
    if not sorted_values:
        return 0.0
    if q <= 0:
        return sorted_values[0]
    if q >= 1:
        return sorted_values[-1]
    pos = q * (len(sorted_values) - 1)
    lo = int(pos)
    frac = pos - lo
    if lo + 1 >= len(sorted_values):
        return sorted_values[lo]
    return sorted_values[lo] * (1 - frac) + sorted_values[lo + 1] * frac


def summarize_latencies(latencies_s: list[float]) -> dict[str, float]:
    """Summary stats (in milliseconds) from a list of latencies in seconds."""
    if not latencies_s:
        return {k: 0.0 for k in ("min", "mean", "p50", "p90", "p95", "p99", "max")}
    ms = sorted(v * 1000.0 for v in latencies_s)
    return {
        "min": ms[0],
        "mean": sum(ms) / len(ms),
        "p50": percentile(ms, 0.50),
        "p90": percentile(ms, 0.90),
        "p95": percentile(ms, 0.95),
        "p99": percentile(ms, 0.99),
        "max": ms[-1],
    }


# ---------------------------------------------------------------------------
# Console presentation
# ---------------------------------------------------------------------------
class _Palette:
    def __init__(self) -> None:
        enabled = os.environ.get("NO_COLOR") is None and sys.stdout.isatty()
        if enabled and os.name == "nt":
            # Best-effort: enable ANSI escape processing on Windows 10+.
            try:
                import ctypes

                kernel32 = ctypes.windll.kernel32
                kernel32.SetConsoleMode(kernel32.GetStdHandle(-11), 7)
            except Exception:  # noqa: BLE001
                pass
        self.enabled = enabled

    def _wrap(self, code: str, text: str) -> str:
        return f"\033[{code}m{text}\033[0m" if self.enabled else text

    def bold(self, t: str) -> str:
        return self._wrap("1", t)

    def green(self, t: str) -> str:
        return self._wrap("32", t)

    def red(self, t: str) -> str:
        return self._wrap("31", t)

    def yellow(self, t: str) -> str:
        return self._wrap("33", t)

    def cyan(self, t: str) -> str:
        return self._wrap("36", t)

    def dim(self, t: str) -> str:
        return self._wrap("2", t)


C = _Palette()


def banner(title: str) -> None:
    line = "═" * 72
    print(f"\n{C.cyan(line)}")
    print(C.bold(f" {title}"))
    print(C.cyan(line))


def section(title: str) -> None:
    print(f"\n{C.bold(C.cyan('▶ ' + title))}")


def check(ok: bool, msg: str) -> bool:
    mark = C.green("✓") if ok else C.red("✗")
    print(f"   {mark} {msg}")
    return ok


def info(msg: str) -> None:
    print(f"   {C.dim('·')} {msg}")


def warn(msg: str) -> None:
    print(f"   {C.yellow('!')} {msg}")


def human_int(n: float) -> str:
    return f"{int(round(n)):,}"


def fmt_ms(ms: float) -> str:
    return f"{ms:,.1f} ms"
