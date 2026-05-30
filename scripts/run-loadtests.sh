#!/usr/bin/env bash
# Run the load-test suite against a running stack. By default both phases run;
# pass a MODE (or set RUN_MODE) to run only one:
#
#   bash scripts/run-loadtests.sh                 # both (default)
#   bash scripts/run-loadtests.sh population      # only loadtest/showcase.py
#   bash scripts/run-loadtests.sh performance     # only loadtest/benchmark.py
#
# Both scripts talk to the Go API on :8080 directly (see loadtest/common.py for
# why), so run this where that port is reachable:
#   - locally, after `docker compose up -d --build`, or
#   - ON the droplet (8080 is bound to 127.0.0.1 there).
#
# Pass extra flags through with env vars, e.g.:
#   SHOWCASE_ARGS="--clicks 8000 --no-ai" BENCH_ARGS="--duration 15" \
#     bash scripts/run-loadtests.sh
set -euo pipefail
cd "$(dirname "$0")/.."   # repo root, so the loadtest/ paths resolve

usage() {
  cat <<'EOF'
Usage: bash scripts/run-loadtests.sh [MODE]

  MODE selects which load-test phase(s) to run (default: both):
    population   (aliases: pop, showcase)    only loadtest/showcase.py
    performance  (aliases: perf, benchmark)  only loadtest/benchmark.py
    both         (alias:   all)              run both phases

  MODE can also be set via the RUN_MODE env var. Forward per-script flags with
  SHOWCASE_ARGS / BENCH_ARGS, e.g.:
    SHOWCASE_ARGS="--clicks 8000 --no-ai" bash scripts/run-loadtests.sh population
EOF
}

# --- 0. which phases to run --------------------------------------------------
MODE="${1:-${RUN_MODE:-both}}"
run_population=false
run_performance=false
case "$MODE" in
  pop|population|showcase)     run_population=true ;;
  perf|performance|benchmark)  run_performance=true ;;
  both|all)                    run_population=true; run_performance=true ;;
  -h|--help|help)              usage; exit 0 ;;
  *)
    echo "ERROR: unknown mode '$MODE'." >&2
    echo >&2
    usage >&2
    exit 2 ;;
esac

# --- 1. uv must be installed -------------------------------------------------
if ! command -v uv >/dev/null 2>&1; then
  echo "ERROR: 'uv' is not installed — it runs the PEP 723 loadtest scripts." >&2
  echo "Install it (https://docs.astral.sh/uv/):" >&2
  echo "  curl -LsSf https://astral.sh/uv/install.sh | sh                 # Linux/macOS" >&2
  echo "  powershell -c \"irm https://astral.sh/uv/install.ps1 | iex\"      # Windows" >&2
  exit 1
fi
echo "==> $(uv --version) found  ·  mode: $MODE"

showcase_rc=0
bench_rc=0

# --- 2. population: showcase.py ---------------------------------------------
if [ "$run_population" = true ]; then
  echo
  echo "================================================================"
  echo "  Population   ·  loadtest/showcase.py"
  echo "================================================================"
  uv run loadtest/showcase.py ${SHOWCASE_ARGS:-} || showcase_rc=$?
fi

# --- 3. performance: benchmark.py -------------------------------------------
if [ "$run_performance" = true ]; then
  echo
  echo "================================================================"
  echo "  Performance  ·  loadtest/benchmark.py"
  echo "================================================================"
  uv run loadtest/benchmark.py ${BENCH_ARGS:-} || bench_rc=$?
fi

# --- 4. summary --------------------------------------------------------------
echo
echo "==> Finished. Reports are under loadtest/results/."
[ "$run_population" = true ]  && echo "    showcase  exit=${showcase_rc}"
[ "$run_performance" = true ] && echo "    benchmark exit=${bench_rc}"
# Non-zero overall status if any phase that ran failed.
[ "$showcase_rc" -eq 0 ] && [ "$bench_rc" -eq 0 ]
