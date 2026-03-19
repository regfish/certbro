#!/bin/sh
set -eu

REPO="${CERTBRO_TEST_REPO:-regfish/certbro-devel}"
SERVER="${CERTBRO_TEST_SERVER:-automation-test}"
REMOTE_DIR="${CERTBRO_TEST_REMOTE_DIR:-/tmp/certbro-testdeploy}"
INSTALL_DIR="${CERTBRO_TEST_INSTALL_DIR:-/usr/local/bin}"
ARCHIVE="${CERTBRO_TEST_ARCHIVE:-certbro_linux_amd64.tar.gz}"
BINARY_NAME="${CERTBRO_TEST_BINARY_NAME:-certbro}"
BOOTSTRAP_BIN_DIR="${CERTBRO_TEST_BOOTSTRAP_BIN_DIR:-$HOME/.local/bin}"
CHECKSUMS_FILE="checksums.txt"

usage() {
  cat <<'EOF'
Usage:
  ./testdeploy.sh [options] [tag]

Downloads a release artifact from the private certbro-devel repository,
copies it to a test server, verifies its checksum, installs the binary,
and prints the deployed version.

If no tag is provided, the latest release is used.

Options:
  --server HOST        SSH target, for example root@automation-test
  --repo OWNER/REPO    GitHub repository to download from
  --archive NAME       Release archive name to deploy
  --remote-dir PATH    Remote staging directory
  --install-dir PATH   Remote installation directory
  -h, --help           Show this help

Environment:
  CERTBRO_TEST_SERVER            Default value for --server
  CERTBRO_TEST_REPO              Default value for --repo
  CERTBRO_TEST_ARCHIVE           Default value for --archive
  CERTBRO_TEST_REMOTE_DIR        Default value for --remote-dir
  CERTBRO_TEST_INSTALL_DIR       Default value for --install-dir
  CERTBRO_TEST_BINARY_NAME       Default installed binary name
  CERTBRO_TEST_BOOTSTRAP_BIN_DIR Directory used when gh must be bootstrapped locally

Authentication:
  gh must be authenticated for the private repository, for example via GH_TOKEN.
EOF
}

fail() {
  printf 'testdeploy failed: %s\n' "$1" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

download_file() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
    return
  fi
  fail "missing downloader (need curl or wget)"
}

fetch_url() {
  url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO- "$url"
    return
  fi
  fail "missing downloader (need curl or wget)"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  fail "missing sha256 tool (need sha256sum or shasum)"
}

detect_os() {
  os="$(uname -s)"
  case "$os" in
    Linux) printf 'linux\n' ;;
    Darwin) printf 'darwin\n' ;;
    *) fail "unsupported local operating system for gh bootstrap: $os" ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported local architecture for gh bootstrap: $arch" ;;
  esac
}

latest_gh_tag() {
  response="$(fetch_url https://api.github.com/repos/cli/cli/releases/latest | tr -d '\n')"
  tag="$(printf '%s' "$response" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$tag" ] || fail "could not determine latest gh release version"
  printf '%s\n' "$tag"
}

ensure_gh() {
  if command -v gh >/dev/null 2>&1; then
    return
  fi

  printf '[testdeploy] gh not found, bootstrapping locally into %s\n' "$BOOTSTRAP_BIN_DIR"

  os="$(detect_os)"
  arch="$(detect_arch)"
  gh_tag="$(latest_gh_tag)"
  gh_version="${gh_tag#v}"
  bootstrap_tmpdir="$(mktemp -d)"
  mkdir -p "$BOOTSTRAP_BIN_DIR"

  cleanup_bootstrap() {
    rm -rf "$bootstrap_tmpdir"
  }
  trap 'cleanup_bootstrap; rm -rf "$tmpdir"' EXIT INT TERM

  case "$os" in
    linux)
      archive_name="gh_${gh_version}_linux_${arch}.tar.gz"
      download_file "https://github.com/cli/cli/releases/download/${gh_tag}/${archive_name}" "${bootstrap_tmpdir}/${archive_name}"
      tar -xzf "${bootstrap_tmpdir}/${archive_name}" -C "${bootstrap_tmpdir}"
      source_path="${bootstrap_tmpdir}/gh_${gh_version}_linux_${arch}/bin/gh"
      ;;
    darwin)
      archive_name="gh_${gh_version}_macOS_${arch}.zip"
      download_file "https://github.com/cli/cli/releases/download/${gh_tag}/${archive_name}" "${bootstrap_tmpdir}/${archive_name}"
      need_cmd unzip
      unzip -q "${bootstrap_tmpdir}/${archive_name}" -d "${bootstrap_tmpdir}"
      source_path="${bootstrap_tmpdir}/gh_${gh_version}_macOS_${arch}/bin/gh"
      ;;
    *)
      fail "unsupported local operating system for gh bootstrap: $os"
      ;;
  esac

  [ -f "$source_path" ] || fail "downloaded gh archive did not contain the gh binary"
  install -m 0755 "$source_path" "${BOOTSTRAP_BIN_DIR}/gh"
  PATH="${BOOTSTRAP_BIN_DIR}:$PATH"
  export PATH

  cleanup_bootstrap
  trap 'rm -rf "$tmpdir"' EXIT INT TERM

  command -v gh >/dev/null 2>&1 || fail "gh bootstrap completed but gh is still not executable"
}

ensure_gh_auth() {
  if [ -n "${GH_TOKEN:-}" ] || [ -n "${GITHUB_TOKEN:-}" ]; then
    return
  fi
  gh auth status >/dev/null 2>&1 || fail "gh is not authenticated for private repository access; export GH_TOKEN or run gh auth login"
}

VERSION=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --server)
      [ "$#" -ge 2 ] || fail "missing value for --server"
      SERVER="$2"
      shift 2
      ;;
    --repo)
      [ "$#" -ge 2 ] || fail "missing value for --repo"
      REPO="$2"
      shift 2
      ;;
    --archive)
      [ "$#" -ge 2 ] || fail "missing value for --archive"
      ARCHIVE="$2"
      shift 2
      ;;
    --remote-dir)
      [ "$#" -ge 2 ] || fail "missing value for --remote-dir"
      REMOTE_DIR="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || fail "missing value for --install-dir"
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      fail "unknown option: $1"
      ;;
    *)
      if [ -n "$VERSION" ]; then
        fail "only one optional tag argument is supported"
      fi
      VERSION="$1"
      shift
      ;;
  esac
done

if [ "$#" -gt 0 ]; then
  fail "unexpected extra arguments: $*"
fi

[ -n "$SERVER" ] || fail "missing test server: pass --server or set CERTBRO_TEST_SERVER"

need_cmd scp
need_cmd ssh
need_cmd tar

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

ensure_gh
ensure_gh_auth

printf '[testdeploy] gh: %s\n' "$(command -v gh)"
printf '[testdeploy] repo: %s\n' "$REPO"
printf '[testdeploy] server: %s\n' "$SERVER"
printf '[testdeploy] archive: %s\n' "$ARCHIVE"

if [ -n "$VERSION" ]; then
  printf '[testdeploy] download release %s\n' "$VERSION"
  gh release download "$VERSION" \
    -R "$REPO" \
    -D "$tmpdir" \
    -p "$ARCHIVE" \
    -p "$CHECKSUMS_FILE"
  resolved_version="$VERSION"
else
  printf '[testdeploy] download latest release\n'
  gh release download \
    -R "$REPO" \
    -D "$tmpdir" \
    -p "$ARCHIVE" \
    -p "$CHECKSUMS_FILE"
  resolved_version="$(gh release view -R "$REPO" --json tagName -q .tagName)"
  [ -n "$resolved_version" ] || fail "could not determine latest release tag"
fi

expected="$(awk -v file="$ARCHIVE" '$2 == file { print $1 }' "$tmpdir/$CHECKSUMS_FILE")"
[ -n "$expected" ] || fail "checksum entry for $ARCHIVE not found"

actual="$(sha256_file "$tmpdir/$ARCHIVE")"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $ARCHIVE"

printf '[testdeploy] local checksum ok for %s\n' "$resolved_version"

ssh "$SERVER" /bin/sh -s -- "$REMOTE_DIR" <<'EOF'
set -eu
mkdir -p "$1"
EOF

printf '[testdeploy] upload to %s:%s\n' "$SERVER" "$REMOTE_DIR"
scp "$tmpdir/$ARCHIVE" "$tmpdir/$CHECKSUMS_FILE" "${SERVER}:${REMOTE_DIR}/"

printf '[testdeploy] verify and install on server\n'
ssh "$SERVER" /bin/sh -s -- "$REMOTE_DIR" "$INSTALL_DIR" "$ARCHIVE" "$CHECKSUMS_FILE" "$BINARY_NAME" "$resolved_version" <<'EOF'
set -eu

remote_dir="$1"
install_dir="$2"
archive="$3"
checksums_file="$4"
binary_name="$5"
version="$6"

fail() {
  printf 'remote install failed: %s\n' "$1" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  fail "missing sha256 tool (need sha256sum or shasum)"
}

cd "$remote_dir"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "$checksums_file")"
[ -n "$expected" ] || fail "checksum entry for $archive not found"

actual="$(sha256_file "$archive")"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$archive"
[ -f "$binary_name" ] || fail "archive did not contain $binary_name"

if [ -d "$install_dir" ]; then
  :
elif [ -w "$(dirname "$install_dir")" ]; then
  mkdir -p "$install_dir"
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "$install_dir"
else
  fail "cannot create install directory $install_dir"
fi

if [ -w "$install_dir" ]; then
  install -m 0755 "$binary_name" "$install_dir/$binary_name"
else
  command -v sudo >/dev/null 2>&1 || fail "install directory is not writable and sudo is unavailable: $install_dir"
  sudo install -m 0755 "$binary_name" "$install_dir/$binary_name"
fi

"$install_dir/$binary_name" version
printf 'deployed %s from %s to %s\n' "$binary_name" "$version" "$install_dir/$binary_name"
EOF

printf '[testdeploy] ok\n'
