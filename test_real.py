"""test_real.py — production-like load generator and smoke-tester for the URL shortener.

Creates three normal user accounts, gives each a mix of auto-generated and custom-alias
short links, fires a large volume of redirect traffic with realistic browser/device/referrer
mix, then verifies every major feature added to the stack:

  • Custom aliases
  • Link deactivation  (redirect → 302 to /app/not-found) and reactivation
  • Link hard-deletion (redirect → 302 to /app/not-found)
  • Analytics API — period presets, custom date range, bot-exclusion toggle
  • Browser breakdown in ClickHouse (new `browser` column)

Usage:
    pip install requests
    python test_real.py                # 150,000 clicks (default)
    python test_real.py 300000         # custom click count
    TARGET_RPS=800 python test_real.py # higher throughput

WHY IT TALKS TO :8080 DIRECTLY (not nginx on :80)
--------------------------------------------------
nginx overwrites X-Real-IP with the connecting address, and the redirect rate limit is
keyed per client IP (100/min). Hitting go-api directly lets us:
  * inject a public IP  → GeoIP resolves a real country (~70% of clicks)
  * inject a private IP → empty country ("direct/unknown") (~30% of clicks)
  * spread every request across a unique IP → no rate-limit throttling
"""

import os
import random
import subprocess
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor

import requests  # pip install requests

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
BASE    = os.environ.get("BASE", "http://localhost:8080")  # direct to go-api
APP_URL = "http://localhost/app/"                           # human-facing UI (nginx)
PROM_URL = "http://localhost:9090"
CLICKHOUSE_CONTAINER = "shortener-clickhouse"

TOTAL_CLICKS = int(sys.argv[1]) if len(sys.argv) > 1 else 150_000
# go-api drains ~700 clicks/s to Kafka (4 workers × 1000-deep queue).
# 600 keeps drops low while finishing 150 k in ~4 min.
TARGET_RPS  = int(os.environ.get("TARGET_RPS",  "600"))
CONCURRENCY = int(os.environ.get("CONCURRENCY", "64"))
PUBLIC_IP_RATIO = 0.70  # fraction of clicks with a public IP → real country

ADMIN_EMAIL    = os.environ.get("ADMIN_EMAIL",    "admin@shorty.local")
ADMIN_PASSWORD = os.environ.get("ADMIN_PASSWORD", "AdminMetrics2026")
GRAFANA_USER   = "admin"
GRAFANA_PASSWORD = os.environ.get("GRAFANA_PASSWORD", "admin")

USERS = [
    {"name": "Alice", "email": "alice@shorty.local", "password": "alicepass123",  "n_links": 4},
    {"name": "Bob",   "email": "bob@shorty.local",   "password": "bobpass123456", "n_links": 3},
    {"name": "Carol", "email": "carol@shorty.local", "password": "carolpass1234", "n_links": 2},
]

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

# Public first-octets spanning all five RIRs — resolves to ~50 countries via GeoLite2.
PUBLIC_OCTETS = [
    3, 4, 8, 9, 12, 13, 17, 18, 23, 24, 34, 40, 45, 52, 63, 64, 66, 68, 72, 74, 96, 104, 108,
    2, 5, 31, 46, 51, 62, 80, 81, 82, 83, 84, 86, 87, 88, 90, 91, 92, 93, 94, 95,
    141, 151, 185, 188, 193, 194, 195, 212, 213, 217,
    1, 14, 27, 36, 39, 42, 43, 49, 58, 59, 60, 61, 103, 106, 110, 111, 112, 113, 114, 116, 118,
    119, 120, 121, 122, 123, 124, 125, 126, 180, 182, 183, 202, 203, 210, 211, 218, 219, 220,
    177, 179, 181, 186, 187, 189, 190, 191, 200, 201,
    41, 102, 105, 154, 196, 197,
]

# (user_agent, weight) — varied browser mix so the new `browser` column has real diversity.
# The consumer parses these via mssola/useragent → browser name stored in ClickHouse.
USER_AGENTS = [
    # Chrome desktop — most common
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", 28),
    # Chrome desktop (Mac)
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", 8),
    # Firefox desktop
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0", 10),
    # Firefox desktop (Mac)
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 14.2; rv:121.0) Gecko/20100101 Firefox/121.0", 4),
    # Safari desktop (Mac) — reports as "Safari"
    ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15", 6),
    # Edge desktop — reports as "Edge"
    ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", 6),
    # Chrome mobile (Android)
    ("Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36", 12),
    # Safari mobile (iPhone)
    ("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", 14),
    # Safari tablet (iPad)
    ("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", 4),
    # Samsung Internet (reports as "Samsung Browser")
    ("Mozilla/5.0 (Linux; Android 13; SM-G998B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0 Mobile Safari/537.36", 3),
    # Googlebot (is_bot=1, excluded from browser breakdown)
    ("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", 3),
    # Bingbot
    ("Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", 2),
]

REFERERS = [
    "", "", "", "",  # direct traffic is most common
    "https://news.ycombinator.com/",
    "https://t.co/",
    "https://www.google.com/",
    "https://www.reddit.com/",
    "https://www.facebook.com/",
    "https://www.linkedin.com/feed/",
    "https://twitter.com/",
    "https://www.producthunt.com/",
]

# Custom aliases to create — demonstrates the custom-alias feature.
CUSTOM_ALIASES = [
    ("github-repo",  "https://github.com/NikitaPash/url-shortener"),
    ("hacker-news",  "https://news.ycombinator.com/"),
    ("go-docs",      "https://go.dev/doc/"),
    ("yt-rickroll",  "https://www.youtube.com/watch?v=dQw4w9WgXcQ"),
]

# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------
_tls = threading.local()


def session() -> requests.Session:
    s = getattr(_tls, "s", None)
    if s is None:
        s = requests.Session()
        _tls.s = s
    return s


def rand_public_ip() -> str:
    return f"{random.choice(PUBLIC_OCTETS)}.{random.randint(1, 254)}.{random.randint(0, 255)}.{random.randint(1, 254)}"


def rand_private_ip() -> str:
    return f"10.{random.randint(0, 255)}.{random.randint(0, 255)}.{random.randint(1, 254)}"


def rand_client_ip() -> str:
    return rand_public_ip() if random.random() < PUBLIC_IP_RATIO else rand_private_ip()


def auth_header(token: str) -> dict:
    return {"Authorization": f"Bearer {token}"}


def get_token(email: str, password: str) -> str:
    ip = {"X-Real-IP": rand_public_ip()}
    session().post(f"{BASE}/auth/register", json={"email": email, "password": password},
                   headers=ip, timeout=10)
    r = session().post(f"{BASE}/auth/login", json={"email": email, "password": password},
                       headers=ip, timeout=10)
    r.raise_for_status()
    return r.json()["token"]


def shorten(token: str, url: str, alias: str = "") -> str:
    body: dict = {"url": url}
    if alias:
        body["custom_alias"] = alias
    r = session().post(f"{BASE}/api/shorten",
                       headers={**auth_header(token), "X-Real-IP": rand_public_ip()},
                       json=body, timeout=10)
    r.raise_for_status()
    return r.json()["id"]


def set_active(token: str, link_id: str, active: bool) -> int:
    r = session().patch(f"{BASE}/api/links/{link_id}",
                        headers={**auth_header(token), "X-Real-IP": rand_public_ip()},
                        json={"is_active": active}, timeout=10)
    return r.status_code


def delete_link(token: str, link_id: str) -> int:
    r = session().delete(f"{BASE}/api/links/{link_id}",
                         headers={**auth_header(token), "X-Real-IP": rand_public_ip()},
                         timeout=10)
    return r.status_code


def redirect_status(link_id: str) -> tuple[int, str]:
    """Return (status_code, Location) without following the redirect."""
    r = session().get(f"{BASE}/{link_id}",
                      headers={"X-Real-IP": rand_public_ip()},
                      allow_redirects=False, timeout=10)
    return r.status_code, r.headers.get("Location", "")


def get_analytics(token: str, link_id: str, **params) -> dict:
    r = session().get(f"{BASE}/api/links/{link_id}/analytics",
                      headers={**auth_header(token), "X-Real-IP": rand_public_ip()},
                      params=params, timeout=15)
    if r.status_code != 200:
        return {"error": r.text}
    return r.json()


# ---------------------------------------------------------------------------
# Rate gate
# ---------------------------------------------------------------------------
class RateGate:
    def __init__(self, rps: int):
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


class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.ok = 0
        self.throttled = 0
        self.errors = 0

    def add(self, ok=0, throttled=0, errors=0):
        with self.lock:
            self.ok += ok
            self.throttled += throttled
            self.errors += errors


# ---------------------------------------------------------------------------
# Click worker
# ---------------------------------------------------------------------------
def do_click(short_id: str, gate: RateGate, stats: Stats) -> None:
    ua = random.choices([u for u, _ in USER_AGENTS], weights=[w for _, w in USER_AGENTS])[0]
    headers = {"User-Agent": ua, "X-Real-IP": rand_client_ip()}
    ref = random.choice(REFERERS)
    if ref:
        headers["Referer"] = ref

    for _ in range(3):
        gate.wait()
        try:
            r = session().get(f"{BASE}/{short_id}", headers=headers,
                              allow_redirects=False, timeout=10)
        except requests.RequestException:
            stats.add(errors=1)
            headers["X-Real-IP"] = rand_client_ip()
            continue
        if r.status_code == 429:
            stats.add(throttled=1)
            headers["X-Real-IP"] = rand_client_ip()
            continue
        stats.add(ok=1)
        return


# ---------------------------------------------------------------------------
# Reporting helpers
# ---------------------------------------------------------------------------
def clickhouse(query: str) -> str:
    try:
        out = subprocess.run(
            ["docker", "exec", CLICKHOUSE_CONTAINER, "clickhouse-client", "-q", query],
            capture_output=True, text=True, timeout=30,
        )
        return (out.stdout or out.stderr).strip()
    except Exception as e:  # noqa: BLE001
        return f"(clickhouse query skipped: {e})"


def prom_scalar(expr: str) -> str:
    try:
        r = requests.get(f"{PROM_URL}/api/v1/query", params={"query": expr}, timeout=10)
        res = r.json()["data"]["result"]
        return res[0]["value"][1] if res else "0"
    except Exception as e:  # noqa: BLE001
        return f"(skipped: {e})"


def banner(title: str) -> None:
    print("\n" + "=" * 70)
    print(title)
    print("=" * 70)


def ok_or_fail(cond: bool, msg: str) -> None:
    print(f"  {'✓' if cond else '✗'} {msg}")


def print_credentials(links_by_user: dict, tokens_by_user: dict = None) -> None:
    banner("CREDENTIALS  —  log in at " + APP_URL)
    print("Normal accounts (own links, no Analytics access):")
    for u in USERS:
        ids = " ".join(links_by_user.get(u["email"], []))
        print(f"  {u['name']:<6} email={u['email']:<24} password={u['password']}")
        if ids:
            print(f"         links: {ids}")
    print("\nAdmin account (can use the Analytics page / AI agent):")
    print(f"  email={ADMIN_EMAIL}  password={ADMIN_PASSWORD}")
    print("\nGrafana / Prometheus / Jaeger:")
    print(f"  Grafana    http://localhost/grafana/    login {GRAFANA_USER} / {GRAFANA_PASSWORD}")
    print("  Prometheus http://localhost/prometheus/  (no login)")
    print("  Jaeger     http://localhost/jaeger/      (no login)")
    print("\nNew Grafana dashboards to check:")
    print("  • Device Breakdown  (device + browser panels)")
    print("  • Browser Breakdown (dedicated browser dashboard)")


# ---------------------------------------------------------------------------
# Feature smoke-tests
# ---------------------------------------------------------------------------
def smoke_custom_aliases(token: str) -> list[str]:
    """Create links with custom aliases; return their IDs."""
    banner("Smoke — custom aliases")
    ids = []
    for alias, url in CUSTOM_ALIASES:
        try:
            sid = shorten(token, url, alias=alias)
            ok_or_fail(sid == alias, f"alias '{alias}' → id={sid}")
            ids.append(sid)
        except requests.HTTPError as e:
            # Alias may already exist from a prior run — that's fine.
            if e.response.status_code == 409:
                print(f"  ~ alias '{alias}' already taken (skip)")
                ids.append(alias)
            else:
                ok_or_fail(False, f"alias '{alias}' → HTTP {e.response.status_code}")
    return ids


def smoke_link_lifecycle(token: str) -> None:
    """Deactivate → verify 302 to /app/not-found → reactivate → delete."""
    banner("Smoke — link deactivation / deletion")

    sid = shorten(token, "https://example.com/lifecycle-test")
    print(f"  created link: {sid}")

    # Active link should redirect to the original URL.
    code, loc = redirect_status(sid)
    ok_or_fail(code == 302 and "example.com" in loc,
               f"active → 302 to target  (got {code} → {loc!r})")

    # Deactivate.
    sc = set_active(token, sid, False)
    ok_or_fail(sc == 204, f"PATCH is_active=false → 204  (got {sc})")

    # Inactive link must redirect to the 404 page.
    code, loc = redirect_status(sid)
    ok_or_fail(code == 302 and loc == "/app/not-found",
               f"inactive → 302 /app/not-found  (got {code} → {loc!r})")

    # Reactivate.
    sc = set_active(token, sid, True)
    ok_or_fail(sc == 204, f"PATCH is_active=true  → 204  (got {sc})")

    # Should redirect normally again.
    code, loc = redirect_status(sid)
    ok_or_fail(code == 302 and "example.com" in loc,
               f"reactivated → 302 to target  (got {code} → {loc!r})")

    # Hard-delete.
    sc = delete_link(token, sid)
    ok_or_fail(sc == 204, f"DELETE → 204  (got {sc})")

    # Deleted link must redirect to the 404 page.
    code, loc = redirect_status(sid)
    ok_or_fail(code == 302 and loc == "/app/not-found",
               f"deleted → 302 /app/not-found  (got {code} → {loc!r})")


def smoke_analytics_api(token: str, link_id: str) -> None:
    """Exercise the analytics API — all new params and verify response shape."""
    banner(f"Smoke — analytics API  (link: {link_id})")

    from datetime import date, timedelta
    today = date.today()
    yesterday = (today - timedelta(days=1)).isoformat()
    week_ago  = (today - timedelta(days=7)).isoformat()

    # 1. Period preset (30 days).
    data = get_analytics(token, link_id, period=30)
    ok_or_fail("total_clicks" in data,     "period=30 → total_clicks present")
    ok_or_fail("unique_visitors" in data,  "period=30 → unique_visitors present")
    ok_or_fail("bot_clicks" in data,       "period=30 → bot_clicks present")
    ok_or_fail("avg_per_day" in data,      "period=30 → avg_per_day present")
    ok_or_fail("previous_period" in data,  "period=30 → previous_period present")
    ok_or_fail("peak_hours" in data,       "period=30 → peak_hours present (24 entries)")
    ok_or_fail(len(data.get("peak_hours", [])) == 24,
               f"peak_hours has 24 entries  (got {len(data.get('peak_hours', []))})")
    ok_or_fail("browsers" in data,         "period=30 → browsers present")
    ok_or_fail("devices" in data,          "period=30 → devices present")
    ok_or_fail("countries" in data,        "period=30 → countries present")
    ok_or_fail("referrers" in data,        "period=30 → referrers present")

    print(f"\n  Stats for last 30 days:")
    print(f"    total_clicks:    {data.get('total_clicks', '?')}")
    print(f"    unique_visitors: {data.get('unique_visitors', '?')}")
    print(f"    bot_clicks:      {data.get('bot_clicks', '?')}")
    print(f"    avg_per_day:     {data.get('avg_per_day', '?'):.2f}" if isinstance(data.get('avg_per_day'), float) else f"    avg_per_day: {data.get('avg_per_day', '?')}")
    browsers = [b["label"] for b in (data.get("browsers") or [])]
    print(f"    browsers seen:   {', '.join(browsers) or '(none yet)'}")
    devices  = [d["label"] for d in (data.get("devices") or [])]
    print(f"    devices seen:    {', '.join(devices) or '(none yet)'}")

    # 2. Custom date range.
    data2 = get_analytics(token, link_id, **{"from": week_ago, "to": yesterday})
    ok_or_fail("total_clicks" in data2 and "error" not in data2,
               f"custom range {week_ago}→{yesterday} → valid response")
    ok_or_fail(len(data2.get("clicks_over_time", [])) == 7,
               f"custom range returns 7-day timeline  (got {len(data2.get('clicks_over_time', []))})")

    # 3. Bot exclusion.
    data_no_bots = get_analytics(token, link_id, period=30, exclude_bots="true")
    data_with_bots = data  # already fetched above
    total_no_bots  = data_no_bots.get("total_clicks", 0)
    total_with_bots = data_with_bots.get("total_clicks", 0)
    bot_count = data_with_bots.get("bot_clicks", 0)
    ok_or_fail(total_no_bots <= total_with_bots,
               f"exclude_bots=true reduces total  ({total_no_bots} ≤ {total_with_bots})")
    ok_or_fail(data_no_bots.get("bot_clicks", -1) == bot_count,
               f"bot_clicks field unchanged by exclude_bots  (still {bot_count})")
    print(f"\n  exclude_bots: total dropped from {total_with_bots} → {total_no_bots} "
          f"(excluded {bot_count} bot clicks)")

    # 4. Invalid date range should return 400.
    bad = get_analytics(token, link_id, **{"from": yesterday, "to": week_ago})
    ok_or_fail("error" in bad, f"reversed date range → error response  ({bad.get('error', '?')!r})")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def main() -> None:
    random.seed()
    print(f"Target: {TOTAL_CLICKS:,} clicks @ ~{TARGET_RPS} rps, "
          f"{CONCURRENCY} workers, {int(PUBLIC_IP_RATIO * 100)}% with a country.")

    # Preflight.
    try:
        if session().get(f"{BASE}/healthz", timeout=5).status_code != 200:
            print(f"go-api not healthy at {BASE}. Is the stack up?  (docker compose up -d)")
            sys.exit(1)
    except requests.RequestException as e:
        print(f"Cannot reach {BASE} — is the stack up?\n  {e}")
        sys.exit(1)

    # Create users + links.
    banner("Creating accounts and links")
    links_by_user: dict[str, list[str]] = {}
    tokens_by_user: dict[str, str] = {}
    weighted_links: list[str] = []

    for u in USERS:
        token = get_token(u["email"], u["password"])
        tokens_by_user[u["email"]] = token
        ids = []
        for _ in range(u["n_links"]):
            ids.append(shorten(token, random.choice(TARGET_URLS)))
        links_by_user[u["email"]] = ids
        for sid in ids:
            weighted_links.extend([sid] * random.randint(1, 10))
        print(f"  {u['name']}: {len(ids)} auto links → {' '.join(ids)}")

    # Verify the admin account exists.
    try:
        r = session().post(f"{BASE}/auth/login",
                           json={"email": ADMIN_EMAIL, "password": ADMIN_PASSWORD},
                           headers={"X-Real-IP": rand_public_ip()}, timeout=10)
        admin_ok = r.status_code == 200
    except requests.RequestException:
        admin_ok = False
    if admin_ok:
        print(f"  admin login OK ({ADMIN_EMAIL})")
    else:
        print(f"  WARNING: admin login FAILED for {ADMIN_EMAIL}. "
              "Set ADMIN_EMAIL/ADMIN_PASSWORD in .env and restart go-api.")

    # Pick Alice's token for smoke-tests (she has the most links).
    alice_token = tokens_by_user[USERS[0]["email"]]

    # Smoke-test: custom aliases.
    alias_ids = smoke_custom_aliases(alice_token)
    # Add aliases to the click pool too so they accumulate some analytics data.
    for aid in alias_ids:
        weighted_links.extend([aid] * random.randint(3, 8))
        links_by_user[USERS[0]["email"]].append(aid)

    # Smoke-test: link lifecycle (deactivate / reactivate / delete).
    smoke_link_lifecycle(alice_token)

    print_credentials(links_by_user, tokens_by_user)

    # Fire the load.
    banner(f"Generating {TOTAL_CLICKS:,} clicks")
    gate  = RateGate(TARGET_RPS)
    stats = Stats()
    start = time.monotonic()
    done  = 0
    with ThreadPoolExecutor(max_workers=CONCURRENCY) as pool:
        futures = []
        for _ in range(TOTAL_CLICKS):
            futures.append(pool.submit(do_click, random.choice(weighted_links), gate, stats))
            if len(futures) >= 20_000:
                for f in futures:
                    f.result()
                futures = []
                done += 20_000
                elapsed = time.monotonic() - start
                print(f"  {done:,}/{TOTAL_CLICKS:,}  ok={stats.ok:,} "
                      f"throttled={stats.throttled} err={stats.errors}  "
                      f"({done / elapsed:,.0f} rps)")
        for f in futures:
            f.result()
    elapsed = time.monotonic() - start
    print(f"  sent {stats.ok:,} clicks in {elapsed:,.0f}s "
          f"({stats.ok / elapsed:,.0f} rps); "
          f"throttled={stats.throttled} errors={stats.errors}")

    # Wait for the Kafka consumer to flush to ClickHouse.
    print("  waiting 12s for the Kafka consumer to flush to ClickHouse...")
    time.sleep(12)

    # ClickHouse results.
    banner("Results in ClickHouse")
    print("Total clicks stored:\n  " + clickhouse("SELECT count() FROM shortener.clicks"))

    print("\nClicks per short_id (top 10):\n" + clickhouse(
        "SELECT short_id, count() AS clicks "
        "FROM shortener.clicks GROUP BY short_id ORDER BY clicks DESC LIMIT 10"))

    print("\nTop 15 countries:\n" + clickhouse(
        "SELECT if(country='','(none)',country) AS country, count() AS clicks "
        "FROM shortener.clicks GROUP BY country ORDER BY clicks DESC LIMIT 15"))

    print("\nDevice breakdown:\n" + clickhouse(
        "SELECT device, count() AS clicks, "
        "round(count() * 100 / (SELECT count() FROM shortener.clicks), 1) AS pct "
        "FROM shortener.clicks GROUP BY device ORDER BY clicks DESC"))

    print("\nBrowser breakdown (bots excluded):\n" + clickhouse(
        "SELECT if(browser='','(unknown)',browser) AS browser, count() AS clicks, "
        "round(count() * 100 / (SELECT count() FROM shortener.clicks WHERE is_bot=0), 1) AS pct "
        "FROM shortener.clicks WHERE is_bot = 0 GROUP BY browser ORDER BY clicks DESC LIMIT 12"))

    print("\nBot vs human:\n" + clickhouse(
        "SELECT if(is_bot=1,'bot','human') AS type, count() AS clicks "
        "FROM shortener.clicks GROUP BY is_bot ORDER BY is_bot DESC"))

    print("\nUnique visitors (distinct IPs) per link (top 5):\n" + clickhouse(
        "SELECT short_id, uniq(ip) AS unique_ips "
        "FROM shortener.clicks GROUP BY short_id ORDER BY unique_ips DESC LIMIT 5"))

    print("\nPeak hours (UTC, all links):\n" + clickhouse(
        "SELECT toHour(timestamp) AS hour, count() AS clicks "
        "FROM shortener.clicks GROUP BY hour ORDER BY hour ASC"))

    print("\nTop referrers:\n" + clickhouse(
        "SELECT if(referrer='','(direct)',referrer) AS referrer, count() AS clicks "
        "FROM shortener.clicks GROUP BY referrer ORDER BY clicks DESC LIMIT 8"))

    # Prometheus.
    banner("Prometheus counters (go-api)")
    print(f"  redirects served:  {prom_scalar('shortener_redirects_total')}")
    print(f"  kafka published:   {prom_scalar('shortener_kafka_published_total')}")
    print(f"  clicks dropped:    {prom_scalar('shortener_clicks_dropped_total')}"
          "  (>0 means we outran the publish queue — lower TARGET_RPS to reduce)")
    print(f"  cache hits:        {prom_scalar('shortener_cache_hits_total')}")
    print(f"  cache misses:      {prom_scalar('shortener_cache_misses_total')}")
    p95_expr = 'histogram_quantile(0.95, rate(http_server_request_duration_seconds_bucket{job="go-api"}[5m]))'
    print(f"  p95 latency:       {prom_scalar(p95_expr)}s")

    # Analytics API smoke-test (after load so there's real data).
    # Pick the hottest link belonging to Alice.
    alice_links = links_by_user.get(USERS[0]["email"], [])
    if alice_links:
        smoke_analytics_api(alice_token, alice_links[0])
    else:
        print("\n(skipping analytics smoke-test — no links found for Alice)")

    print_credentials(links_by_user, tokens_by_user)
    print()
    print("Tip: open Grafana and check the 'Device Breakdown' and 'Browser Breakdown' dashboards.")
    print("     Log in to the app as Alice to explore per-link analytics charts.")


if __name__ == "__main__":
    main()
