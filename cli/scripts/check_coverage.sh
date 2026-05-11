#!/usr/bin/env bash
set -euo pipefail

threshold="${1:-95}"
profile="${2:-coverage.out}"

if ! [[ "$threshold" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
	echo "coverage threshold must be numeric, got: $threshold" >&2
	exit 2
fi

packages=()
while IFS= read -r pkg; do
	packages+=("$pkg")
done < <(go list ./... | grep -v -E '/(internal/repository/repositorytest|internal/repository/postgres|internal/s3client|internal/e2e|internal/cloude2e|migrations)$')
if [[ "${#packages[@]}" -eq 0 ]]; then
	echo "no packages selected for coverage" >&2
	exit 2
fi

# `-race` catches concurrency bugs in sync, syncengine, and pc serve handlers;
# `-covermode=atomic` is required when `-race` is enabled.
go test -race -covermode=atomic -coverprofile="$profile" "${packages[@]}"

total_line="$(go tool cover -func="$profile" | awk '/^total:/ { print }')"
if [[ -z "$total_line" ]]; then
	echo "failed to parse total coverage from profile: $profile" >&2
	exit 2
fi

coverage_pct="$(awk '/^total:/ { gsub("%", "", $3); print $3 }' <<<"$total_line")"
if ! awk -v got="$coverage_pct" -v minimum="$threshold" 'BEGIN { exit !(got+0 >= minimum+0) }'; then
	echo "coverage check failed: ${coverage_pct}% < ${threshold}%" >&2
	exit 1
fi

echo "coverage check passed: ${coverage_pct}% >= ${threshold}%"
