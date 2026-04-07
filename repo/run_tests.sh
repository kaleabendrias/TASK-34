#!/usr/bin/env bash
# Single-command HarborWorks test runner.
#
# Brings up the test stack (postgres + a one-shot tests container that hosts
# the cover-instrumented binary), runs the unit_tests/ and API_tests/ suites
# inside the container, and propagates its exit code.
#
# Exit codes mirror the inner runner:
#   0 — all tests passed and coverage thresholds met
#   1 — at least one test failed
#   2 — coverage shortfall

set -uo pipefail

cd "$(dirname "$0")"

PROJECT="harborworks-test"
COMPOSE_FILE="docker-compose.test.yml"

cleanup() {
  echo
  echo "── Tearing down test stack ───────────────────────────────────────"
  docker compose -f "$COMPOSE_FILE" -p "$PROJECT" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "── Building test image ───────────────────────────────────────────"
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" build tests
BUILD_RC=$?
if [ $BUILD_RC -ne 0 ]; then
  echo "build failed"
  exit $BUILD_RC
fi

echo
echo "── Running test suites ───────────────────────────────────────────"
# Allow callers to sweep thresholds: UNIT_THRESHOLD=80 API_THRESHOLD=80 ./run_tests.sh
# Compose env_file/inline merging means we have to write a small override file
# so thresholds pass through cleanly; alternative is `-e` on `run`.
export UNIT_THRESHOLD="${UNIT_THRESHOLD:-90.0}"
export API_THRESHOLD="${API_THRESHOLD:-90.0}"

# `up --abort-on-container-exit --exit-code-from tests` propagates the tests
# container's exit code reliably across compose versions (unlike `run`).
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" up \
  --abort-on-container-exit \
  --exit-code-from tests \
  --no-color
RC=$?

echo
echo "── Done (exit ${RC}) ─────────────────────────────────────────────"
exit $RC
