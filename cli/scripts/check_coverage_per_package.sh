#!/usr/bin/env bash
set -euo pipefail

threshold="${1:-95}"
profile="${2:-coverage.out}"

if ! [[ "$threshold" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
	echo "coverage threshold must be numeric, got: $threshold" >&2
	exit 2
fi

if [[ ! -f "$profile" ]]; then
	echo "coverage profile not found: $profile" >&2
	exit 2
fi

status=0
module_path="$(go list -m)"
exclude_re='/(internal/repository/repositorytest|internal/repository/postgres|internal/s3client|internal/e2e|internal/cloude2e|migrations)$'
mode_line="$(sed -n '1p' "$profile")"
if [[ "$mode_line" != mode:* ]]; then
	echo "coverage profile missing mode line: $profile" >&2
	exit 2
fi
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

packages=()
declare -A packages_with_tests=()

while IFS=$'\t' read -r pkg test_files external_test_files; do
	if [[ "$pkg" =~ $exclude_re ]]; then
		continue
	fi
	packages+=("$pkg")
	if (( test_files + external_test_files > 0 )); then
		packages_with_tests["$pkg"]=1
	fi
done < <(go list -f '{{.ImportPath}}{{"\t"}}{{len .TestGoFiles}}{{"\t"}}{{len .XTestGoFiles}}' ./...)

if [[ "${#packages[@]}" -eq 0 ]]; then
	echo "no packages selected for coverage" >&2
	exit 2
fi

for pkg in "${packages[@]}"; do
	if [[ -z "${packages_with_tests[$pkg]+set}" ]]; then
		continue
	fi

	pkg_profile="${tmp_dir}/$(tr '/.' '__' <<<"$pkg").out"
	relative_pkg="${pkg#"$module_path"/}"
	{
		printf '%s\n' "$mode_line"
		awk -v prefix="${pkg}/" -v relative_prefix="${relative_pkg}/" 'index($1, prefix) == 1 || index($1, relative_prefix) == 1 { print }' "$profile"
	} > "$pkg_profile"

	if [[ "$(wc -l < "$pkg_profile")" -le 1 ]]; then
		echo "coverage profile has no data for package $pkg" >&2
		exit 2
	fi

	total_line="$(go tool cover -func="$pkg_profile" | awk '/^total:/ { print }')"
	if [[ -z "$total_line" ]]; then
		echo "failed to parse coverage for package $pkg" >&2
		exit 2
	fi

	coverage_pct="$(awk '/^total:/ { gsub("%", "", $3); print $3 }' <<<"$total_line")"
	if ! awk -v got="$coverage_pct" -v minimum="$threshold" 'BEGIN { exit !(got+0 >= minimum+0) }'; then
		echo "coverage check failed for $pkg: ${coverage_pct}% < ${threshold}%" >&2
		status=1
	else
		echo "coverage check passed for $pkg: ${coverage_pct}% >= ${threshold}%"
	fi
done

if [[ "$status" -ne 0 ]]; then
	exit "$status"
fi

echo "per-package coverage check passed: each tested package >= ${threshold}%"
