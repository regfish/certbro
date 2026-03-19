#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/certbro-preflight.XXXXXX")

cleanup() {
  if [ "${CERTBRO_KEEP_TEST_ARTIFACTS:-0}" = "1" ]; then
    printf '[preflight] keeping artifacts in %s\n' "${RUN_DIR}"
    return
  fi
  rm -rf "${RUN_DIR}"
}
trap cleanup EXIT INT TERM

mkdir -p "${RUN_DIR}/gocache" "${RUN_DIR}/gotmp"
export GOCACHE="${GOCACHE:-${RUN_DIR}/gocache}"
export GOTMPDIR="${GOTMPDIR:-${RUN_DIR}/gotmp}"

printf '[preflight] unit tests\n'
go test ./...

printf '[preflight] build certbro\n'
go build -o "${RUN_DIR}/certbro" ./cmd/certbro

printf '[preflight] validate install.sh syntax\n'
sh -n "${ROOT_DIR}/install.sh"

printf '[preflight] cli smoke tests\n'
"${ROOT_DIR}/scripts/test-smoke.sh"

printf '[preflight] ok\n'
