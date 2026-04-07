#!/usr/bin/env bash
# HarborWorks test runner. Executes:
#   1. unit tests with coverage scoped to pure-logic packages
#   2. starts the cover-instrumented server binary in the background
#   3. runs API tests against the live server
#   4. stops the server (graceful so coverage flushes)
#   5. computes per-scope coverage percentages
#   6. enforces two separate thresholds (defaults: 90.0% / 90.0%)
#
# Exit codes:
#   0 — every test passed and both coverage thresholds were met
#   1 — at least one test failed
#   2 — coverage shortfall (still runs both suites first)

set -uo pipefail

UNIT_THRESHOLD="${UNIT_THRESHOLD:-90.0}"
API_THRESHOLD="${API_THRESHOLD:-90.0}"
APP_BIND="${APP_HOST:-127.0.0.1}:${APP_PORT:-8080}"
COVERAGE_DIR="/coverage"
OUT_DIR="/out"

mkdir -p "$COVERAGE_DIR" "$OUT_DIR"
rm -rf "$COVERAGE_DIR"/* "$OUT_DIR"/*

cd /src

UNIT_PKGS='github.com/harborworks/booking-hub/internal/domain,github.com/harborworks/booking-hub/internal/infrastructure/cache,github.com/harborworks/booking-hub/internal/infrastructure/crypto'
API_PKG_REGEX='github.com/harborworks/booking-hub/internal/api/(handlers|middleware)'

UNIT_RC=0
API_RC=0
COVERAGE_RC=0

echo "════════════════════════════════════════════════════════════════"
echo "HarborWorks test runner"
echo "════════════════════════════════════════════════════════════════"
echo "  unit threshold : ${UNIT_THRESHOLD}%"
echo "  api  threshold : ${API_THRESHOLD}%"
echo "  app bind       : ${APP_BIND}"
echo "  unit pkgs      : ${UNIT_PKGS}"
echo "  api pkgs regex : ${API_PKG_REGEX}"
echo

####################################
# 1. UNIT TESTS
####################################
echo "── [1/4] Running unit tests ──────────────────────────────────────"
go test -count=1 -coverprofile="$OUT_DIR/unit.cov" -coverpkg="$UNIT_PKGS" ./unit_tests/... || UNIT_RC=$?
echo

####################################
# 2. START COVER-INSTRUMENTED SERVER
####################################
echo "── [2/4] Starting cover-instrumented server ──────────────────────"
GOCOVERDIR="$COVERAGE_DIR" server-cover &
APP_PID=$!

# Wait for /healthz.
ready=0
for i in $(seq 1 60); do
  if curl -sf "http://${APP_BIND}/healthz" >/dev/null 2>&1; then
    ready=1
    echo "  app is ready (pid=$APP_PID)"
    break
  fi
  sleep 0.5
done
if [ "$ready" -ne 1 ]; then
  echo "  ERROR: app failed to become ready"
  kill -TERM "$APP_PID" 2>/dev/null || true
  wait "$APP_PID" 2>/dev/null || true
  exit 1
fi
echo

####################################
# 3. API TESTS
####################################
echo "── [3/4] Running API tests ───────────────────────────────────────"
APP_URL="http://${APP_BIND}" go test -count=1 -p=1 ./API_tests/... || API_RC=$?
echo

####################################
# 4. STOP SERVER & COMPUTE COVERAGE
####################################
echo "── [4/4] Stopping server and computing coverage ──────────────────"
kill -TERM "$APP_PID" 2>/dev/null || true
wait "$APP_PID" 2>/dev/null || true

# Convert ALL coverage data to text format. The package filter is then
# applied via grep so the regex syntax is unambiguous and inspectable.
go tool covdata textfmt -i="$COVERAGE_DIR" -o="$OUT_DIR/all.cov" 2>&1 | sed 's/^/    /' || true

if [ ! -s "$OUT_DIR/all.cov" ]; then
  echo "  WARNING: covdata textfmt produced no output"
  API_PCT="0.0"
else
  # api.cov keeps the mode header line plus only the api packages we care about.
  {
    head -1 "$OUT_DIR/all.cov"
    grep -E 'internal/api/(handlers|middleware)' "$OUT_DIR/all.cov" || true
  } > "$OUT_DIR/api.cov"

  if [ "$(wc -l < "$OUT_DIR/api.cov")" -le 1 ]; then
    echo "  WARNING: no api/handlers or api/middleware lines in coverage profile"
    API_PCT="0.0"
  else
    API_PCT=$(go tool cover -func="$OUT_DIR/api.cov" | tail -1 | awk '{print $3}' | tr -d '%')
  fi
fi

if [ ! -s "$OUT_DIR/unit.cov" ]; then
  UNIT_PCT="0.0"
else
  UNIT_PCT=$(go tool cover -func="$OUT_DIR/unit.cov" | tail -1 | awk '{print $3}' | tr -d '%')
fi

echo
echo "════════════════════════════════════════════════════════════════"
echo "Coverage summary"
echo "════════════════════════════════════════════════════════════════"
printf "  unit : %6s%%   threshold %s%%\n" "${UNIT_PCT:-0.0}" "$UNIT_THRESHOLD"
printf "  api  : %6s%%   threshold %s%%\n" "${API_PCT:-0.0}"  "$API_THRESHOLD"
echo

# When the API coverage is below threshold, dump the worst-covered functions
# so a developer can see immediately which handlers need more tests.
if awk -v a="${API_PCT:-0}" -v b="$API_THRESHOLD" 'BEGIN{exit !(a+0 < b+0)}'; then
  echo "  worst-covered API functions:"
  go tool cover -func="$OUT_DIR/api.cov" 2>/dev/null \
    | grep -v 'total:' \
    | awk '{print $NF, $0}' \
    | sort -n \
    | head -20 \
    | awk '{$1=""; print "   ", $0}'
  echo
fi

# Threshold gates (awk handles float comparison portably).
if ! awk -v a="${UNIT_PCT:-0}" -v b="$UNIT_THRESHOLD" 'BEGIN{exit !(a+0 >= b+0)}'; then
  echo "FAIL: unit coverage ${UNIT_PCT}% is below threshold ${UNIT_THRESHOLD}%"
  COVERAGE_RC=1
fi
if ! awk -v a="${API_PCT:-0}" -v b="$API_THRESHOLD" 'BEGIN{exit !(a+0 >= b+0)}'; then
  echo "FAIL: api coverage ${API_PCT}% is below threshold ${API_THRESHOLD}%"
  COVERAGE_RC=1
fi

echo
echo "Test results"
echo "  unit suite : $([ $UNIT_RC -eq 0 ] && echo PASS || echo FAIL)"
echo "  api  suite : $([ $API_RC  -eq 0 ] && echo PASS || echo FAIL)"

# Compose final exit code: tests fail with 1, coverage shortfall with 2.
FINAL_RC=0
if [ $UNIT_RC -ne 0 ] || [ $API_RC -ne 0 ]; then
  FINAL_RC=1
elif [ $COVERAGE_RC -ne 0 ]; then
  FINAL_RC=2
fi

echo
if [ $FINAL_RC -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "FAILED (rc=$FINAL_RC)"
fi
exit $FINAL_RC
