#!/usr/bin/env bash

set -euo pipefail
exec > >(tee -a autoscale.log) 2>&1

# ─── tuneable parameters (override via env) ───────────────────────────────────
POLL_INTERVAL="${POLL_INTERVAL:-10}"           # seconds between metric checks
SCALE_UP_THRESHOLD="${SCALE_UP_THRESHOLD:-70}" # CPU % to trigger scale-up
SCALE_DOWN_THRESHOLD="${SCALE_DOWN_THRESHOLD:-20}" # CPU % to trigger scale-down
SCALE_UP_CONSECUTIVE="${SCALE_UP_CONSECUTIVE:-2}"  # consecutive polls above threshold
SCALE_DOWN_CONSECUTIVE="${SCALE_DOWN_CONSECUTIVE:-5}" # conservative: must be low for 5 polls
COOLDOWN_SECONDS="${COOLDOWN_SECONDS:-30}"     # pause after any scaling event
MIN_REPLICAS="${MIN_REPLICAS:-1}"
MAX_REPLICAS="${MAX_REPLICAS:-5}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"

# Services to watch (space-separated)
WATCHED_SERVICES="${WATCHED_SERVICES:-doctor-service appointment-service notification-service mock-gateway}"

# ─── colour helpers ───────────────────────────────────────────────────────────
TS()   { date '+%Y-%m-%d %H:%M:%S'; }
info() { echo "[$(TS)] [INFO ] $*"; }
warn() { echo "[$(TS)] [WARN ] $*"; }
scaleup()   { echo "[$(TS)] [SCALE↑] $*"; }
scaledown() { echo "[$(TS)] [SCALE↓] $*"; }

# ─── state: consecutive counters per service ─────────────────────────────────
declare -A up_count    # service → consecutive polls above threshold
declare -A down_count  # service → consecutive polls below threshold
declare -A replicas    # service → current replica count
declare -A last_scale  # service → epoch of last scale event

for svc in $WATCHED_SERVICES; do
  up_count[$svc]=0
  down_count[$svc]=0
  replicas[$svc]=1
  last_scale[$svc]=0
done

# ─── helpers ──────────────────────────────────────────────────────────────────

get_cpu_percent() {
  # Returns the average CPU % across all running containers of a service.
  # docker stats --no-stream prints: CONTAINER  CPU%  MEM  ...
  # We match by service name (docker compose names containers as <project>-<svc>-<n>)
  local svc="$1"
  local total=0
  local count=0

  while IFS= read -r line; do
    cpu=$(echo "$line" | awk '{print $2}' | tr -d '%')
    total=$(echo "$total + $cpu" | bc 2>/dev/null || echo 0)
    (( count++ )) || true
  done < <(docker stats --no-stream --format "{{.Name}} {{.CPUPerc}}" 2>/dev/null \
            | grep -i "$svc" || true)

  if [[ $count -eq 0 ]]; then
    echo "0"
  else
    echo "scale=1; $total / $count" | bc 2>/dev/null || echo "0"
  fi
}

current_replicas() {
  local svc="$1"
  docker compose -f "$COMPOSE_FILE" ps --quiet "$svc" 2>/dev/null | wc -l | tr -d ' '
}

do_scale() {
  local svc="$1"
  local n="$2"
  info "Scaling '$svc' to $n replicas ..."
  docker compose -f "$COMPOSE_FILE" up -d --scale "${svc}=${n}" --no-recreate 2>&1 \
    | tail -3 || warn "Scale command returned non-zero for $svc"
  replicas[$svc]=$n
  last_scale[$svc]=$(date +%s)
}

in_cooldown() {
  local svc="$1"
  local now; now=$(date +%s)
  local diff=$(( now - last_scale[$svc] ))
  [[ $diff -lt $COOLDOWN_SECONDS ]]
}

# ─── main loop ───────────────────────────────────────────────────────────────

info "Auto-scaler started."
info "Services: $WATCHED_SERVICES"
info "Thresholds: up=${SCALE_UP_THRESHOLD}% (${SCALE_UP_CONSECUTIVE} polls), down=${SCALE_DOWN_THRESHOLD}% (${SCALE_DOWN_CONSECUTIVE} polls)"
info "Replicas: min=$MIN_REPLICAS max=$MAX_REPLICAS | cooldown=${COOLDOWN_SECONDS}s | poll=${POLL_INTERVAL}s"
echo ""

while true; do
  for svc in $WATCHED_SERVICES; do
    cpu=$(get_cpu_percent "$svc")
    cur="${replicas[$svc]}"

    info "  $svc: CPU=${cpu}%  replicas=${cur}  up_streak=${up_count[$svc]}  down_streak=${down_count[$svc]}"

    if in_cooldown "$svc"; then
      info "  $svc: in cooldown — skip"
      continue
    fi

    # ── scale-up check ─────────────────────────────────────────────────────
    if (( $(echo "$cpu > $SCALE_UP_THRESHOLD" | bc -l 2>/dev/null || echo 0) )); then
      (( up_count[$svc]++ )) || true
      down_count[$svc]=0

      if [[ ${up_count[$svc]} -ge $SCALE_UP_CONSECUTIVE ]]; then
        new=$(( cur + 1 ))
        if [[ $new -le $MAX_REPLICAS ]]; then
          scaleup "$svc: CPU=${cpu}% > ${SCALE_UP_THRESHOLD}% for ${SCALE_UP_CONSECUTIVE} polls → scaling $cur→$new"
          do_scale "$svc" "$new"
          up_count[$svc]=0
        else
          warn "$svc: already at MAX_REPLICAS ($MAX_REPLICAS), cannot scale up"
          up_count[$svc]=0
        fi
      fi

    # ── scale-down check ───────────────────────────────────────────────────
    elif (( $(echo "$cpu < $SCALE_DOWN_THRESHOLD" | bc -l 2>/dev/null || echo 0) )); then
      (( down_count[$svc]++ )) || true
      up_count[$svc]=0

      if [[ ${down_count[$svc]} -ge $SCALE_DOWN_CONSECUTIVE ]]; then
        new=$(( cur - 1 ))
        if [[ $new -ge $MIN_REPLICAS ]]; then
          scaledown "$svc: CPU=${cpu}% < ${SCALE_DOWN_THRESHOLD}% for ${SCALE_DOWN_CONSECUTIVE} polls → scaling $cur→$new"
          do_scale "$svc" "$new"
          down_count[$svc]=0
        else
          down_count[$svc]=0  # already at minimum
        fi
      fi

    # ── CPU in normal band → reset both streaks ────────────────────────────
    else
      up_count[$svc]=0
      down_count[$svc]=0
    fi

  done

  sleep "$POLL_INTERVAL"
done
