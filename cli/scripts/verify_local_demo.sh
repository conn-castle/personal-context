#!/usr/bin/env bash
set -euo pipefail

if ((BASH_VERSINFO[0] < 4)); then
	echo "error: bash 4+ is required (found ${BASH_VERSION}). On macOS: brew install bash" >&2
	exit 2
fi

usage() {
	cat <<'USAGE'
Usage: ./scripts/verify_local_demo.sh [options]

Runs a generalized local demo flow using the real `pc` binary:
- setup
- project set
- add 10 numbered slides
- delete 5 slides
- restore 1 of them
- move 1 remaining slide
- verify search, trash, show, project list, and doctor output

Then prepares a human-viewable HTML summary page plus persisted slide previews for
the first and last active slides, and optionally opens the summary in a browser.

Options:
  --no-open                Do not open the summary page in a browser.
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
	artifacts_root="$(mktemp -d "${repo_root}/tmp/local-demo-XXXXXX")"
else
	mkdir -p "${artifacts_root}"
	artifacts_root="$(cd "${artifacts_root}" && pwd)"
fi

if [[ "${cleanup}" -eq 1 ]]; then
	trap 'rm -rf "${artifacts_root}"' EXIT
fi

pc_home="${artifacts_root}/home"
inputs_root="${artifacts_root}/inputs"
preview_dir="${artifacts_root}/preview"
pc_bin="${artifacts_root}/pc"
render_helper="${artifacts_root}/render_local_demo.go"
active_list_file="${artifacts_root}/active.tsv"
deleted_list_file="${artifacts_root}/deleted.tsv"
first_slide_json="${artifacts_root}/first-slide.json"
last_slide_json="${artifacts_root}/last-slide.json"
summary_file="${preview_dir}/index.html"
first_slide_file="${preview_dir}/first-slide.html"
last_slide_file="${preview_dir}/last-slide.html"

mkdir -p "${inputs_root}" "${preview_dir}"

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

trim_crlf() {
	tr -d '\r\n'
}

contains_line() {
	local needle="$1"
	local line
	for line in "${@:2}"; do
		if [[ "${line}" == "${needle}" ]]; then
			return 0
		fi
	done
	return 1
}

open_summary() {
	local target="$1"
	local open_cmd=""

	if command -v open >/dev/null 2>&1; then
		open_cmd="open"
	elif command -v xdg-open >/dev/null 2>&1; then
		open_cmd="xdg-open"
	elif command -v wslview >/dev/null 2>&1; then
		open_cmd="wslview"
	else
		fail "could not find a browser opener command (open, xdg-open, wslview)"
	fi

	"${open_cmd}" "${target}" >/dev/null 2>&1 || fail "failed to open summary file in browser with ${open_cmd}"
}

write_slide_input() {
	local index="$1"
	local dir="${inputs_root}/slide-${index}"
	local title
	local body
	local notes

	title="$(printf 'Slide %02d' "${index}")"
	body="<p>Local demo content for ${title}.</p><p>Sequence number: ${index}.</p>"
	notes="Notes for ${title}."

	case "${index}" in
		1)
			body="<p>This demo created 10 slides, deleted slides 06-10, restored Slide 08, and moved Slide 04 after Slide 02.</p><p>Use the summary page to confirm the final order and trash membership.</p>"
			notes="Narrative slide for the local demo."
			;;
		8)
			body="<p>Expected final active order: 01, 02, 04, 03, 05, 08.</p><p>Expected trash: 06, 07, 09, 10.</p>"
			notes="Final-state explanation for the local demo."
			;;
		6|7|9|10)
			body="<p>${title} is expected to remain in trash at the end of the demo.</p><p>Sequence number: ${index}.</p>"
			notes="This slide should remain deleted after the demo completes."
			;;
	esac

	mkdir -p "${dir}"
	cat >"${dir}/slide.html" <<HTML
<html>
  <body>
    <h1>${title}</h1>
    ${body}
  </body>
</html>
HTML

	cat >"${dir}/notes.md" <<NOTES
${notes}
NOTES
}

declare -A slide_ids
declare -A slide_titles
date_value="2025-04-01"
project_id="demo/local"
device_id="demo-device"

setup_out="$(run_pc setup)"
[[ "${setup_out}" == *"Personal Context initialized at"* ]] || fail "setup output did not include initialization message"

run_pc device register "${device_id}" >/dev/null
run_pc project add "${project_id}" >/dev/null

for i in {1..10}; do
	write_slide_input "${i}"
	title="$(printf 'Slide %02d' "${i}")"
	slide_titles["${i}"]="${title}"
	id="$(run_pc add --date "${date_value}" --project "${project_id}" --device "${device_id}" "${inputs_root}/slide-${i}" | trim_crlf)"
	[[ "${id}" =~ ^[0-9]{8}-[a-f0-9]{8}$ ]] || fail "unexpected slide ID format from add ${i}: ${id}"
	slide_ids["${i}"]="${id}"
done

for i in 6 7 8 9 10; do
	delete_out="$(run_pc delete "${slide_ids[${i}]}")"
	[[ "${delete_out}" == *"deleted"* ]] || fail "delete output did not include success message for slide ${i}"
done

restore_out="$(run_pc restore "${slide_ids[8]}")"
[[ "${restore_out}" == *"restored"* ]] || fail "restore output did not include success message for slide 8"

move_out="$(run_pc move "${slide_ids[4]}" --after "${slide_ids[2]}")"
[[ "${move_out}" == *"moved"* ]] || fail "move output did not include success message for slide 4"

active_ids_raw="$(run_pc search --format ids "Slide")"
mapfile -t active_ids < <(printf '%s\n' "${active_ids_raw}" | sed '/^$/d')
expected_active=(
	"${slide_ids[1]}"
	"${slide_ids[2]}"
	"${slide_ids[4]}"
	"${slide_ids[3]}"
	"${slide_ids[5]}"
	"${slide_ids[8]}"
)

[[ "${#active_ids[@]}" -eq "${#expected_active[@]}" ]] || fail "expected ${#expected_active[@]} active slides, got ${#active_ids[@]}"
for idx in "${!expected_active[@]}"; do
	[[ "${active_ids[${idx}]}" == "${expected_active[${idx}]}" ]] || fail "active order mismatch at position $((idx + 1)): expected ${expected_active[${idx}]}, got ${active_ids[${idx}]}"
done

trash_out="$(run_pc trash)"
mapfile -t deleted_ids < <(grep -Eo '[0-9]{8}-[a-f0-9]{8}' <<<"${trash_out}")
expected_deleted=(
	"${slide_ids[6]}"
	"${slide_ids[7]}"
	"${slide_ids[9]}"
	"${slide_ids[10]}"
)

[[ "${#deleted_ids[@]}" -eq "${#expected_deleted[@]}" ]] || fail "expected ${#expected_deleted[@]} deleted slides in trash, got ${#deleted_ids[@]}"
for idx in "${!expected_deleted[@]}"; do
	[[ "${deleted_ids[${idx}]}" == "${expected_deleted[${idx}]}" ]] || fail "trash order mismatch at position $((idx + 1)): expected ${expected_deleted[${idx}]}, got ${deleted_ids[${idx}]}"
done

contains_line "${slide_ids[8]}" "${active_ids[@]}" || fail "restored slide 8 was not returned in active search results"
if contains_line "${slide_ids[8]}" "${deleted_ids[@]}"; then
	fail "restored slide 8 still appeared in trash output"
fi

restored_json="$(run_pc show --format json "${slide_ids[8]}")"
[[ "${restored_json}" == *'"deleted_at": null'* ]] || fail "restored slide 8 still has non-null deleted_at"
[[ "${restored_json}" == *'Expected final active order: 01, 02, 04, 03, 05, 08.'* ]] || fail "restored slide 8 JSON did not contain expected persisted HTML"

project_out="$(run_pc project list)"
[[ "${project_out}" == *"${project_id}"* ]] || fail "project list did not include ${project_id}"

doctor_out="$(run_pc doctor)"
[[ "${doctor_out}" == *"All checks passed."* ]] || fail "doctor output did not report success"

run_pc show --format json "${slide_ids[1]}" >"${first_slide_json}"
run_pc show --format json "${slide_ids[8]}" >"${last_slide_json}"

: >"${active_list_file}"
for i in 1 2 4 3 5 8; do
	printf '%02d\t%s\t%s\n' "${i}" "${slide_ids[${i}]}" "${slide_titles[${i}]}" >>"${active_list_file}"
done

: >"${deleted_list_file}"
for i in 6 7 9 10; do
	printf '%02d\t%s\t%s\n' "${i}" "${slide_ids[${i}]}" "${slide_titles[${i}]}" >>"${deleted_list_file}"
done

cat >"${render_helper}" <<'EOF'
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

type slideJSON struct {
	ID          string  `json:"id"`
	HTMLContent string  `json:"html_content"`
	Notes       *string `json:"notes"`
}

type listEntry struct {
	Number string
	ID     string
	Title  string
}

type summaryData struct {
	Active      []listEntry
	Deleted     []listEntry
	FirstTitle  string
	FirstNotes  string
	LastTitle   string
	LastNotes   string
}

func main() {
	var firstJSONPath string
	var lastJSONPath string
	var firstHTMLPath string
	var lastHTMLPath string
	var summaryPath string
	var activeListPath string
	var deletedListPath string

	flag.StringVar(&firstJSONPath, "first-json", "", "Path to the first active slide JSON file")
	flag.StringVar(&lastJSONPath, "last-json", "", "Path to the last active slide JSON file")
	flag.StringVar(&firstHTMLPath, "first-html", "", "Path to write the first active slide HTML")
	flag.StringVar(&lastHTMLPath, "last-html", "", "Path to write the last active slide HTML")
	flag.StringVar(&summaryPath, "summary", "", "Path to write the summary HTML")
	flag.StringVar(&activeListPath, "active-list", "", "Path to active slide TSV")
	flag.StringVar(&deletedListPath, "deleted-list", "", "Path to deleted slide TSV")
	flag.Parse()

	firstSlide := readSlide(firstJSONPath)
	lastSlide := readSlide(lastJSONPath)
	active := readList(activeListPath)
	deleted := readList(deletedListPath)

	writeFile(firstHTMLPath, firstSlide.HTMLContent)
	writeFile(lastHTMLPath, lastSlide.HTMLContent)

	if len(active) == 0 {
		panic("active list must not be empty")
	}

	data := summaryData{
		Active:     active,
		Deleted:    deleted,
		FirstTitle: active[0].Title,
		FirstNotes: deref(firstSlide.Notes),
		LastTitle:  active[len(active)-1].Title,
		LastNotes:  deref(lastSlide.Notes),
	}

	tmpl := template.Must(template.New("summary").Parse(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Local Demo Summary</title>
    <style>
      body {
        margin: 0;
        font-family: ui-sans-serif, system-ui, sans-serif;
        background: #f5f7fb;
        color: #0f172a;
      }

      main {
        max-width: 1200px;
        margin: 0 auto;
        padding: 32px 20px 48px;
      }

      h1, h2 {
        margin: 0 0 12px;
      }

      p {
        margin: 0 0 16px;
        line-height: 1.5;
      }

      .stats {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
        gap: 12px;
        margin: 24px 0;
      }

      .stat {
        background: white;
        border: 1px solid #dbe2f0;
        border-radius: 14px;
        padding: 16px;
      }

      .tables {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 16px;
        margin: 24px 0;
      }

      table {
        width: 100%;
        border-collapse: collapse;
        background: white;
        border: 1px solid #dbe2f0;
        border-radius: 14px;
        overflow: hidden;
      }

      th,
      td {
        text-align: left;
        padding: 10px 12px;
        border-bottom: 1px solid #e5eaf3;
      }

      tr:last-child td {
        border-bottom: 0;
      }

      .previews {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
        gap: 16px;
        margin-top: 24px;
      }

      .preview-card {
        background: white;
        border: 1px solid #dbe2f0;
        border-radius: 14px;
        padding: 16px;
      }

      iframe {
        width: 100%;
        aspect-ratio: 16 / 9;
        border: 2px solid #cbd5e1;
        border-radius: 12px;
        background: white;
      }

      code {
        font-family: ui-monospace, SFMono-Regular, monospace;
      }
    </style>
  </head>
  <body>
    <main>
      <h1>Local Demo Summary</h1>
      <p>This artifact was generated from persisted local state after a scripted CLI workflow.</p>
      <p>Actions performed: create 10 slides, delete slides 06-10, restore Slide 08, move Slide 04 after Slide 02.</p>

      <section class="stats" aria-label="Summary stats">
        <div class="stat">
          <strong>Active slides</strong>
          <div>{{len .Active}}</div>
        </div>
        <div class="stat">
          <strong>Deleted slides</strong>
          <div>{{len .Deleted}}</div>
        </div>
        <div class="stat">
          <strong>First active slide</strong>
          <div>{{.FirstTitle}}</div>
        </div>
        <div class="stat">
          <strong>Last active slide</strong>
          <div>{{.LastTitle}}</div>
        </div>
      </section>

      <section class="tables">
        <div>
          <h2>Active Slides</h2>
          <table id="active-order">
            <thead>
              <tr>
                <th>#</th>
                <th>Title</th>
                <th>ID</th>
              </tr>
            </thead>
            <tbody>
              {{range .Active}}
              <tr>
                <td>{{.Number}}</td>
                <td>{{.Title}}</td>
                <td><code>{{.ID}}</code></td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
        <div>
          <h2>Trash</h2>
          <table id="trash-list">
            <thead>
              <tr>
                <th>#</th>
                <th>Title</th>
                <th>ID</th>
              </tr>
            </thead>
            <tbody>
              {{range .Deleted}}
              <tr>
                <td>{{.Number}}</td>
                <td>{{.Title}}</td>
                <td><code>{{.ID}}</code></td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </section>

      <section class="previews">
        <article class="preview-card">
          <h2>{{.FirstTitle}}</h2>
          <p>{{.FirstNotes}}</p>
          <iframe id="first-slide-frame" title="First active slide preview" src="./first-slide.html"></iframe>
        </article>
        <article class="preview-card">
          <h2>{{.LastTitle}}</h2>
          <p>{{.LastNotes}}</p>
          <iframe id="last-slide-frame" title="Last active slide preview" src="./last-slide.html"></iframe>
        </article>
      </section>
    </main>
  </body>
</html>`))

	mkdirAll(filepath.Dir(summaryPath))
	file, err := os.Create(summaryPath)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := tmpl.Execute(file, data); err != nil {
		panic(err)
	}
}

func readSlide(path string) slideJSON {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var slide slideJSON
	if err := json.Unmarshal(data, &slide); err != nil {
		panic(err)
	}
	return slide
}

func readList(path string) []listEntry {
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	entries := make([]listEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			panic("expected TSV with 3 columns")
		}
		entries = append(entries, listEntry{
			Number: parts[0],
			ID:     parts[1],
			Title:  parts[2],
		})
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return entries
}

func writeFile(path string, content string) {
	mkdirAll(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(err)
	}
}

func mkdirAll(path string) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		panic(err)
	}
}

func deref(value *string) string {
	if value == nil {
		return "(none)"
	}
	return *value
}
EOF

(
	cd "${cli_dir}"
	go run "${render_helper}" \
		--first-json "${first_slide_json}" \
		--last-json "${last_slide_json}" \
		--first-html "${first_slide_file}" \
		--last-html "${last_slide_file}" \
		--summary "${summary_file}" \
		--active-list "${active_list_file}" \
		--deleted-list "${deleted_list_file}"
)

[[ -f "${summary_file}" ]] || fail "summary file was not created"
[[ -f "${first_slide_file}" ]] || fail "first slide preview file was not created"
[[ -f "${last_slide_file}" ]] || fail "last slide preview file was not created"

if [[ "${open_preview}" -eq 1 ]]; then
	open_summary "${summary_file}"
fi

cat <<SUMMARY
Local demo verification passed.
Artifacts root: ${artifacts_root}
PC_HOME: ${pc_home}
Summary file: ${summary_file}
First slide file: ${first_slide_file}
Last slide file: ${last_slide_file}
Browser opened: $([[ "${open_preview}" -eq 1 ]] && echo yes || echo no)
Cleanup command: rm -rf "${artifacts_root}"
SUMMARY
