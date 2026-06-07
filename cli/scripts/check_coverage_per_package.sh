#!/usr/bin/env bash
set -euo pipefail

threshold="${1:-95}"
profile="${2:-}"

if ! [[ "$threshold" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
	echo "coverage threshold must be numeric, got: $threshold" >&2
	exit 2
fi

package_filter='/(internal/repository/repositorytest|internal/repository/postgres|internal/s3client|internal/e2e|internal/cloude2e|migrations)$'

selected_packages() {
	go list ./... | grep -v -E "$package_filter"
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

if [[ "$profile" != "" ]]; then
	if [[ ! -f "$profile" ]]; then
		echo "coverage profile not found: $profile" >&2
		exit 2
	fi

	packages_file="${tmp_dir}/packages.txt"
	selected_packages >"$packages_file"

	if ! head -n 1 "$profile" | grep -q '^mode:'; then
		echo "invalid coverage profile $profile: missing mode line" >&2
		exit 2
	fi

	status=0
	checked=0
	while IFS= read -r pkg; do
		profile_path="${tmp_dir}/$(echo "$pkg" | tr '/.' '__').out"
		awk -v pkg="$pkg" '
			NR == 1 {
				print
				next
			}
			{
				split($1, range_parts, ":")
				file_path = range_parts[1]
				package_path = file_path
				sub("/[^/]+$", "", package_path)
				if (package_path == pkg) {
					print
				}
			}
		' "$profile" >"$profile_path"

		if [[ "$(wc -l <"$profile_path" | tr -d ' ')" -le 1 ]]; then
			continue
		fi
		checked=$((checked + 1))

		total_line="$(go tool cover -func="$profile_path" | awk '/^total:/ { print }')"
		if [[ -z "$total_line" ]]; then
			echo "failed to parse coverage for package $pkg from profile: $profile" >&2
			exit 2
		fi

		coverage_pct="$(awk '/^total:/ { gsub("%", "", $3); print $3 }' <<<"$total_line")"
		echo "$pkg coverage: ${coverage_pct}%"
		if ! awk -v got="$coverage_pct" -v minimum="$threshold" 'BEGIN { exit !(got+0 >= minimum+0) }'; then
			echo "coverage check failed for $pkg: ${coverage_pct}% < ${threshold}%" >&2
			status=1
		fi
	done <"$packages_file"

	if [[ "$checked" -eq 0 ]]; then
		echo "no package coverage data found in profile: $profile" >&2
		exit 2
	fi
	if [[ "$status" -ne 0 ]]; then
		exit "$status"
	fi

	echo "per-package coverage check passed: each tested package >= ${threshold}%"
	exit 0
fi

status=0
while IFS= read -r pkg; do
	profile_path="${tmp_dir}/$(echo "$pkg" | tr '/.' '__').out"
	# Race detector matches the aggregate run so concurrency bugs surface here too.
	test_output="$(go test -race -covermode=atomic -coverprofile="$profile_path" "$pkg" 2>&1)" || {
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

done < <(selected_packages)

if [[ "$status" -ne 0 ]]; then
	exit "$status"
fi

echo "per-package coverage check passed: each tested package >= ${threshold}%"
