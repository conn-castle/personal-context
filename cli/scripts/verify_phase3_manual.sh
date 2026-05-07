#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: ./scripts/verify_phase3_manual.sh [options]

Runs a full local Phase 3 verification flow:
- setup
- add
- show (text + json)
- edit
- move
- delete
- restore

Then prepares a previewable HTML slide bundle and opens it in the default browser.

Options:
  --no-open                Do not open the preview in a browser.
  --cleanup                Remove generated artifacts after completion.
  --artifacts-root <path>  Write artifacts to the provided directory.
  -h, --help               Show this help text.
USAGE
}

open_preview=1
cleanup=0
artifacts_root=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--no-open)
			open_preview=0
			shift
			;;
		--cleanup)
			cleanup=1
			shift
			;;
		--artifacts-root)
			if [[ $# -lt 2 ]]; then
				echo "--artifacts-root requires a path argument" >&2
				exit 2
			fi
			artifacts_root="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

if ! command -v go >/dev/null 2>&1; then
	echo "go is required but was not found on PATH" >&2
	exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cli_dir="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${cli_dir}/.." && pwd)"

if [[ -z "${artifacts_root}" ]]; then
	mkdir -p "${repo_root}/tmp"
	artifacts_root="$(mktemp -d "${repo_root}/tmp/phase3-manual-XXXXXX")"
else
	mkdir -p "${artifacts_root}"
	artifacts_root="$(cd "${artifacts_root}" && pwd)"
fi

if [[ "${cleanup}" -eq 1 ]]; then
	trap 'rm -rf "${artifacts_root}"' EXIT
fi

pc_home="${artifacts_root}/home"
input_a="${artifacts_root}/input-a"
input_b="${artifacts_root}/input-b"
input_edit="${artifacts_root}/input-edit"
preview_dir="${artifacts_root}/preview"
pc_bin="${artifacts_root}/pc"

mkdir -p "${input_a}/figures" "${input_a}/data" "${input_b}" "${input_edit}/figures" "${input_edit}/data" "${preview_dir}/figures"

cat >"${input_a}/slide.html" <<'HTML'
<html>
  <body>
    <h1>Slide A</h1>
    <img src="figures/plot.svg" alt="Plot 1" />
  </body>
</html>
HTML

cat >"${input_a}/notes.md" <<'NOTES'
Initial notes
NOTES

cat >"${input_a}/metadata.json" <<'META'
{"project_id":"phase3/manual","git_remote_url":"https://github.com/example/repo","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
META

cat >"${input_a}/figures/plot.svg" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" width="400" height="220" viewBox="0 0 400 220">
  <rect width="400" height="220" fill="#f0f7ff" />
  <text x="24" y="40" font-size="28" fill="#0f172a">Plot 1</text>
  <line x1="24" y1="180" x2="370" y2="180" stroke="#1d4ed8" stroke-width="4" />
  <polyline points="24,170 90,140 160,150 240,90 320,110 370,70" fill="none" stroke="#2563eb" stroke-width="6" />
</svg>
SVG

cat >"${input_a}/data/metrics.csv" <<'CSV'
x,y
1,2
CSV

cat >"${input_b}/slide.html" <<'HTML'
<html><body><h1>Slide B</h1></body></html>
HTML

cat >"${input_edit}/slide.html" <<'HTML'
<html>
  <body>
    <h1>Slide A edited</h1>
    <img src="figures/plot2.svg" alt="Plot 2" />
  </body>
</html>
HTML

cat >"${input_edit}/notes.md" <<'NOTES'
Edited notes
NOTES

cat >"${input_edit}/metadata.json" <<'META'
{"project_id":"phase3/edited","source_device_id":"phase3-device"}
META

cat >"${input_edit}/figures/plot2.svg" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" width="400" height="220" viewBox="0 0 400 220">
  <rect width="400" height="220" fill="#fff7ed" />
  <text x="24" y="40" font-size="28" fill="#7c2d12">Plot 2</text>
  <line x1="24" y1="180" x2="370" y2="180" stroke="#c2410c" stroke-width="4" />
  <polyline points="24,160 90,110 160,120 240,70 320,90 370,55" fill="none" stroke="#ea580c" stroke-width="6" />
</svg>
SVG

cat >"${input_edit}/data/metrics2.csv" <<'CSV'
x,y
3,4
CSV

(
	cd "${cli_dir}"
	go build -o "${pc_bin}" ./cmd/pc
)

run_pc() {
	PC_HOME="${pc_home}" "${pc_bin}" "$@"
}

fail() {
	echo "verification failed: $*" >&2
	exit 1
}

setup_out="$(run_pc setup)"
[[ "${setup_out}" == *"Personal Context initialized at"* ]] || fail "setup output did not include initialization message"

device_id="phase3-device"
run_pc device register "${device_id}" >/dev/null
run_pc project add "phase3/manual" >/dev/null
run_pc project add "phase3/edited" >/dev/null

id1="$(run_pc add --date 2025-03-01 --device "${device_id}" "${input_a}" | tr -d '\r\n')"
[[ "${id1}" =~ ^[0-9]{8}-[a-f0-9]{8}$ ]] || fail "unexpected slide ID format from first add: ${id1}"

id2="$(run_pc add --date 2025-03-01 --device "${device_id}" --project "phase3/manual" "${input_b}" | tr -d '\r\n')"
[[ "${id2}" =~ ^[0-9]{8}-[a-f0-9]{8}$ ]] || fail "unexpected slide ID format from second add: ${id2}"

show_text="$(run_pc show "${id1}")"
for expected in "ID:" "${id1}" "Project:" "phase3/manual" "Notes:" "Initial notes" "Figures:" "Data files:"; do
	[[ "${show_text}" == *"${expected}"* ]] || fail "show text output missing: ${expected}"
done

show_json="$(run_pc show --format json "${id1}")"
for expected in '"project_id": "phase3/manual"' '"filename": "plot.svg"' '"filename": "metrics.csv"'; do
	[[ "${show_json}" == *"${expected}"* ]] || fail "show json output missing: ${expected}"
done

run_pc edit "${id1}" "${input_edit}" >/dev/null

edited_text="$(run_pc show "${id1}")"
for expected in "Project:" "phase3/edited" "Notes:" "Edited notes" "plot2.svg" "metrics2.csv"; do
	[[ "${edited_text}" == *"${expected}"* ]] || fail "edited show output missing: ${expected}"
done
[[ "${edited_text}" != *"plot.svg"* ]] || fail "edited slide still lists old figure plot.svg"
[[ "${edited_text}" != *"metrics.csv"* ]] || fail "edited slide still lists old data file metrics.csv"

move_out="$(run_pc move "${id2}" --first)"
[[ "${move_out}" == *"moved"* ]] || fail "move output did not include success message"

extract_day_order() {
	local slide_id="$1"
	run_pc show "${slide_id}" | awk -F':' '/^DayOrder:/ {gsub(/^[ \t]+/, "", $2); print $2; exit}'
}

day_order_lt() {
	local left="$1"
	local right="$2"
	local first=""
	first="$(printf '%s\n%s\n' "${left}" "${right}" | LC_ALL=C sort | head -n1)"
	[[ "${left}" != "${right}" && "${first}" == "${left}" ]]
}

order_1="$(extract_day_order "${id1}")"
order_2="$(extract_day_order "${id2}")"
[[ -n "${order_1}" ]] || fail "missing day order for ${id1}"
[[ -n "${order_2}" ]] || fail "missing day order for ${id2}"
day_order_lt "${order_2}" "${order_1}" || fail "move --first did not reorder slides (${id2} day_order=${order_2}, ${id1} day_order=${order_1})"

delete_out="$(run_pc delete "${id1}")"
[[ "${delete_out}" == *"deleted"* ]] || fail "delete output did not include success message"

deleted_text="$(run_pc show "${id1}")"
if ! grep -qi 'deleted' <<<"${deleted_text}"; then
	fail "show output did not indicate deleted state after delete"
fi

restore_out="$(run_pc restore "${id1}")"
[[ "${restore_out}" == *"restored"* ]] || fail "restore output did not include success message"

restored_json="$(run_pc show --format json "${id1}")"
[[ "${restored_json}" == *'"deleted_at": null'* ]] || fail "restored slide still has non-null deleted_at"
[[ "${restored_json}" == *'Slide A edited'* ]] || fail "restored slide does not contain edited HTML"

cp "${input_edit}/slide.html" "${preview_dir}/slide.html"
cp "${input_edit}/figures/plot2.svg" "${preview_dir}/figures/plot2.svg"

cat >"${preview_dir}/viewer.html" <<'HTML'
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Phase 3 Slide Preview</title>
    <style>
      body {
        margin: 0;
        min-height: 100vh;
        display: grid;
        place-items: center;
        background: #f3f4f6;
        font-family: sans-serif;
      }

      #slide-boundary {
        width: min(90vw, 1280px);
        aspect-ratio: 16 / 9;
        border: 3px solid #dc2626;
        background: white;
        box-shadow: 0 10px 30px rgba(0, 0, 0, 0.12);
      }

      #slide-frame {
        width: 100%;
        height: 100%;
        border: 0;
      }
    </style>
  </head>
  <body>
    <div id="slide-boundary">
      <iframe id="slide-frame" title="Slide Preview" src="./slide.html"></iframe>
    </div>
  </body>
</html>
HTML

preview_file="${preview_dir}/viewer.html"
[[ -f "${preview_file}" ]] || fail "preview file was not created"

open_cmd=""
if [[ "${open_preview}" -eq 1 ]]; then
	if command -v open >/dev/null 2>&1; then
		open_cmd="open"
	elif command -v xdg-open >/dev/null 2>&1; then
		open_cmd="xdg-open"
	elif command -v wslview >/dev/null 2>&1; then
		open_cmd="wslview"
	else
		fail "could not find a browser opener command (open, xdg-open, wslview)"
	fi

	"${open_cmd}" "${preview_file}" >/dev/null 2>&1 || fail "failed to open preview file in browser with ${open_cmd}"
fi

cat <<SUMMARY
Phase 3 verification passed.
Artifacts root: ${artifacts_root}
PC_HOME: ${pc_home}
Slide IDs:
  - ${id1}
  - ${id2}
Preview file: ${preview_file}
Browser opened: $([[ "${open_preview}" -eq 1 ]] && echo yes || echo no)
Cleanup command: rm -rf "${artifacts_root}"
SUMMARY
