#!/bin/sh
set -eu

REPO="${CERTBRO_REPO:-regfish/certbro}"
VERSION="${CERTBRO_VERSION:-latest}"
INSTALL_DIR="${CERTBRO_INSTALL_DIR:-/usr/local/bin}"
BIN_NAME="certbro"

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux|darwin) printf '%s\n' "$os" ;;
    *)
      echo "unsupported operating system: $os" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *)
      echo "unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac
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
  echo "missing sha256 tool (need sha256sum or shasum)" >&2
  exit 1
}

download() {
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
  echo "missing downloader (need curl or wget)" >&2
  exit 1
}

os="$(detect_os)"
arch="$(detect_arch)"
archive="${BIN_NAME}_${os}_${arch}.tar.gz"
checksums="checksums.txt"

if [ "$VERSION" = "latest" ]; then
  base_url="https://github.com/${REPO}/releases/latest/download"
else
  base_url="https://github.com/${REPO}/releases/download/${VERSION}"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

download "${base_url}/${archive}" "${tmpdir}/${archive}"
download "${base_url}/${checksums}" "${tmpdir}/${checksums}"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "${tmpdir}/${checksums}")"
if [ -z "$expected" ]; then
  echo "checksum entry for ${archive} not found" >&2
  exit 1
fi

actual="$(sha256_file "${tmpdir}/${archive}")"
if [ "$expected" != "$actual" ]; then
  echo "checksum mismatch for ${archive}" >&2
  exit 1
fi

tar -xzf "${tmpdir}/${archive}" -C "${tmpdir}"

if [ ! -f "${tmpdir}/${BIN_NAME}" ]; then
  echo "archive did not contain ${BIN_NAME}" >&2
  exit 1
fi

if [ -d "${INSTALL_DIR}" ]; then
  :
elif [ -w "$(dirname "${INSTALL_DIR}")" ]; then
  mkdir -p "${INSTALL_DIR}"
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "${INSTALL_DIR}"
else
  echo "cannot create install directory ${INSTALL_DIR}" >&2
  exit 1
fi

if [ -w "${INSTALL_DIR}" ]; then
  install -m 0755 "${tmpdir}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
else
  if ! command -v sudo >/dev/null 2>&1; then
    echo "install directory is not writable and sudo is unavailable: ${INSTALL_DIR}" >&2
    exit 1
  fi
  sudo install -m 0755 "${tmpdir}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
fi

echo "installed ${BIN_NAME} to ${INSTALL_DIR}/${BIN_NAME}"
