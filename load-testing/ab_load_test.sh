#!/usr/bin/env bash
# =============================================================================
# ab_load_test.sh — Apache Benchmark load tests for mock-gateway
#
# Requirements: apache2-utils (apt install apache2-utils)
#               jq (apt install jq)
#
# Usage:
#   chmod +x ab_load_test.sh
#   ./ab_load_test.sh [HOST]          # default host: http://localhost:8080
#   ./ab_load_test.sh http://prod:8080
# =============================================================================

set -euo pipefail

HOST="${1:-http://localhost:8080}"
RESULTS_DIR="./ab-results"
mkdir -p "$RESULTS_DIR"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# ─── colour helpers ───────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${CYAN}[INFO]${NC} $*"; }
section() { echo -e "\n${YELLOW}══════════════════════════════════════════${NC}"; \
             echo -e "${YELLOW}  $*${NC}"; \
             echo -e "${YELLOW}══════════════════════════════════════════${NC}"; }

# ─── check dependencies ───────────────────────────────────────────────────────
for cmd in ab jq curl; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: '$cmd' not found. Install with: apt install apache2-utils jq curl"
    exit 1
  fi
done

# ─── wait for gateway to be ready ────────────────────────────────────────────
info "Waiting for gateway at $HOST/health ..."
for i in $(seq 1 10); do
  if curl -sf "$HOST/health" >/dev/null 2>&1; then
    info "Gateway is ready."
    break
  fi
  sleep 2
  if [[ $i -eq 10 ]]; then
    echo "ERROR: Gateway did not respond after 20 s. Is it running?"
    exit 1
  fi
done

# ─── create request body file ─────────────────────────────────────────────────
BODY_FILE=$(mktemp /tmp/ab_notify_XXXX.json)
cat > "$BODY_FILE" <<'EOF'
{"idempotency_key":"ab-test-fixed-key","channel":"email","recipient":"loadtest@clinic.example","message":"Appointment confirmed tomorrow at 10:00"}
EOF

run_ab() {
  local label="$1"   # test name
  local url="$2"     # full URL
  local n="$3"       # total requests
  local c="$4"       # concurrency
  local method="${5:-GET}"
  local out="$RESULTS_DIR/${TIMESTAMP}_${label}.txt"

  info "Running: $label  (n=$n c=$c method=$method)"

  local extra_args=()
  if [[ "$method" == "POST" ]]; then
    extra_args+=(-p "$BODY_FILE" -T "application/json")
  fi

  ab -n "$n" -c "$c" -q "${extra_args[@]}" "$url" > "$out" 2>&1 || true

  # Extract key metrics from ab output
  local rps p50 p95 p99 failed
  rps=$(grep    "Requests per second"   "$out" | awk '{print $4}')
  p50=$(grep    "50%"                   "$out" | awk '{print $2}')
  p95=$(grep    "95%"                   "$out" | awk '{print $2}')
  p99=$(grep    "99%"                   "$out" | awk '{print $2}')
  failed=$(grep "Failed requests"       "$out" | awk '{print $3}')

  echo -e "${GREEN}  ✓ $label${NC}"
  echo "    RPS: ${rps:-N/A}   p50: ${p50:-N/A}ms   p95: ${p95:-N/A}ms   p99: ${p99:-N/A}ms   failed: ${failed:-0}"
}

# =============================================================================
# TEST SUITE
# =============================================================================

section "1. Smoke Test — low load (GET /health)"
run_ab "smoke_health"   "$HOST/health"  50   5  "GET"

section "2. Baseline — moderate load (POST /notify)"
run_ab "baseline_notify" "$HOST/notify" 500  10 "POST"

section "3. Sustained Load — high concurrency (POST /notify)"
run_ab "sustained_notify" "$HOST/notify" 2000 50 "POST"

section "4. Spike Test — burst (POST /notify)"
run_ab "spike_notify" "$HOST/notify" 1000 100 "POST"

section "5. Health Check Under Load (GET /health)"
run_ab "health_under_load" "$HOST/health" 1000 25 "GET"

# =============================================================================
# SUMMARY REPORT
# =============================================================================
section "Summary Report"
echo ""
printf "%-30s %10s %10s %10s %10s\n" "Test" "RPS" "p50 (ms)" "p95 (ms)" "p99 (ms)"
printf "%-30s %10s %10s %10s %10s\n" "----" "---" "--------" "--------" "--------"

for f in "$RESULTS_DIR"/${TIMESTAMP}_*.txt; do
  label=$(basename "$f" .txt | sed "s/${TIMESTAMP}_//")
  rps=$(grep    "Requests per second" "$f" | awk '{print $4}')
  p50=$(grep    "50%"                 "$f" | awk '{print $2}')
  p95=$(grep    "95%"                 "$f" | awk '{print $2}')
  p99=$(grep    "99%"                 "$f" | awk '{print $2}')
  printf "%-30s %10s %10s %10s %10s\n" \
    "$label" "${rps:-N/A}" "${p50:-N/A}" "${p95:-N/A}" "${p99:-N/A}"
done

echo ""
info "Full ab output saved to: $RESULTS_DIR/"
rm -f "$BODY_FILE"
