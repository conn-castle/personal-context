# Helper functions for scripts/test-release.sh.

run_release_generation_test() {
  section "Release Generation Test"

  mock_bin="$tmp_dir/mock-bin"
  mkdir -p "$mock_bin"
  cat > "$mock_bin/go" << 'MOCK_GO'
#!/usr/bin/env bash
set -euo pipefail

log_path="${MOCK_GO_LOG:?MOCK_GO_LOG not set}"

output=""
ldflags=""
pkg=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    -ldflags)
      ldflags="$2"
      shift 2
      ;;
    *)
      pkg="$1"
      shift
      ;;
  esac
done

if [[ -z "$output" ]]; then
  echo "Error: Mock go called without -o" >&2
  exit 1
fi

if [[ -z "$pkg" ]]; then
  echo "Error: Mock go called without a package path" >&2
  exit 1
fi

printf '%s|%s|%s|%s|%s|%s|%s\n' "${PWD}" "${GOOS:-}" "${GOARCH:-}" "${CGO_ENABLED:-}" "$output" "$ldflags" "$pkg" >> "$log_path"

mkdir -p "$(dirname "$output")"
echo "mock binary: $output" > "$output"
chmod +x "$output"
MOCK_GO
  chmod +x "$mock_bin/go"

  build_success=0
  echo "Running build-release.sh in test environment..."

  if (
    export PATH="$mock_bin:$PATH"
    export MOCK_GO_LOG="$go_log"
    cd "$ROOT_DIR"
    PC_VERSION="$expected_version" DIST_DIR="$dist_dir" ./scripts/build-release.sh
  ) > "$tmp_dir/build.log" 2>&1; then
    build_success=1
    pass "build-release.sh executed successfully"
  else
    build_exit_code=$?
    fail "build-release.sh failed (exit code $build_exit_code)"
    echo "--- Build Log ---"
    cat "$tmp_dir/build.log"
    echo "-----------------"
  fi
}

run_build_invocation_details() {
  section "Build Invocation Details"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping build invocation verification because build-release.sh failed"
  elif [[ ! -s "$go_log" ]]; then
    fail "No go build invocations recorded by the mock"
  else
    invocation_count=$(wc -l < "$go_log" | tr -d ' ')
    if [[ "$invocation_count" -eq 4 ]]; then
      pass "Expected number of go build invocations (4)"
    else
      fail "Unexpected go build invocation count: $invocation_count"
    fi

    seen_darwin_arm64=0
    seen_darwin_amd64=0
    seen_linux_arm64=0
    seen_linux_amd64=0

    while IFS='|' read -r pwd_path goos goarch cgo output ldflags pkg; do
      if [[ "$pwd_path" != "$ROOT_DIR/cli" ]]; then
        fail "go build did not run from cli module: $pwd_path"
      fi

      if [[ -z "$goos" || -z "$goarch" ]]; then
        fail "GOOS/GOARCH not set for output: $output"
      fi

      if [[ "$cgo" != "0" ]]; then
        fail "CGO_ENABLED is not 0 for $goos/$goarch ($output)"
      fi

      if [[ "$pkg" != "./cmd/pc" ]]; then
        fail "go build package mismatch: $pkg"
      fi

      if [[ "$ldflags" != *"-X main.version=$expected_version"* ]]; then
        fail "Missing version ldflags for $goos/$goarch ($output)"
      fi

      if [[ "$ldflags" != *"-s"* || "$ldflags" != *"-w"* ]]; then
        fail "Missing strip flags (-s -w) for $goos/$goarch ($output)"
      fi

      case "$goos/$goarch/$output" in
        "darwin/arm64/$dist_dir/pc-darwin-arm64")
          seen_darwin_arm64=1
          ;;
        "darwin/amd64/$dist_dir/pc-darwin-amd64")
          seen_darwin_amd64=1
          ;;
        "linux/arm64/$dist_dir/pc-linux-arm64")
          seen_linux_arm64=1
          ;;
        "linux/amd64/$dist_dir/pc-linux-amd64")
          seen_linux_amd64=1
          ;;
        *)
          fail "Unexpected build target: GOOS=$goos GOARCH=$goarch output=$output"
          ;;
      esac
    done < "$go_log"

    if [[ "$seen_darwin_arm64" -eq 1 ]]; then pass "Build target present: darwin/arm64"; else fail "Missing build target: darwin/arm64"; fi
    if [[ "$seen_darwin_amd64" -eq 1 ]]; then pass "Build target present: darwin/amd64"; else fail "Missing build target: darwin/amd64"; fi
    if [[ "$seen_linux_arm64" -eq 1 ]]; then pass "Build target present: linux/arm64"; else fail "Missing build target: linux/arm64"; fi
    if [[ "$seen_linux_amd64" -eq 1 ]]; then pass "Build target present: linux/amd64"; else fail "Missing build target: linux/amd64"; fi
  fi
}

run_artifact_verification() {
  section "Artifact Verification"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping artifact verification because build-release.sh failed"
  else
    source_tarball="personal-context-${expected_version_no_v}.tar.gz"
    expected_artifacts=(
      "pc-darwin-arm64"
      "pc-darwin-amd64"
      "pc-linux-arm64"
      "pc-linux-amd64"
      "$source_tarball"
      "checksums.txt"
    )

    for artifact in "${expected_artifacts[@]}"; do
      if [[ -f "$dist_dir/$artifact" ]]; then
        pass "Artifact created: $artifact"
      else
        fail "Artifact missing: $artifact"
      fi
    done
  fi
}

run_source_tarball_verification() {
  section "Source Tarball Verification"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping source tarball verification because build-release.sh failed"
  else
    source_tarball="personal-context-${expected_version_no_v}.tar.gz"
    tar_list="$tmp_dir/source-tarball-contents.txt"
    prefix="personal-context-${expected_version_no_v}/"

    if tar -tzf "$dist_dir/$source_tarball" > "$tar_list" 2>/dev/null; then
      pass "Source tarball is readable: $source_tarball"
    else
      fail "Source tarball could not be read: $source_tarball"
    fi

    if awk -v prefix="$prefix" 'index($0, prefix) != 1 { exit 1 }' "$tar_list"; then
      pass "Source tarball entries are prefixed with $prefix"
    else
      fail "Source tarball entries missing expected prefix $prefix"
    fi

    if grep -qx "${prefix}README.md" "$tar_list"; then
      pass "Source tarball includes README.md"
    else
      fail "Source tarball missing README.md"
    fi

    if grep -qx "${prefix}cli/go.mod" "$tar_list"; then
      pass "Source tarball includes cli/go.mod"
    else
      fail "Source tarball missing cli/go.mod"
    fi
  fi
}

run_checksum_integrity() {
  section "Checksum Integrity"

  if [[ $build_success -ne 1 ]]; then
    warn "Skipping checksum verification because build-release.sh failed"
  elif [[ -f "$dist_dir/checksums.txt" ]]; then
    if grep -qE '^[a-f0-9]{64}[[:space:]]+' "$dist_dir/checksums.txt"; then
      pass "checksums.txt format is valid"
    else
      fail "checksums.txt format is invalid"
    fi

    (
      cd "$dist_dir"
      if command -v sha256sum >/dev/null 2>&1; then
        if sha256sum -c checksums.txt --status 2>/dev/null || sha256sum -c checksums.txt >/dev/null 2>&1; then
          pass "Checksums verified successfully (using sha256sum)"
        else
          fail "Checksum verification failed (using sha256sum)"
        fi
      elif command -v shasum >/dev/null 2>&1; then
        if shasum -a 256 -c checksums.txt >/dev/null 2>&1; then
          pass "Checksums verified successfully (using shasum)"
        else
          fail "Checksum verification failed (using shasum)"
        fi
      else
        fail "Neither sha256sum nor shasum found; cannot verify checksum content."
      fi
    )

    expected_checksum_files=(
      "pc-darwin-arm64"
      "pc-darwin-amd64"
      "pc-linux-arm64"
      "pc-linux-amd64"
      "personal-context-${expected_version_no_v}.tar.gz"
    )

    expected_checksum_list="$tmp_dir/expected-checksums-files.txt"
    actual_checksum_list="$tmp_dir/actual-checksums-files.txt"
    checksum_diff="$tmp_dir/checksums-files.diff"

    printf '%s\n' "${expected_checksum_files[@]}" | sort > "$expected_checksum_list"
    awk '{print $2}' "$dist_dir/checksums.txt" | sed 's|^\./||' | grep -v '^checksums.txt$' | sort > "$actual_checksum_list"

    if diff -u "$expected_checksum_list" "$actual_checksum_list" > "$checksum_diff"; then
      pass "checksums.txt entries match expected artifacts"
    else
      fail "checksums.txt entries do not match expected artifacts"
      cat "$checksum_diff"
    fi

    if awk '{print $2}' "$dist_dir/checksums.txt" | sed 's|^\./||' | grep -qx "checksums.txt"; then
      fail "checksums.txt contains a hash of itself (regression)"
    else
      pass "checksums.txt does not include itself"
    fi
  else
    fail "Skipping checksum verification (checksums.txt missing)"
  fi
}
