#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

version="${PC_VERSION:-dev}"
dist_dir="${DIST_DIR:-dist}"
version_no_v="${version#v}"
source_name="personal-context-${version_no_v}"
source_tar="${dist_dir}/${source_name}.tar"
source_tgz="${source_tar}.gz"

mkdir -p "$dist_dir"
dist_abs="$(cd "$dist_dir" && pwd)"

if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git not found; cannot generate source tarball" >&2
  exit 1
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "ERROR: not inside a git repository; cannot generate source tarball" >&2
  exit 1
fi

if ! command -v gzip >/dev/null 2>&1; then
  echo "ERROR: gzip not found; cannot generate source tarball" >&2
  exit 1
fi

build() {
  local goos="$1"
  local goarch="$2"
  local output="$3"
  (
    cd cli
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -o "${dist_abs}/${output}" -ldflags "-s -w -X main.version=${version}" ./cmd/pc
  )
}

build darwin arm64 pc-darwin-arm64
build darwin amd64 pc-darwin-amd64
build linux arm64 pc-linux-arm64
build linux amd64 pc-linux-amd64

git archive --format=tar --prefix="${source_name}/" HEAD > "$source_tar"
gzip -n -f "$source_tar"

if [[ ! -f "$source_tgz" ]]; then
  echo "ERROR: source tarball was not created at ${source_tgz}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && sha256sum ./* > checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$dist_dir" && rm -f checksums.txt && shasum -a 256 ./* > checksums.txt)
else
  echo "ERROR: sha256sum/shasum not found; cannot generate checksums.txt" >&2
  exit 1
fi
