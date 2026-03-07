#!/usr/bin/env bash
set -euo pipefail

threshold="${1:-95}"

if ! [[ "$threshold" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
	echo "coverage threshold must be numeric, got: $threshold" >&2
	exit 2
fi

status=0
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

while IFS= read -r pkg; do
	profile_path="${tmp_dir}/$(echo "$pkg" | tr '/.' '__').out"
	test_output="$(go test -covermode=atomic -coverprofile="$profile_path" "$pkg" 2>&1)" || {
		echo "$test_output"
		exit 1
	}
	echo "$test_output"

	if grep -q '\[no test files\]' <<<"$test_output"; then
		continue
	fi

	total_line="$(go tool cover -func="$profile_path" | awk '/^total:/ { print }')"
	if [[ -z "$total_line" ]]; then
		echo "failed to parse coverage for package $pkg" >&2
		exit 2
	fi

	coverage_pct="$(awk '/^total:/ { gsub("%", "", $3); print $3 }' <<<"$total_line")"
	if ! awk -v got="$coverage_pct" -v minimum="$threshold" 'BEGIN { exit !(got+0 >= minimum+0) }'; then
		echo "coverage check failed for $pkg: ${coverage_pct}% < ${threshold}%" >&2
		status=1
	fi

done < <(go list ./... | grep -v -E '/(internal/repository/repositorytest|internal/e2e|migrations)$')

if [[ "$status" -ne 0 ]]; then
	exit "$status"
fi

echo "per-package coverage check passed: each tested package >= ${threshold}%"
