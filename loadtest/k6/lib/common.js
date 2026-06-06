// common.js — shared building blocks for the k6 load-test scenarios.
//
// Mirrors loadtest/common.py: realistic traffic (rotated X-Real-IP across the five
// RIRs so GeoIP resolves many countries, a weighted user-agent mix so the consumer
// fills device/browser), a tiny typed client, and a summary writer. Kept
// dependency-free (no remote jslib imports) so a run is reproducible offline.
//
// Why hit go-api:8080 directly and rotate X-Real-IP? nginx overwrites X-Real-IP
// with the socket address and the limiter is keyed per client IP. Hitting the API
// directly with a fresh IP per request spreads load across ~unique IPs, so the
// per-IP limiter never throttles (each IP makes ~1 request) and country diversity
// is controllable — exactly the trick the Python harness uses.

import http from 'k6/http';
import { fail } from 'k6';

export const cfg = {
  base: (__ENV.BASE_URL || 'http://go-api:8080').replace(/\/+$/, ''),
  rotateIp: (__ENV.ROTATE_IP || 'true') !== 'false',
  seedLinks: Number(__ENV.SEED_LINKS || 50),
  // A fresh identity per run so repeated runs don't collide on the unique email.
  email: __ENV.SEED_EMAIL || `loadtest+${Date.now()}@example.com`,
  password: __ENV.SEED_PASSWORD || 'LoadTest!2026',
};

// First octets spanning all five RIRs — resolves to ~50 countries via GeoLite2.
const OCTETS = [
  3, 4, 8, 13, 18, 23, 34, 52, 63, 66, 72, 96, 104, 108,
  2, 5, 31, 46, 62, 80, 82, 84, 88, 92, 141, 185, 193, 212, 217,
  1, 14, 27, 36, 43, 58, 103, 110, 118, 122, 125, 180, 202, 210, 218,
  177, 181, 186, 190, 200, 41, 102, 154, 196,
];

// (user_agent) mix — the consumer parses these (mssola/useragent) into
// device/browser, so ClickHouse ends up with realistic diversity during a run.
const USER_AGENTS = [
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0',
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
  'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36',
  'Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)',
];

const TARGET_URLS = [
  'https://github.com/NikitaPash/url-shortener',
  'https://en.wikipedia.org/wiki/URL_shortening',
  'https://news.ycombinator.com/',
  'https://stackoverflow.com/questions',
  'https://go.dev/doc/',
  'https://clickhouse.com/docs',
];

export function randInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}
export function randItem(arr) {
  return arr[randInt(0, arr.length - 1)];
}
export function randIp() {
  return `${randItem(OCTETS)}.${randInt(1, 254)}.${randInt(0, 255)}.${randInt(1, 254)}`;
}

// Headers for an anonymous request (redirect): rotated IP + UA so the limiter is
// dodged and the click event carries realistic country/device/browser.
export function ipHeaders(extra) {
  const h = Object.assign({ 'User-Agent': randItem(USER_AGENTS) }, extra || {});
  if (cfg.rotateIp) h['X-Real-IP'] = randIp();
  return h;
}
export function authHeaders(token, extra) {
  return Object.assign(ipHeaders(extra), { Authorization: `Bearer ${token}` });
}

// seedLinks() runs once in setup(): register (ignore 409), log in, create N links.
// Returns { token, shortIds, base } shared with every VU. Uses rotated IPs so the
// auth/API limiter doesn't throttle the seeding itself on the local rig.
export function seedLinks() {
  const json = { 'Content-Type': 'application/json' };
  // responseType:'text' keeps the body for these requests despite the scenarios'
  // global discardResponseBodies:true (kept for the high-RPS redirect phase, whose
  // 302s have no useful body). Without it, login.json('token') / r.json('id') below
  // see a null body and throw.
  const seed = (h) => ({ headers: h, responseType: 'text' });

  http.post(`${cfg.base}/auth/register`, JSON.stringify({ email: cfg.email, password: cfg.password }), seed(ipHeaders(json)));

  const login = http.post(`${cfg.base}/auth/login`, JSON.stringify({ email: cfg.email, password: cfg.password }), seed(ipHeaders(json)));
  if (login.status !== 200) {
    fail(`seed: login failed (status ${login.status}): ${login.body}`);
  }
  const token = login.json('token');

  const shortIds = [];
  for (let i = 0; i < cfg.seedLinks; i++) {
    const r = http.post(`${cfg.base}/api/shorten`, JSON.stringify({ url: randItem(TARGET_URLS) }), seed(authHeaders(token, json)));
    if (r.status === 200 || r.status === 201) {
      shortIds.push(r.json('id'));
    }
  }
  if (shortIds.length === 0) {
    fail('seed: produced no links — is the stack up and the API reachable?');
  }
  console.log(`seed: created ${shortIds.length} links as ${cfg.email}`);
  return { token, shortIds, base: cfg.base };
}

// ---------------------------------------------------------------------------
// Summary writer — JSON to /results plus a compact, report-friendly stdout block.
// Defining handleSummary replaces k6's default text summary, so we render our own
// focused view (throughput, error rate, per-trend p50/p95/p99, key counters).
// ---------------------------------------------------------------------------
export function buildSummary(data, name) {
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const out = {};
  out[`/results/${name}-${stamp}.json`] = JSON.stringify(data, null, 2);
  out['stdout'] = renderSummary(data, name);
  return out;
}

function fmt(n, d = 1) {
  return n === undefined || n === null ? '-' : Number(n).toFixed(d);
}

function renderSummary(data, name) {
  const m = data.metrics || {};
  const lines = [];
  lines.push('');
  lines.push(`──────── ${name} ────────`);

  const reqs = m.http_reqs && m.http_reqs.values;
  if (reqs) {
    lines.push(`requests            ${reqs.count}  (${fmt(reqs.rate, 0)} req/s achieved)`);
  }
  const failed = m.http_req_failed && m.http_req_failed.values;
  if (failed) {
    lines.push(`http_req_failed     ${fmt(failed.rate * 100, 2)} %`);
  }
  const dropped = m.dropped_iterations && m.dropped_iterations.values;
  if (dropped) {
    lines.push(`dropped_iterations  ${dropped.count}  (LG health: should be 0)`);
  }

  // Every Trend metric (latency distributions), incl. our custom probe/redirect ones.
  for (const key of Object.keys(m)) {
    const metric = m[key];
    if (metric.type !== 'trend') continue;
    const v = metric.values;
    lines.push(
      `${key.padEnd(20)}p50 ${fmt(v.med)}  p95 ${fmt(v['p(95)'])}  p99 ${fmt(v['p(99)'])}  max ${fmt(v.max)} ms`,
    );
  }

  // Custom counters (429s, etc.).
  for (const key of Object.keys(m)) {
    const metric = m[key];
    if (metric.type !== 'counter') continue;
    if (['http_reqs', 'dropped_iterations'].includes(key)) continue;
    lines.push(`${key.padEnd(20)}${metric.values.count}`);
  }

  lines.push('───────────────────────────');
  lines.push('');
  return lines.join('\n');
}