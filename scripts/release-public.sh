#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
PRIVATE_REMOTE="${CERTBRO_PRIVATE_REMOTE:-${CERTBRO_GIT_REMOTE:-}}"
EXPECTED_PRIVATE_REPO="${CERTBRO_EXPECTED_PRIVATE_REPO:-}"
PUBLIC_REMOTE="${CERTBRO_PUBLIC_REMOTE:-}"
PUBLIC_REPO="${CERTBRO_PUBLIC_REPO:-}"
PUBLIC_URL="${CERTBRO_PUBLIC_URL:-}"
PUBLIC_BRANCH="${CERTBRO_PUBLIC_BRANCH:-}"
SKIP_PREFLIGHT="${CERTBRO_SKIP_PREFLIGHT:-0}"
DRY_RUN=0
TAG_CREATED=0

usage() {
  code="${1:-1}"
  cat <<'EOF' >&2
usage: ./scripts/release-public.sh vX.Y.Z [snapshot commit message]

Publish the current private HEAD as one public snapshot commit and then create a
public release tag on that snapshot commit only.

Defaults:
  private remote: origin
  expected private repo: regfish/certbro-devel
  public repo: regfish/certbro
  public branch: main

Git config keys supported:
  certbro.privateRemote
  certbro.expectedPrivateRepo
  certbro.publicRemote
  certbro.publicRepo
  certbro.publicBranch

Environment:
  CERTBRO_PRIVATE_REMOTE
  CERTBRO_EXPECTED_PRIVATE_REPO
  CERTBRO_PUBLIC_REMOTE
  CERTBRO_PUBLIC_REPO
  CERTBRO_PUBLIC_URL
  CERTBRO_PUBLIC_BRANCH
  CERTBRO_SKIP_PREFLIGHT=1

Options:
  --dry-run   validate and print the release plan without pushing
  -h, --help  show this help
EOF
  exit "${code}"
}

fail() {
  printf 'release-public failed: %s\n' "$1" >&2
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
SNAPSHOT_MESSAGE="${2:-}"

[ -n "${VERSION}" ] || usage 1

case "${VERSION}" in
  v*)
    ;;
  *)
    fail "version must start with 'v', for example v0.2.0"
    ;;
esac

if [ -z "${SNAPSHOT_MESSAGE}" ]; then
  SNAPSHOT_MESSAGE="release: ${VERSION}"
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

if [ -z "${PUBLIC_REMOTE}" ]; then
  PUBLIC_REMOTE="$(git config --get certbro.publicRemote 2>/dev/null || true)"
fi
if [ -z "${PUBLIC_REMOTE}" ] && git remote get-url public >/dev/null 2>&1; then
  PUBLIC_REMOTE="public"
fi

if [ -z "${PUBLIC_REPO}" ]; then
  PUBLIC_REPO="$(git config --get certbro.publicRepo 2>/dev/null || true)"
fi
if [ -z "${PUBLIC_REPO}" ]; then
  PUBLIC_REPO="regfish/certbro"
fi

if [ -z "${PUBLIC_BRANCH}" ]; then
  PUBLIC_BRANCH="$(git config --get certbro.publicBranch 2>/dev/null || true)"
fi
if [ -z "${PUBLIC_BRANCH}" ]; then
  PUBLIC_BRANCH="main"
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

TAG_TARGET=""
TAG_TARGET_LABEL=""
TAG_TARGET_URL=""
if [ -n "${PUBLIC_REMOTE}" ]; then
  git remote get-url "${PUBLIC_REMOTE}" >/dev/null 2>&1 || fail "git remote '${PUBLIC_REMOTE}' does not exist"
  TAG_TARGET="${PUBLIC_REMOTE}"
  TAG_TARGET_LABEL="${PUBLIC_REMOTE}/${PUBLIC_BRANCH}"
  TAG_TARGET_URL="$(git remote get-url "${PUBLIC_REMOTE}")"
else
  if [ -z "${PUBLIC_URL}" ]; then
    PUBLIC_URL="https://github.com/${PUBLIC_REPO}.git"
  fi
  TAG_TARGET="${PUBLIC_URL}"
  TAG_TARGET_LABEL="${PUBLIC_REPO}/${PUBLIC_BRANCH}"
  TAG_TARGET_URL="${PUBLIC_URL}"
fi

PUBLIC_SLUG="$(repo_slug_from_url "${TAG_TARGET_URL}")"
if [ -n "${PUBLIC_SLUG}" ] && [ "${PUBLIC_SLUG}" != "${PUBLIC_REPO}" ]; then
  fail "public target ${TAG_TARGET_LABEL} points to ${PUBLIC_SLUG}, expected ${PUBLIC_REPO}"
fi

if git rev-parse -q --verify "refs/tags/${VERSION}" >/dev/null 2>&1; then
  fail "local tag ${VERSION} already exists"
fi
if git ls-remote --exit-code --tags "${TAG_TARGET}" "refs/tags/${VERSION}" >/dev/null 2>&1; then
  fail "public tag ${VERSION} already exists on ${TAG_TARGET_LABEL}"
fi

printf '[release-public] private remote: %s (%s)\n' "${PRIVATE_REMOTE}" "${PRIVATE_URL}"
printf '[release-public] private branch: %s\n' "${BRANCH}"
printf '[release-public] public target: %s\n' "${TAG_TARGET_LABEL}"
printf '[release-public] public tag: %s\n' "${VERSION}"
printf '[release-public] private head: %s\n' "$(git rev-parse HEAD)"

if [ "${SKIP_PREFLIGHT}" != "1" ]; then
  printf '[release-public] run preflight\n'
  "${ROOT_DIR}/scripts/test-preflight.sh"
else
  printf '[release-public] skip preflight\n'
fi

if [ "${DRY_RUN}" = "1" ]; then
  printf '[release-public] dry run enabled; publish and tag creation are skipped\n'
  exit 0
fi

printf '[release-public] push branch %s to %s\n' "${BRANCH}" "${PRIVATE_REMOTE}"
git push "${PRIVATE_REMOTE}" "${BRANCH}"

PUBLISH_OUTPUT="$(
  CERTBRO_PRIVATE_REMOTE="${PRIVATE_REMOTE}" \
  CERTBRO_EXPECTED_PRIVATE_REPO="${EXPECTED_PRIVATE_REPO}" \
  CERTBRO_PUBLIC_REMOTE="${PUBLIC_REMOTE}" \
  CERTBRO_PUBLIC_REPO="${PUBLIC_REPO}" \
  CERTBRO_PUBLIC_URL="${PUBLIC_URL}" \
  CERTBRO_PUBLIC_BRANCH="${PUBLIC_BRANCH}" \
  "${ROOT_DIR}/scripts/publish-public.sh" "${SNAPSHOT_MESSAGE}"
)"
printf '%s\n' "${PUBLISH_OUTPUT}"

SNAPSHOT_COMMIT="$(printf '%s\n' "${PUBLISH_OUTPUT}" | awk -F': ' '/^snapshot commit: / { print $2; exit }')"
[ -n "${SNAPSHOT_COMMIT}" ] || fail "could not parse snapshot commit from publish-public.sh output"

if ! git log -1 --format=%B "${SNAPSHOT_COMMIT}" | grep -q '^certbro-public-snapshot: true$'; then
  fail "snapshot commit ${SNAPSHOT_COMMIT} is missing the certbro public snapshot marker"
fi

printf '[release-public] create temporary local tag %s on snapshot %s\n' "${VERSION}" "${SNAPSHOT_COMMIT}"
git tag -a "${VERSION}" "${SNAPSHOT_COMMIT}" -m "Release ${VERSION}"
TAG_CREATED=1

printf '[release-public] push public tag %s to %s\n' "${VERSION}" "${TAG_TARGET_LABEL}"
git push "${TAG_TARGET}" "refs/tags/${VERSION}:refs/tags/${VERSION}"

printf '[release-public] ok\n'
printf '[release-public] published snapshot %s and pushed public tag %s\n' "${SNAPSHOT_COMMIT}" "${VERSION}"
