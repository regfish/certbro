#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/certbro-smoke.XXXXXX")
API_ADDR="${CERTBRO_SMOKE_API_ADDR:-127.0.0.1:18081}"
API_BASE="http://${API_ADDR}"
API_KEY="${CERTBRO_SMOKE_API_KEY:-smoke-key}"
STATE_FILE="${TMP_DIR}/state.json"
CERTIFICATES_DIR="${TMP_DIR}"
RENEW_LOCK_FILE="${TMP_DIR}/certbro-renew.lock"
OUTPUT_DIR="${TMP_DIR}/example.com"
SYSTEMD_DIR="${TMP_DIR}/systemd"
ENV_FILE="${TMP_DIR}/certbro.env"
BIN_PATH="${TMP_DIR}/certbro"
MOCK_LOG="${TMP_DIR}/mock-api.log"

cleanup() {
  if [ -n "${MOCK_PID:-}" ]; then
    kill "${MOCK_PID}" >/dev/null 2>&1 || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
  if [ "${CERTBRO_KEEP_TEST_ARTIFACTS:-0}" = "1" ]; then
    printf '[smoke] keeping artifacts in %s\n' "${TMP_DIR}"
    return
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'smoke test failed: %s\n' "$1" >&2
  if [ -f "${MOCK_LOG}" ]; then
    printf '\nmock api log:\n' >&2
    cat "${MOCK_LOG}" >&2
  fi
  exit 1
}

mkdir -p "${TMP_DIR}/gocache" "${TMP_DIR}/gotmp"
export GOCACHE="${GOCACHE:-${TMP_DIR}/gocache}"
export GOTMPDIR="${GOTMPDIR:-${TMP_DIR}/gotmp}"
export CERTBRO_CERTIFICATES_DIR="${CERTBRO_CERTIFICATES_DIR:-${CERTIFICATES_DIR}}"
export CERTBRO_RENEW_LOCK_FILE="${CERTBRO_RENEW_LOCK_FILE:-${RENEW_LOCK_FILE}}"

printf '[smoke] build certbro\n'
go build -o "${BIN_PATH}" ./cmd/certbro

printf '[smoke] start mock regfish api on %s\n' "${API_ADDR}"
REGFISH_MOCK_ADDR="${API_ADDR}" REGFISH_MOCK_API_KEY="${API_KEY}" \
  go run ./scripts/mock-regfish-api.go >"${MOCK_LOG}" 2>&1 &
MOCK_PID=$!

i=0
until curl -fsS -H "x-api-key: ${API_KEY}" "${API_BASE}/tls/products" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "${i}" -ge 30 ]; then
    fail "mock api did not become ready"
  fi
  sleep 1
done

printf '[smoke] configure with invalid key must fail\n'
if "${BIN_PATH}" --state-file "${STATE_FILE}" configure --api-key wrong-key --api-base-url "${API_BASE}" >"${TMP_DIR}/invalid-config.out" 2>"${TMP_DIR}/invalid-config.err"; then
  fail "configure unexpectedly succeeded with invalid key"
fi
if ! grep -q "validate API key" "${TMP_DIR}/invalid-config.err"; then
  fail "configure error did not mention API key validation"
fi

printf '[smoke] configure with valid key\n'
"${BIN_PATH}" --state-file "${STATE_FILE}" configure \
  --api-key "${API_KEY}" \
  --api-base-url "${API_BASE}" >/dev/null

printf '[smoke] invalid product must be rejected before ordering\n'
if "${BIN_PATH}" --state-file "${STATE_FILE}" issue \
  --name invalid-product \
  --common-name invalid.example.com \
  --product NopeSSL \
  --output-dir "${TMP_DIR}/invalid.example.com" \
  --wait-interval 10ms \
  --wait-timeout 2s >"${TMP_DIR}/invalid-product.out" 2>"${TMP_DIR}/invalid-product.err"; then
  fail "issue unexpectedly succeeded with invalid product"
fi
if ! grep -q "available products" "${TMP_DIR}/invalid-product.err"; then
  fail "invalid product error did not list available products"
fi
if ! grep -q "RapidSSL" "${TMP_DIR}/invalid-product.err"; then
  fail "invalid product error did not mention RapidSSL"
fi

printf '[smoke] issue certificate\n'
ISSUE_OUT=$("${BIN_PATH}" --state-file "${STATE_FILE}" issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --product RapidSSL \
  --output-dir "${OUTPUT_DIR}" \
  --wait-interval 10ms \
  --wait-timeout 5s)
printf '%s\n' "${ISSUE_OUT}"

if [ ! -f "${OUTPUT_DIR}/certbro.json" ]; then
  fail "issue did not write certbro.json"
fi
if [ ! -f "${OUTPUT_DIR}/live/fullchain.pem" ]; then
  fail "issue did not write live/fullchain.pem"
fi
if [ ! -f "${OUTPUT_DIR}/live/privkey.pem" ]; then
  fail "issue did not write live/privkey.pem"
fi

INITIAL_CERTIFICATE_ID=$(sed -n 's/^certificate_id: //p' <<EOF
${ISSUE_OUT}
EOF
)
if [ -z "${INITIAL_CERTIFICATE_ID}" ]; then
  fail "issue output did not contain certificate_id"
fi

printf '[smoke] force renewal\n'
RENEW_OUT=$("${BIN_PATH}" --state-file "${STATE_FILE}" renew \
  --name example-com \
  --force \
  --wait-interval 10ms \
  --wait-timeout 5s)
printf '%s\n' "${RENEW_OUT}"

RENEWED_CERTIFICATE_ID=$(sed -n 's/^  certificate_id: //p' <<EOF
${RENEW_OUT}
EOF
)
if [ -z "${RENEWED_CERTIFICATE_ID}" ]; then
  fail "renew output did not contain certificate_id"
fi
if [ "${RENEWED_CERTIFICATE_ID}" = "${INITIAL_CERTIFICATE_ID}" ]; then
  fail "renew did not create a new certificate identifier"
fi

if [ "$(uname -s)" = "Linux" ]; then
  printf '[smoke] install systemd files without systemctl\n'
  "${BIN_PATH}" --state-file "${STATE_FILE}" install \
    --certificates-dir "${CERTIFICATES_DIR}" \
    --systemd-dir "${SYSTEMD_DIR}" \
    --env-file "${ENV_FILE}" \
    --binary-path "${BIN_PATH}" \
    --skip-systemctl >/dev/null

  if [ ! -f "${SYSTEMD_DIR}/certbro.service" ]; then
    fail "install did not write certbro.service"
  fi
  if [ ! -f "${SYSTEMD_DIR}/certbro.timer" ]; then
    fail "install did not write certbro.timer"
  fi
  if [ ! -f "${ENV_FILE}" ]; then
    fail "install did not write env file"
  fi
  if ! grep -q 'REGFISH_API_KEY="' "${ENV_FILE}"; then
    fail "env file does not contain REGFISH_API_KEY"
  fi
  if grep -q 'CERTBRO_USER_AGENT_INSTANCE=' "${ENV_FILE}"; then
    fail "env file must not contain CERTBRO_USER_AGENT_INSTANCE"
  fi
else
  printf '[smoke] skip systemd install check on non-Linux\n'
fi

printf '[smoke] list managed certificates\n'
LIST_OUT=$("${BIN_PATH}" --state-file "${STATE_FILE}" list)
printf '%s\n' "${LIST_OUT}"
if ! printf '%s\n' "${LIST_OUT}" | grep -q '^example-com$'; then
  fail "list output does not contain example-com"
fi

printf '[smoke] ok\n'
