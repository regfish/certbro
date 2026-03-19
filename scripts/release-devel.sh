#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
PRIVATE_REMOTE="${CERTBRO_PRIVATE_REMOTE:-${CERTBRO_GIT_REMOTE:-}}"
EXPECTED_PRIVATE_REPO="${CERTBRO_EXPECTED_PRIVATE_REPO:-}"
SKIP_PREFLIGHT="${CERTBRO_SKIP_PREFLIGHT:-0}"
DRY_RUN=0
TAG_CREATED=0

usage() {
  code="${1:-1}"
  cat <<'EOF' >&2
usage: ./scripts/release-devel.sh devel/vX.Y.Z-dev.N [tag message]

Create a development release tag in the private repository only.

Defaults:
  private remote: origin
  expected private repo: regfish/certbro-devel

Git config keys supported:
  certbro.privateRemote
  certbro.expectedPrivateRepo

Environment:
  CERTBRO_PRIVATE_REMOTE
  CERTBRO_EXPECTED_PRIVATE_REPO
  CERTBRO_SKIP_PREFLIGHT=1

Options:
  --dry-run   validate and print the release plan without pushing
  -h, --help  show this help
EOF
  exit "${code}"
}

fail() {
  printf 'release-devel failed: %s\n' "$1" >&2
  exit 1
}

repo_slug_from_url() {
  url="$1"
  url="${url%/}"
  case "$url" in
    git@github.com:*)
      url="${url#git@github.com:}"
      ;;
    ssh://git@github.com/*)
      url="${url#ssh://git@github.com/}"
      ;;
    https://github.com/*)
      url="${url#https://github.com/}"
      ;;
    http://github.com/*)
      url="${url#http://github.com/}"
      ;;
    *)
      printf '%s\n' ""
      return 0
      ;;
  esac
  url="${url%.git}"
  printf '%s\n' "$url"
}

cleanup() {
  if [ "${TAG_CREATED}" = "1" ]; then
    git tag -d "${VERSION}" >/dev/null 2>&1 || true
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      fail "unknown option: $1"
      ;;
    *)
      break
      ;;
  esac
done

VERSION="${1:-}"
TAG_MESSAGE="${2:-}"

[ -n "${VERSION}" ] || usage 1

case "${VERSION}" in
  devel/v*)
    ;;
  *)
    fail "version must start with 'devel/v', for example devel/v0.2.0-dev.1"
    ;;
esac

if [ -z "${TAG_MESSAGE}" ]; then
  TAG_MESSAGE="Development release ${VERSION}"
fi

cd "${ROOT_DIR}"
trap cleanup EXIT INT TERM

git rev-parse --git-dir >/dev/null 2>&1 || fail "not a git repository"

if [ -z "${PRIVATE_REMOTE}" ]; then
  PRIVATE_REMOTE="$(git config --get certbro.privateRemote 2>/dev/null || true)"
fi
if [ -z "${PRIVATE_REMOTE}" ]; then
  PRIVATE_REMOTE="origin"
fi

if [ -z "${EXPECTED_PRIVATE_REPO}" ]; then
  EXPECTED_PRIVATE_REPO="$(git config --get certbro.expectedPrivateRepo 2>/dev/null || true)"
fi
if [ -z "${EXPECTED_PRIVATE_REPO}" ]; then
  EXPECTED_PRIVATE_REPO="regfish/certbro-devel"
fi

BRANCH="$(git branch --show-current)"
[ -n "${BRANCH}" ] || fail "detached HEAD is not supported"

if [ -n "$(git status --porcelain --untracked-files=all)" ]; then
  fail "working tree is not clean; commit or stash tracked and untracked changes first"
fi

git remote get-url "${PRIVATE_REMOTE}" >/dev/null 2>&1 || fail "git remote '${PRIVATE_REMOTE}' does not exist"
PRIVATE_URL="$(git remote get-url "${PRIVATE_REMOTE}")"
PRIVATE_SLUG="$(repo_slug_from_url "${PRIVATE_URL}")"

if [ -n "${PRIVATE_SLUG}" ] && [ "${PRIVATE_SLUG}" != "${EXPECTED_PRIVATE_REPO}" ]; then
  fail "private remote ${PRIVATE_REMOTE} points to ${PRIVATE_SLUG}, expected ${EXPECTED_PRIVATE_REPO}"
fi

if git rev-parse -q --verify "refs/tags/${VERSION}" >/dev/null 2>&1; then
  fail "local tag ${VERSION} already exists"
fi
if git ls-remote --exit-code --tags "${PRIVATE_REMOTE}" "refs/tags/${VERSION}" >/dev/null 2>&1; then
  fail "remote tag ${VERSION} already exists on ${PRIVATE_REMOTE}"
fi

printf '[release-devel] private remote: %s (%s)\n' "${PRIVATE_REMOTE}" "${PRIVATE_URL}"
printf '[release-devel] branch: %s\n' "${BRANCH}"
printf '[release-devel] tag: %s\n' "${VERSION}"
printf '[release-devel] head: %s\n' "$(git rev-parse HEAD)"

if [ "${SKIP_PREFLIGHT}" != "1" ]; then
  printf '[release-devel] run preflight\n'
  "${ROOT_DIR}/scripts/test-preflight.sh"
else
  printf '[release-devel] skip preflight\n'
fi

if [ "${DRY_RUN}" = "1" ]; then
  printf '[release-devel] dry run enabled; not pushing branch or tag\n'
  exit 0
fi

printf '[release-devel] push branch %s to %s\n' "${BRANCH}" "${PRIVATE_REMOTE}"
git push "${PRIVATE_REMOTE}" "${BRANCH}"

printf '[release-devel] create temporary local tag %s\n' "${VERSION}"
git tag -a "${VERSION}" -m "${TAG_MESSAGE}"
TAG_CREATED=1

printf '[release-devel] push tag %s to %s\n' "${VERSION}" "${PRIVATE_REMOTE}"
git push "${PRIVATE_REMOTE}" "refs/tags/${VERSION}:refs/tags/${VERSION}"

printf '[release-devel] ok\n'
printf '[release-devel] pushed private tag %s\n' "${VERSION}"
