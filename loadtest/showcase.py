#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "httpx>=0.27",
# ]
# ///
"""showcase.py — guided end-to-end feature demonstration for the URL shortener.

A polished successor to ``test_real.py``, written to be shown as part of a
bachelor's thesis. It walks the entire system in labelled sections, asserting
the expected behaviour at each step and printing a clean pass/fail transcript,
then writes a Markdown report (under ``loadtest/results/``, git/Docker-ignored)
that can be dropped straight into an appendix.

It demonstrates every major feature of the stack:

  1. Health & readiness probes
  2. Accounts & JWT auth (register / login, plus the seeded admin)
  3. Short-link creation — auto codes and custom aliases (with collision handling)
  4. Listing & pagination
  5. Realistic redirect traffic (weighted browser / device / country / referrer mix)
  6. Link lifecycle — deactivate → 302 /app/not-found → reactivate → delete
  7. Per-link analytics API — period presets, custom range, bot exclusion, validation
  8. AI analytics agent — natural-language → validated, user-scoped SQL (incl. an
     off-topic rejection and a concrete per-user data-isolation check)
  9. Security — per-IP rate limiting and JWT logout revocation (denylist)
 10. Observability — live Prometheus counters + the ClickHouse data landscape

Usage (uv resolves dependencies automatically):

    uv run loadtest/showcase.py
    uv run loadtest/showcase.py --clicks 8000
    uv run loadtest/showcase.py --no-ai            # skip the Gemini-backed section
    uv run loadtest/showcase.py --clicks 0 --no-report

Set ADMIN_EMAIL / ADMIN_PASSWORD (matching your .env) to verify the admin login.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import random
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from datetime import date, datetime, timedelta, timezone
from pathlib import Path

import httpx

import common as cm

RESULTS_ROOT = Path(__file__).resolve().parent / "results"

ADMIN_EMAIL = os.environ.get("ADMIN_EMAIL", "admin@example.com")
ADMIN_PASSWORD = os.environ.get("ADMIN_PASSWORD", "adminpassword")
GRAFANA_PASSWORD = os.environ.get("GRAFANA_PASSWORD", "admin")

PERSONAS = [
    {"name": "Alice", "email": "alice@demo.local", "password": "alicepass123", "n_links": 5},
    {"name": "Bob", "email": "bob@demo.local", "password": "bobpass123456", "n_links": 3},
    {"name": "Carol", "email": "carol@demo.local", "password": "carolpass1234", "n_links": 2},
]

CUSTOM_ALIASES = [
    ("thesis-repo", "https://github.com/NikitaPash/url-shortener"),
    ("hacker-news", "https://news.ycombinator.com/"),
    ("go-docs", "https://go.dev/doc/"),
    ("rickroll", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"),
]

AI_QUESTIONS = [
    "How many clicks did I get in the last 7 days?",
    "Which of my links got the most clicks?",
    "What countries are my visitors from?",
    "What percentage of my traffic is on mobile?",
    "Where is my traffic coming from?",
    "How many of my clicks came from bots?",
]
AI_OFFTOPIC = "What is the capital of France?"


# ---------------------------------------------------------------------------
# Report collector
# ---------------------------------------------------------------------------
@dataclass
class Report:
    results: list[tuple[str, bool, str]] = field(default_factory=list)
    facts: list[tuple[str, str]] = field(default_factory=list)
    ai_log: list[dict] = field(default_factory=list)
    skipped: list[str] = field(default_factory=list)
    _section: str = "general"

    def section(self, title: str) -> None:
        self._section = title
        cm.section(title)

    def check(self, ok: bool, msg: str) -> bool:
        cm.check(ok, msg)
        self.results.append((self._section, bool(ok), msg))
        return bool(ok)

    def fact(self, label: str, value) -> None:
        self.facts.append((label, str(value)))

    def skip(self, msg: str) -> None:
        cm.warn(msg)
        self.skipped.append(f"{self._section}: {msg}")

    @property
    def passed(self) -> int:
        return sum(1 for _, ok, _ in self.results if ok)

    @property
    def total(self) -> int:
        return len(self.results)


@dataclass
class Person:
    name: str
    email: str
    password: str
    token: str = ""
    user_id: str = ""
    links: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def jwt_sub(token: str) -> str:
    """Best-effort decode of the JWT 'sub' (user id) claim without verifying."""
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload)).get("sub", "")
    except Exception:  # noqa: BLE001
        return ""


def generate_traffic(base_url: str, weighted_links: list[str], total: int,
                     rps: float, concurrency: int) -> dict[str, int]:
    """Fire `total` realistic redirects across many IPs at a paced RPS."""
    gate = cm.RateGate(rps)
    stats = {"ok": 0, "throttled": 0, "errors": 0}
    lock = threading.Lock()
    limits = httpx.Limits(max_connections=concurrency + 10, max_keepalive_connections=concurrency + 10)
    client = httpx.Client(timeout=10, follow_redirects=False, limits=limits)

    def one(sid: str) -> None:
        for _ in range(3):  # retry on throttle/error with a fresh IP
            gate.wait()
            try:
                r = client.get(f"{base_url}/{sid}", headers=cm.click_headers())
            except httpx.HTTPError:
                with lock:
                    stats["errors"] += 1
                continue
            if r.status_code == 429:
                with lock:
                    stats["throttled"] += 1
                continue
            with lock:
                stats["ok"] += 1
            return

    targets = [random.choice(weighted_links) for _ in range(total)]
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        list(pool.map(one, targets))
    client.close()
    return stats


# ---------------------------------------------------------------------------
# Sections
# ---------------------------------------------------------------------------
def sec_preflight(api: cm.ApiClient, rep: Report) -> bool:
    rep.section("1 · Health & readiness")
    healthy = api.healthy()
    rep.check(healthy, "GET /healthz → 200 (liveness)")
    if not healthy:
        return False
    rep.check(api.ready(), "GET /readyz → 200 (Postgres + Redis reachable)")
    return True


def sec_accounts(api: cm.ApiClient, rep: Report) -> list[Person]:
    rep.section("2 · Accounts & JWT authentication")
    people: list[Person] = []
    for spec in PERSONAS:
        p = Person(name=spec["name"], email=spec["email"], password=spec["password"])
        token = api.register_and_login(p.email, p.password)
        p.token = token
        p.user_id = jwt_sub(token)
        rep.check(bool(token), f"{p.name}: register + login → JWT issued")
        people.append(p)

    # Wrong password must be rejected.
    bad = api.login(people[0].email, "definitely-wrong-password")
    rep.check(bad.status_code == 401, "login with wrong password → 401")

    # Seeded admin (gates the Admin Panel).
    admin = api.login(ADMIN_EMAIL, ADMIN_PASSWORD)
    if admin.status_code == 200:
        rep.check(True, f"seeded admin login OK ({ADMIN_EMAIL})")
    else:
        rep.skip(f"admin login failed for {ADMIN_EMAIL} — set ADMIN_EMAIL/ADMIN_PASSWORD to match your .env")
    return people


def sec_links(api: cm.ApiClient, rep: Report, people: list[Person]) -> None:
    rep.section("3 · Short-link creation (auto codes + custom aliases)")
    for spec, p in zip(PERSONAS, people):
        for _ in range(spec["n_links"]):
            try:
                p.links.append(api.shorten_id(p.token, random.choice(cm.TARGET_URLS)))
            except httpx.HTTPError:
                pass
        rep.check(len(p.links) == spec["n_links"],
                  f"{p.name}: created {len(p.links)} auto-generated short links")

    alice = people[0]
    created_alias = 0
    for alias, url in CUSTOM_ALIASES:
        r = api.shorten(alice.token, url, alias=alias)
        if r.status_code == 201 and r.json().get("id") == alias:
            created_alias += 1
            alice.links.append(alias)
        elif r.status_code == 409:
            alice.links.append(alias)  # already exists from a prior run
            cm.info(f"alias '{alias}' already taken (reused)")
    rep.check(created_alias > 0 or any(a in alice.links for a, _ in CUSTOM_ALIASES),
              f"custom aliases available ({created_alias} newly created)")

    # Pagination over Alice's links.
    rep.section("4 · Listing & pagination")
    r1 = api.list_links(alice.token, limit=4, offset=0)
    r2 = api.list_links(alice.token, limit=4, offset=4)
    ok = r1.status_code == 200 and r2.status_code == 200
    rep.check(ok, "GET /api/links with limit/offset → 200")
    if ok:
        total = r1.json().get("total", 0)
        page1 = len(r1.json().get("links", []))
        page2 = len(r2.json().get("links", []))
        rep.check(total >= len(alice.links),
                  f"reported total ({total}) covers Alice's links; page1={page1}, page2={page2}")
        rep.fact("Alice link count", total)


def sec_traffic(rep: Report, base_url: str, people: list[Person], clicks: int,
                rps: float, concurrency: int) -> None:
    rep.section("5 · Realistic redirect traffic")
    if clicks <= 0:
        rep.skip("traffic generation disabled (--clicks 0)")
        return
    weighted: list[str] = []
    for p in people:
        for sid in p.links:
            weighted.extend([sid] * random.randint(1, 8))
    if not weighted:
        rep.skip("no links to drive traffic to")
        return
    cm.info(f"firing {clicks:,} clicks at ~{rps:.0f} rps across {len(set(weighted))} links "
            f"(mixed browsers / devices / countries / referrers)")
    t0 = time.monotonic()
    stats = generate_traffic(base_url, weighted, clicks, rps, concurrency)
    dt = max(time.monotonic() - t0, 0.001)
    rep.check(stats["ok"] > 0,
              f"served {stats['ok']:,} redirects in {dt:.1f}s "
              f"({stats['ok'] / dt:,.0f} rps; throttled={stats['throttled']}, errors={stats['errors']})")
    rep.fact("Clicks generated", f"{stats['ok']:,}")
    cm.info("waiting 12s for the Kafka → consumer → ClickHouse pipeline to flush…")
    time.sleep(12)


def sec_lifecycle(api: cm.ApiClient, rep: Report, actor: Person) -> None:
    rep.section("6 · Link lifecycle (deactivate / reactivate / delete)")
    sid = api.shorten_id(actor.token, "https://example.com/lifecycle-demo")
    cm.info(f"created throwaway link: {sid}")

    code, loc = api.redirect(sid)
    rep.check(code == 302 and "example.com" in loc, f"active link → 302 to target (got {code})")

    rep.check(api.set_active(actor.token, sid, False) == 204, "PATCH is_active=false → 204")
    code, loc = api.redirect(sid)
    rep.check(code == 302 and loc == "/app/not-found", f"deactivated → 302 /app/not-found (got {code} → {loc!r})")

    rep.check(api.set_active(actor.token, sid, True) == 204, "PATCH is_active=true → 204")
    code, loc = api.redirect(sid)
    rep.check(code == 302 and "example.com" in loc, "reactivated → 302 to target")

    rep.check(api.delete_link(actor.token, sid) == 204, "DELETE → 204")
    code, loc = api.redirect(sid)
    rep.check(code == 302 and loc == "/app/not-found", "deleted → 302 /app/not-found")


def sec_analytics(api: cm.ApiClient, rep: Report, actor: Person) -> None:
    rep.section("7 · Per-link analytics API")
    if not actor.links:
        rep.skip("no link to query analytics for")
        return
    link_id = actor.links[0]
    today = date.today()
    week_ago = (today - timedelta(days=7)).isoformat()
    yesterday = (today - timedelta(days=1)).isoformat()

    status, data = api.analytics(actor.token, link_id, period=30)
    if status == 503:
        rep.skip("analytics endpoint returned 503 (ClickHouse reader not configured)")
        return
    expected_keys = ["total_clicks", "unique_visitors", "bot_clicks", "avg_per_day",
                     "previous_period", "peak_hours", "browsers", "devices", "countries", "referrers"]
    rep.check(status == 200 and all(k in data for k in expected_keys),
              "period=30 → full analytics payload (totals, peak_hours, breakdowns)")
    rep.check(len(data.get("peak_hours", [])) == 24, "peak_hours has 24 hourly buckets")

    cm.info(f"30-day stats for {link_id}: total={data.get('total_clicks')}, "
            f"unique={data.get('unique_visitors')}, bots={data.get('bot_clicks')}")
    browsers = ", ".join(b["label"] for b in (data.get("browsers") or [])) or "(none yet)"
    countries = ", ".join(c["label"] for c in (data.get("countries") or [])[:8]) or "(none yet)"
    cm.info(f"browsers: {browsers}")
    cm.info(f"countries: {countries}")
    rep.fact(f"Top countries ({link_id})", countries)

    status2, data2 = api.analytics(actor.token, link_id, **{"from": week_ago, "to": yesterday})
    rep.check(status2 == 200 and len(data2.get("clicks_over_time", [])) == 7,
              f"custom range {week_ago}→{yesterday} → 7-day timeline")

    _, no_bots = api.analytics(actor.token, link_id, period=30, exclude_bots="true")
    rep.check(no_bots.get("total_clicks", 0) <= data.get("total_clicks", 0),
              f"exclude_bots=true reduces total "
              f"({no_bots.get('total_clicks')} ≤ {data.get('total_clicks')})")

    status_bad, _ = api.analytics(actor.token, link_id, **{"from": yesterday, "to": week_ago})
    rep.check(status_bad == 400, "reversed date range → 400 validation error")


def sec_ai(api: cm.ApiClient, rep: Report, people: list[Person]) -> None:
    rep.section("8 · AI analytics agent (natural language → SQL)")
    alice, bob = people[0], people[1]

    # Probe availability first (handles missing GEMINI_API_KEY / rate limit gracefully).
    status, body = api.ai_query(alice.token, AI_QUESTIONS[0])
    if status == 429:
        time.sleep(3)
        status, body = api.ai_query(alice.token, AI_QUESTIONS[0])
    if status != 200:
        detail = body.get("detail") or body.get("error") or body
        rep.skip(f"AI agent unavailable (HTTP {status}: {detail}) — set GEMINI_API_KEY to enable")
        return

    def ask(person: Person, question: str) -> dict | None:
        st, resp = api.ai_query(person.token, question)
        if st == 429:
            time.sleep(3)
            st, resp = api.ai_query(person.token, question)
        if st != 200:
            return None
        return resp

    # Show the first probe result, then the rest.
    questions = AI_QUESTIONS
    answered = 0
    for i, q in enumerate(questions):
        resp = body if i == 0 else ask(alice, q)
        if not resp:
            continue
        answered += 1
        answer = resp["data"][0] if resp.get("data") else []
        cm.info(f"Q: {q}")
        cm.info(f"   ↳ {resp.get('explanation', '').strip()}")
        cm.info(f"   ↳ SQL: {resp.get('sql', '').strip()[:110]}…")
        cm.info(f"   ↳ answer: {answer}")
        rep.ai_log.append({"user": alice.name, "question": q, "sql": resp.get("sql", ""),
                          "explanation": resp.get("explanation", ""), "answer": answer})
        time.sleep(0.4)  # stay under nginx's 20 req/min/IP
    rep.check(answered >= 1, f"agent answered {answered}/{len(questions)} analytics questions with SQL")

    # Off-topic must be rejected.
    st_off, _ = api.ai_query(alice.token, AI_OFFTOPIC)
    rep.check(st_off == 400, f"off-topic question ('{AI_OFFTOPIC}') → 400 rejected")

    # Data isolation: each user's generated SQL is scoped to their own user_id.
    time.sleep(0.4)
    a = ask(alice, "How many total clicks do I have?")
    time.sleep(0.4)
    b = ask(bob, "How many total clicks do I have?")
    if a and b and alice.user_id and bob.user_id:
        a_sql, b_sql = a.get("sql", ""), b.get("sql", "")
        isolated = (
            alice.user_id in a_sql
            and bob.user_id in b_sql
            and alice.user_id != bob.user_id
            and alice.user_id not in b_sql
        )
        rep.check(isolated, "data isolation: each query is filtered to the caller's own user_id")
        rep.ai_log.append({"user": "isolation-check",
                          "question": "same question, two users",
                          "sql": f"Alice→ …user_id='{alice.user_id[:8]}…'  |  Bob→ …user_id='{bob.user_id[:8]}…'",
                          "explanation": "Generated SQL is scoped per authenticated user.",
                          "answer": []})
    else:
        rep.skip("could not complete the data-isolation check (agent throttled)")


def sec_security(api: cm.ApiClient, rep: Report, base_url: str, gateway: str) -> None:
    rep.section("9 · Security — rate limiting & JWT revocation")

    # Rate limiting: hammer the auth limiter from a single fixed IP.
    fixed_ip = "203.0.113.7"
    throttled = 0
    attempts = 15
    for _ in range(attempts):
        r = api.login("nobody@ratelimit.test", "x", ip=fixed_ip)
        if r.status_code == 429:
            throttled += 1
    rep.check(throttled > 0,
              f"per-IP rate limit engaged: {throttled}/{attempts} rapid logins from one IP → 429")

    # JWT logout denylist: a logged-out token is rejected on protected routes.
    temp = cm.ApiClient(base_url=base_url, gateway=gateway)
    tok = temp.register_and_login(f"denylist-{random.randint(1000, 9999)}@demo.local", "denylistpass1")
    before = temp.list_links(tok).status_code
    rep.check(before == 200, "fresh token → 200 on protected /api/links")
    rep.check(temp.logout(tok) == 200, "POST /auth/logout → 200")
    after = temp.list_links(tok).status_code
    rep.check(after == 401, f"revoked token → 401 (Redis denylist enforced; got {after})")
    temp.close()


def sec_observability(rep: Report) -> None:
    rep.section("10 · Observability")
    redirects = cm.prom_scalar("shortener_redirects_total")
    published = cm.prom_scalar("shortener_kafka_published_total")
    dropped = cm.prom_scalar("shortener_clicks_dropped_total")
    hits = cm.prom_float("shortener_cache_hits_total")
    misses = cm.prom_float("shortener_cache_misses_total")
    ratio = hits / (hits + misses) * 100 if (hits + misses) else 0.0
    cm.info(f"redirects_total      = {redirects}")
    cm.info(f"kafka_published_total= {published}")
    cm.info(f"clicks_dropped_total = {dropped}")
    cm.info(f"cache hit ratio      = {ratio:.1f}%")
    rep.check(not str(redirects).startswith("(skipped"),
              "Prometheus is scraping the Go API (counters readable)")
    rep.fact("Prometheus redirects_total", redirects)
    rep.fact("Cache hit ratio", f"{ratio:.1f}%")

    # ClickHouse data landscape (best-effort; needs docker access).
    total = cm.clickhouse("SELECT count() FROM shortener.clicks")
    countries = cm.clickhouse("SELECT uniqExact(country) FROM shortener.clicks WHERE country != ''")
    if not total.startswith("("):
        cm.info(f"ClickHouse clicks stored = {total}; distinct countries = {countries}")
        rep.fact("ClickHouse clicks stored", total)
        rep.fact("Distinct countries", countries)


# ---------------------------------------------------------------------------
# Final output
# ---------------------------------------------------------------------------
def print_credentials(people: list[Person]) -> None:
    cm.banner("CREDENTIALS — log in at " + cm.APP_URL)
    print("  Demo accounts (own links; AI Analytics open to all logged-in users):")
    for p in people:
        print(f"    {p.name:<6} {p.email:<20} {p.password}")
        if p.links:
            print(f"           links: {' '.join(p.links)}")
    print(f"\n  Seeded admin (gates the Admin Panel):  {ADMIN_EMAIL} / {ADMIN_PASSWORD}")
    print("\n  Dashboards & tracing:")
    print(f"    Grafana     {cm.GATEWAY}/grafana/     (admin / {GRAFANA_PASSWORD})")
    print(f"    Prometheus  {cm.GATEWAY}/prometheus/")
    print(f"    Jaeger      {cm.GATEWAY}/jaeger/")


def write_report(rep: Report, people: list[Person], cfg: dict) -> Path:
    RESULTS_ROOT.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    path = RESULTS_ROOT / f"showcase_{stamp}.md"
    started = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    lines = [f"# URL Shortener — feature showcase report", f"\n_Generated {started}_\n"]
    lines.append(f"**Checks passed:** {rep.passed}/{rep.total}")
    if rep.skipped:
        lines.append(f"  ·  **Skipped:** {len(rep.skipped)}")
    lines.append("\n## Configuration\n")
    for k, v in cfg.items():
        lines.append(f"- {k}: `{v}`")

    # Group checks by section.
    lines.append("\n## Verified behaviour\n")
    current = None
    for sect, ok, msg in rep.results:
        if sect != current:
            lines.append(f"\n**{sect}**\n")
            current = sect
        lines.append(f"- {'✅' if ok else '❌'} {msg}")

    if rep.skipped:
        lines.append("\n## Skipped\n")
        for s in rep.skipped:
            lines.append(f"- ⚠️ {s}")

    if rep.ai_log:
        lines.append("\n## AI agent transcript (natural language → SQL)\n")
        for entry in rep.ai_log:
            lines.append(f"- **Q ({entry['user']}):** {entry['question']}")
            if entry.get("explanation"):
                lines.append(f"  - {entry['explanation']}")
            if entry.get("sql"):
                lines.append(f"  - `SQL:` `{entry['sql'].strip()}`")
            if entry.get("answer"):
                lines.append(f"  - `answer:` `{entry['answer']}`")

    if rep.facts:
        lines.append("\n## Key figures\n")
        for label, value in rep.facts:
            lines.append(f"- {label}: **{value}**")

    lines.append("\n## Demo accounts\n")
    for p in people:
        lines.append(f"- {p.name} — `{p.email}` / `{p.password}` ({len(p.links)} links)")

    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


# ---------------------------------------------------------------------------
# Orchestration
# ---------------------------------------------------------------------------
def run(args) -> int:
    base_url = args.base or cm.GO_API
    gateway = args.gateway or cm.GATEWAY
    random.seed()

    cm.banner("URL Shortener — Feature Showcase")
    cm.info(f"API {base_url}  |  gateway {gateway}  |  {args.clicks:,} demo clicks")

    rep = Report()
    api = cm.ApiClient(base_url=base_url, gateway=gateway)

    if not sec_preflight(api, rep):
        print(cm.C.red(f"\n  go-api is not healthy at {base_url}. Is the stack up? (docker compose up -d)\n"))
        api.close()
        return 1

    people = sec_accounts(api, rep)
    sec_links(api, rep, people)
    sec_traffic(rep, base_url, people, args.clicks, args.rps, args.concurrency)
    sec_lifecycle(api, rep, people[0])
    sec_analytics(api, rep, people[0])
    if not args.no_ai:
        sec_ai(api, rep, people)
    else:
        rep.section("8 · AI analytics agent")
        rep.skip("skipped via --no-ai")
    sec_security(api, rep, base_url, gateway)
    sec_observability(rep)

    api.close()

    # Summary + credentials.
    cm.banner("Summary")
    colour = cm.C.green if rep.passed == rep.total else cm.C.yellow
    print("   " + colour(cm.C.bold(f"{rep.passed}/{rep.total} checks passed")) +
          (f"  ·  {len(rep.skipped)} skipped" if rep.skipped else ""))
    if rep.passed != rep.total:
        for sect, ok, msg in rep.results:
            if not ok:
                print(f"   {cm.C.red('✗')} [{sect}] {msg}")

    print_credentials(people)

    if not args.no_report:
        cfg = {"api": base_url, "gateway": gateway, "clicks": args.clicks,
               "rps": args.rps, "concurrency": args.concurrency}
        path = write_report(rep, people, cfg)
        rel = path.relative_to(Path(__file__).resolve().parent.parent)
        print(f"\n   {cm.C.green('✓')} report written to {cm.C.bold(str(rel))}")

    print(f"\n   {cm.C.dim('Tip: open ' + cm.APP_URL + ' as Alice, then explore Grafana → Device/Browser dashboards.')}\n")
    return 0 if rep.passed == rep.total else 2


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Guided feature showcase for the URL shortener.")
    p.add_argument("--base", default=None, help=f"Go API base URL (default {cm.GO_API})")
    p.add_argument("--gateway", default=None, help=f"nginx gateway URL (default {cm.GATEWAY})")
    p.add_argument("--clicks", type=int, default=3000, help="demo redirect clicks to generate (default 3000; 0 to skip)")
    p.add_argument("--rps", type=float, default=400.0, help="target clicks/sec for traffic (default 400)")
    p.add_argument("--concurrency", type=int, default=32, help="worker threads for traffic (default 32)")
    p.add_argument("--no-ai", action="store_true", help="skip the Gemini-backed AI agent section")
    p.add_argument("--no-report", action="store_true", help="do not write the Markdown report")
    return p.parse_args(argv)


def main() -> None:
    args = parse_args(sys.argv[1:])
    try:
        sys.exit(run(args))
    except KeyboardInterrupt:
        print("\nInterrupted.")
        sys.exit(130)


if __name__ == "__main__":
    main()
