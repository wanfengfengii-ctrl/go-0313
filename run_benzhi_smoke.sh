#!/usr/bin/env bash
#
# Deterministic smoke test for the siphonic roof drainage backend.
#
# It builds the real server binary, starts it against a temporary data
# directory, probes the health endpoint, and exercises the public create/lock
# command surface (including Operation-Id idempotency) over local HTTP only.
# It never touches the network beyond loopback and cleans up every process and
# temporary file before exiting.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

PORT="${SIPHONIC_SMOKE_PORT:-18089}"
BASE="http://127.0.0.1:${PORT}"
TMPDIR="$(mktemp -d "${ROOT}/.smoke.XXXXXX")"
BIN="${TMPDIR}/server"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMPDIR}"
}
trap cleanup EXIT

echo "building server binary..."
go build -o "${BIN}" ./cmd/server

echo "starting server on ${BASE} ..."
"${BIN}" -addr "127.0.0.1:${PORT}" -data-dir "${TMPDIR}/data" &
SERVER_PID=$!

# Wait for the health endpoint to become ready.
health=""
for _ in $(seq 1 100); do
  if health="$(curl -s "${BASE}/healthz" 2>/dev/null)"; then
    if [[ "${health}" == *'"status":"ok"'* ]]; then
      break
    fi
  fi
  sleep 0.1
done

if [[ "${health}" != *'"status":"ok"'* ]]; then
  echo "health check failed: ${health}" >&2
  exit 1
fi
echo "health OK"

create_body='{"task_id":"smoke-task-1"}'
create_resp="$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Operation-Id: smoke-create-1" \
  -d "${create_body}" "${BASE}/v1/tasks")"
if [[ "${create_resp}" != *'"task_id":"smoke-task-1"'* ]]; then
  echo "create task failed: ${create_resp}" >&2
  exit 1
fi
echo "create OK: ${create_resp}"

lock_body='{"task_id":"smoke-task-1","summary_version":1,"graph":{"zones":[{"id":"zone-A","name":"A","outlet":"out-A"}],"drains":[{"id":"drain-A","zone":"zone-A"}],"segments":[{"id":"seg-A","zone":"zone-A","from":"drain-A-port","to":"out-A-port","diameter":110,"length_mm":1000}],"ports":[{"id":"drain-A-port","owner":"drain-A","diameter":110},{"id":"out-A-port","owner":"out-A","diameter":110}],"outlets":[{"id":"out-A"}],"edges":[{"from":"drain-A-port","to":"out-A-port"}]},"materials":[{"id":"pipe-1","batch":"batch-1","kind":"PIPE","length_mm":1000}]}'

lock_resp="$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Operation-Id: smoke-lock-1" \
  -d "${lock_body}" "${BASE}/v1/tasks/smoke-task-1/lock")"
if [[ "${lock_resp}" != *'"locked":true'* ]]; then
  echo "lock task failed: ${lock_resp}" >&2
  exit 1
fi
echo "lock OK: ${lock_resp}"

# Idempotent replay: same Operation-Id and same content returns the same
# successful result rather than a conflict.
replay_resp="$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Operation-Id: smoke-lock-1" \
  -d "${lock_body}" "${BASE}/v1/tasks/smoke-task-1/lock")"
if [[ "${replay_resp}" != *'"locked":true'* ]]; then
  echo "idempotent replay failed: ${replay_resp}" >&2
  exit 1
fi
echo "idempotent replay OK"

# Query the task and confirm the lock state is persisted.
query_resp="$(curl -s "${BASE}/v1/tasks/smoke-task-1")"
if [[ "${query_resp}" != *'"LockState":"LOCKED"'* ]]; then
  echo "query failed: ${query_resp}" >&2
  exit 1
fi
echo "query OK (locked)"

echo "SMOKE PASSED"
