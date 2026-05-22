package cli

// seedRecord defines a tutorial/demo record to be created by `pc seed`.
type seedRecord struct {
	HTMLContent string
	Notes       string
	ProjectID   string
}

// builtinSeeds returns the tutorial records that demonstrate how to use Personal Context.
// Each record is designed for 1920x1080 rendering with px-based sizing.
func builtinSeeds() []seedRecord {
	return []seedRecord{
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Welcome to Personal Context

Personal Context (` + "`pc`" + `) is a CLI-first engineering notebook designed for long-term use (10+ years).

### Key concepts
- **Records** are the atomic unit — each is an HTML file with optional figures, data files, and notes
- **Projects** organize records by work stream (e.g., ` + "`ml/sleep-staging`" + `)
- **Local-first** — everything works offline with SQLite; cloud sync is optional
- **Web UI** — browse your records in a three-panel viewer via ` + "`pc serve`" + `

### Getting started
1. Run ` + "`pc setup`" + ` to initialize your local environment
2. Run ` + "`pc add <folder>`" + ` to create your first record
3. Run ` + "`pc serve`" + ` + ` + "`pnpm dev`" + ` to browse in the web UI`,
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%); color: #1e293b; width: 1920px; height: 1080px; display: flex; align-items: center; justify-content: center; padding: 80px 120px; }
  .container { width: 100%; text-align: center; }
  h1 { font-size: 96px; font-weight: 700; margin-bottom: 24px; background: linear-gradient(90deg, #2563eb, #7c3aed); -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
  .subtitle { font-size: 38px; color: #64748b; margin-bottom: 80px; line-height: 1.4; }
  .features { display: grid; grid-template-columns: repeat(3, 1fr); gap: 40px; text-align: left; }
  .feature { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 24px; padding: 40px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .feature .icon { font-size: 48px; margin-bottom: 16px; }
  .feature h3 { font-size: 30px; font-weight: 600; color: #1e293b; margin-bottom: 12px; }
  .feature p { font-size: 22px; color: #64748b; line-height: 1.5; }
</style>
</head>
<body>
  <div class="container">
    <h1>Personal Context</h1>
    <p class="subtitle">A local-first engineering notebook for capturing your daily work<br>as browsable, searchable records.</p>
    <div class="features">
      <div class="feature">
        <div class="icon">&#128196;</div>
        <h3>HTML Records</h3>
        <p>Each entry is a rich HTML record with figures, data files, and markdown notes.</p>
      </div>
      <div class="feature">
        <div class="icon">&#128193;</div>
        <h3>Organized by Date</h3>
        <p>Records are sorted chronologically and grouped by project.</p>
      </div>
      <div class="feature">
        <div class="icon">&#9729;&#65039;</div>
        <h3>Optional Cloud Sync</h3>
        <p>Sync to Neon + S3 for multi-device access and a web UI.</p>
      </div>
    </div>
  </div>
</body>
</html>`,
		},
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Adding records

### Basic usage
` + "```bash" + `
pc add path/to/folder/
` + "```" + `

### With options
` + "```bash" + `
# Assign to a project
pc add my-record/ --project "ml/sleep-staging"

# Set a specific date (default is today)
pc add my-record/ --date 2026-03-01

# Place at a specific position
pc add my-record/ --first
pc add my-record/ --last
pc add my-record/ --after 20260310-abc12345
pc add my-record/ --before 20260310-def67890
` + "```" + `

### What happens when you add
1. ` + "`record.html`" + ` is read and stored as ` + "`html_content`" + `
2. ` + "`notes.md`" + ` (if present) is stored as markdown notes
3. ` + "`figures/*`" + ` files are copied to ` + "`~/personal-context/figures/{record_id}/`" + `
4. ` + "`data/*`" + ` files are copied to ` + "`~/personal-context/data/{record_id}/`" + ` with SHA-256 hashes
5. ` + "`metadata.json`" + ` fields are applied (project_id, git_remote_url, git_hash)
6. A unique record ID is generated: ` + "`YYYYMMDD-8hexchars`" + `

### Figure references
In your ` + "`record.html`" + `, reference figures with relative paths:
` + "```html" + `
<img src="figures/chart.png" alt="My chart">
` + "```" + `
` + "`pc add`" + ` validates that every ` + "`figures/`" + ` reference in the HTML has a matching file.`,
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #f8fafc; color: #1e293b; width: 1920px; height: 1080px; padding: 80px 110px; display: flex; flex-direction: column; }
  h1 { font-size: 76px; font-weight: 700; color: #2563eb; margin-bottom: 10px; }
  .subtitle { color: #64748b; font-size: 30px; margin-bottom: 56px; }
  .content { display: grid; grid-template-columns: 1fr 1fr; gap: 60px; flex: 1; }
  .steps { display: flex; flex-direction: column; gap: 44px; }
  .step { display: flex; gap: 28px; align-items: flex-start; }
  .step-num { background: #2563eb; color: #ffffff; font-weight: 700; font-size: 28px; width: 56px; height: 56px; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 4px; }
  .step-content h3 { font-size: 32px; font-weight: 600; color: #1e293b; margin-bottom: 10px; }
  .step-content p { font-size: 24px; color: #64748b; line-height: 1.4; }
  pre { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 14px; padding: 18px 24px; font-size: 22px; color: #0f172a; margin-top: 14px; font-family: 'SF Mono', 'Fira Code', monospace; white-space: pre-wrap; line-height: 1.5; }
  .folder { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 20px; padding: 44px 48px; display: flex; flex-direction: column; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .folder h4 { font-size: 22px; color: #64748b; text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 28px; }
  .folder pre { margin: 0; border: none; padding: 0; background: transparent; font-size: 24px; line-height: 1.7; color: #334155; }
</style>
</head>
<body>
  <h1>Adding Records</h1>
  <p class="subtitle">Create records from folders using <code style="color:#2563eb; font-weight:600">pc add</code></p>
  <div class="content">
    <div class="steps">
      <div class="step">
        <div class="step-num">1</div>
        <div class="step-content">
          <h3>Create a folder with record.html</h3>
          <p>The only required file is <code>record.html</code>.</p>
          <pre>mkdir my-record
echo '&lt;h1&gt;Hello&lt;/h1&gt;' &gt; my-record/record.html</pre>
        </div>
      </div>
      <div class="step">
        <div class="step-num">2</div>
        <div class="step-content">
          <h3>Add optional files</h3>
          <p>Include notes.md for presenter notes, figures/ for images referenced in the HTML, data/ for attached files, or metadata.json for project and git info.</p>
        </div>
      </div>
      <div class="step">
        <div class="step-num">3</div>
        <div class="step-content">
          <h3>Run pc add</h3>
          <pre>pc add my-record/
pc add my-record/ --project "ml/experiment"
pc add my-record/ --date 2026-03-01
pc add my-record/ --first</pre>
        </div>
      </div>
    </div>
    <div class="folder">
      <h4>Folder Structure</h4>
      <pre>my-record/
&#x251C;&#x2500;&#x2500; record.html        # optional
&#x251C;&#x2500;&#x2500; notes.md          # optional
&#x251C;&#x2500;&#x2500; metadata.json     # optional
&#x251C;&#x2500;&#x2500; figures/           # optional
&#x2502;   &#x251C;&#x2500;&#x2500; chart.png
&#x2502;   &#x2514;&#x2500;&#x2500; diagram.svg
&#x2514;&#x2500;&#x2500; data/              # optional
    &#x251C;&#x2500;&#x2500; results.csv
    &#x2514;&#x2500;&#x2500; metrics.json</pre>
    </div>
  </div>
</body>
</html>`,
		},
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Managing records

### Viewing
` + "```bash" + `
pc show <id>                 # human-readable output
pc show <id> --format json   # JSON output for scripting
` + "```" + `

### Editing
` + "`pc edit`" + ` does a **full replacement** — the new folder completely replaces the record's content, notes, figures, and data files. The ` + "`updated_at`" + ` timestamp is auto-bumped by a database trigger.

### Moving
Records are ordered by ` + "`(date, day_order, id)`" + `. The ` + "`--first`" + `, ` + "`--last`" + `, ` + "`--before`" + `, ` + "`--after`" + ` flags change the fractional index without touching other records.

### Soft delete
` + "`pc delete`" + ` sets ` + "`deleted_at`" + ` — the record is hidden but recoverable. ` + "`pc restore`" + ` clears it. ` + "`pc gc`" + ` permanently removes records deleted more than 30 days ago.

### Searching
Search matches against ` + "`html_content`" + `, ` + "`notes`" + `, and ` + "`project_id`" + `. Results are sorted by date (newest first).`,
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #f8fafc; color: #1e293b; width: 1920px; height: 1080px; padding: 70px 100px; display: flex; flex-direction: column; }
  h1 { font-size: 72px; font-weight: 700; color: #7c3aed; margin-bottom: 8px; }
  .subtitle { color: #64748b; font-size: 28px; margin-bottom: 36px; }
  .commands { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; flex: 1; }
  .cmd { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 16px; padding: 28px 32px; display: flex; flex-direction: column; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .cmd h3 { font-size: 26px; font-weight: 600; color: #7c3aed; margin-bottom: 8px; }
  .cmd p { font-size: 20px; color: #64748b; margin-bottom: 12px; line-height: 1.4; }
  .cmd pre { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 10px; padding: 12px 16px; font-size: 18px; color: #0f172a; font-family: 'SF Mono', 'Fira Code', monospace; line-height: 1.5; margin-top: auto; }
</style>
</head>
<body>
  <h1>Managing Records</h1>
  <p class="subtitle">View, edit, move, delete, and restore your records</p>
  <div class="commands">
    <div class="cmd">
      <h3>pc show &lt;id&gt;</h3>
      <p>Display record metadata, notes, figures, and data files.</p>
      <pre>pc show 20260310-ad5613b6
pc show 20260310-ad5613b6 --format json</pre>
    </div>
    <div class="cmd">
      <h3>pc edit &lt;id&gt; &lt;path&gt;</h3>
      <p>Full replacement of record content, notes, figures, and data files from a folder.</p>
      <pre>pc edit 20260310-ad5613b6 updated-record/</pre>
    </div>
    <div class="cmd">
      <h3>pc move &lt;id&gt;</h3>
      <p>Change a record's date or position within a day.</p>
      <pre>pc move &lt;id&gt; --date 2026-03-09
pc move &lt;id&gt; --first
pc move &lt;id&gt; --after &lt;other-id&gt;</pre>
    </div>
    <div class="cmd">
      <h3>pc delete / restore</h3>
      <p>Soft-delete a record (recoverable) or undo a deletion.</p>
      <pre>pc delete 20260310-ad5613b6
pc restore 20260310-ad5613b6</pre>
    </div>
    <div class="cmd">
      <h3>pc search &lt;query&gt;</h3>
      <p>Search by HTML content, notes, or project name.</p>
      <pre>pc search "experiment results"
pc search "loss" --project ml/sleep
pc search "TODO" --format json</pre>
    </div>
    <div class="cmd">
      <h3>pc trash / gc</h3>
      <p>List deleted records or permanently remove old trash (&gt;30 days).</p>
      <pre>pc trash
pc gc</pre>
    </div>
  </div>
</body>
</html>`,
		},
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Projects

Projects and source devices are first-class registries. Each record must name a registered project and source device explicitly.

### Registry workflow
` + "```bash" + `
pc project register "ml/sleep-staging"
pc device register "work-laptop"
pc add record1/ --project "ml/sleep-staging" --device "work-laptop"
pc project list
pc device list
` + "```" + `

### Web UI filtering
In the web UI, use the project picker in the left panel to filter records by project. Multiple projects can be selected.`,
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #f8fafc; color: #1e293b; width: 1920px; height: 1080px; padding: 80px 110px; display: flex; flex-direction: column; }
  h1 { font-size: 76px; font-weight: 700; color: #059669; margin-bottom: 10px; }
  .subtitle { color: #64748b; font-size: 30px; margin-bottom: 56px; }
  .grid { display: grid; grid-template-columns: 1.1fr 0.9fr; gap: 60px; flex: 1; }
  .left { display: flex; flex-direction: column; gap: 44px; }
  .section h2 { font-size: 36px; font-weight: 600; color: #059669; margin-bottom: 16px; }
  .section p { font-size: 26px; color: #64748b; line-height: 1.5; margin-bottom: 16px; }
  pre { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 14px; padding: 22px 28px; font-size: 23px; color: #0f172a; font-family: 'SF Mono', 'Fira Code', monospace; line-height: 1.6; }
  .right { display: flex; flex-direction: column; gap: 40px; }
  .right h2 { font-size: 36px; font-weight: 600; color: #059669; margin-bottom: 16px; }
  .right p { font-size: 26px; color: #64748b; line-height: 1.5; margin-bottom: 24px; }
  .examples { display: flex; gap: 18px; flex-wrap: wrap; }
  .example-tag { background: #ecfdf5; border: 1px solid #a7f3d0; border-radius: 32px; padding: 14px 32px; font-size: 26px; color: #059669; }
  .web-note { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 14px; padding: 24px 28px; margin-top: auto; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .web-note h3 { font-size: 24px; color: #059669; margin-bottom: 8px; }
  .web-note p { font-size: 22px; color: #64748b; line-height: 1.4; margin: 0; }
</style>
</head>
<body>
  <h1>Projects</h1>
  <p class="subtitle">Organize records by work stream using slash-convention project IDs</p>
  <div class="grid">
    <div class="left">
      <div class="section">
        <h2>Registry First</h2>
        <p>Register projects and devices, then pass explicit provenance when adding records.</p>
        <pre>pc project register "ml/sleep-staging"
pc device register "work-laptop"
pc add my-record/ --project "ml/sleep-staging" --device "work-laptop"

pc project list    # show all projects</pre>
      </div>
      <div class="section">
        <h2>Folder Metadata</h2>
        <p>Use <code style="color:#059669; font-weight:600">metadata.json</code> instead of flags when another tool prepares the folder.</p>
        <pre>{"project_id":"infra/deploy","source_device_id":"work-laptop"}</pre>
      </div>
    </div>
    <div class="right">
      <div>
        <h2>Naming Convention</h2>
        <p>Use slash-separated hierarchical names. No project table — just strings.</p>
        <div class="examples">
          <span class="example-tag">ml/sleep-staging</span>
          <span class="example-tag">infra/ci-pipeline</span>
          <span class="example-tag">personal-context/tutorial</span>
          <span class="example-tag">reading/papers</span>
          <span class="example-tag">work/quarterly-review</span>
          <span class="example-tag">side-projects/app</span>
        </div>
      </div>
      <div class="web-note">
        <h3>Web UI Filtering</h3>
        <p>Use the project picker in the navigation panel to filter records by one or more projects. Edit a record's project from the metadata bar above the viewer.</p>
      </div>
    </div>
  </div>
</body>
</html>`,
		},
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Web UI

The web UI is a Next.js app with a three-panel resizable layout.

### Local dev mode
` + "`pc serve`" + ` starts a Go HTTP server backed by local SQLite. Next.js detects ` + "`LOCAL_BACKEND_URL`" + ` and proxies to Go.

` + "```bash" + `
make dev-local
` + "```" + `

### Features
- **Record browser**: Navigate records grouped by date, filter by project
- **Preview**: 16:9 scaled iframe rendering of record HTML
- **Notes**: Edit markdown notes inline with full GFM + mermaid support
- **Attachments**: View figures with preview dialog, download data files
- **Sync**: 4-layer smart polling detects CLI changes automatically
- **Dark mode**: Toggle via settings`,
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #f8fafc; color: #1e293b; width: 1920px; height: 1080px; padding: 60px 110px; display: flex; flex-direction: column; }
  h1 { font-size: 72px; font-weight: 700; color: #ea580c; margin-bottom: 8px; }
  .subtitle { color: #64748b; font-size: 28px; margin-bottom: 36px; }
  .panels { display: grid; grid-template-columns: 1fr 2fr 1fr; gap: 24px; margin-bottom: 28px; }
  .panel { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 18px; padding: 28px 32px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .panel h3 { font-size: 20px; font-weight: 600; color: #ea580c; margin-bottom: 16px; text-transform: uppercase; letter-spacing: 0.06em; }
  .panel p { font-size: 22px; color: #64748b; line-height: 1.5; }
  .panel ul { list-style: none; padding: 0; }
  .panel ul li { font-size: 22px; color: #475569; padding: 5px 0; }
  .panel ul li::before { content: ""; display: inline-block; width: 10px; height: 10px; background: #f97316; border-radius: 50%; margin-right: 16px; vertical-align: middle; }
  .bottom { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; flex: 1; }
  .setup { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 18px; padding: 28px 36px; display: flex; flex-direction: column; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .setup h3 { font-size: 26px; font-weight: 600; color: #ea580c; margin-bottom: 16px; }
  .modes { display: flex; flex-direction: column; gap: 14px; }
  .mode-label { font-size: 20px; color: #64748b; margin-bottom: 4px; }
  pre { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 10px; padding: 12px 20px; font-size: 22px; color: #0f172a; font-family: 'SF Mono', 'Fira Code', monospace; }
  .sync { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 18px; padding: 28px 36px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .sync h3 { font-size: 26px; font-weight: 600; color: #ea580c; margin-bottom: 12px; }
  .sync p { font-size: 22px; color: #64748b; line-height: 1.5; }
  .sync ul { list-style: none; padding: 0; margin-top: 10px; }
  .sync ul li { font-size: 20px; color: #475569; padding: 4px 0; }
  .sync ul li::before { content: ""; display: inline-block; width: 8px; height: 8px; background: #f97316; border-radius: 50%; margin-right: 14px; vertical-align: middle; }
</style>
</head>
<body>
  <h1>Web UI</h1>
  <p class="subtitle">Browse your records in a three-panel viewer</p>
  <div class="panels">
    <div class="panel">
      <h3>Navigation</h3>
      <ul>
        <li>Date-grouped record list</li>
        <li>Strip or grid view</li>
        <li>Project filter</li>
        <li>Record count badge</li>
        <li>Trash view</li>
      </ul>
    </div>
    <div class="panel">
      <h3>Record Viewer</h3>
      <p>16:9 sandboxed iframe preview of the selected record's HTML content. Figures are resolved via the API — presigned S3 URLs in cloud mode, direct file serving in local mode.</p>
    </div>
    <div class="panel">
      <h3>Details</h3>
      <ul>
        <li>Markdown notes editor</li>
        <li>Figures with preview</li>
        <li>Data files with sizes</li>
        <li>Git link (if set)</li>
        <li>Delete / restore</li>
      </ul>
    </div>
  </div>
  <div class="bottom">
    <div class="setup">
      <h3>Starting the Web UI</h3>
      <div class="modes">
        <div>
          <p class="mode-label">Local mode (no cloud needed):</p>
          <pre>make dev-local</pre>
        </div>
        <div>
          <p class="mode-label">Cloud mode (Neon + S3):</p>
          <pre>make dev-cloud</pre>
        </div>
        <div>
          <p class="mode-label">Auto-detect:</p>
          <pre>make dev</pre>
        </div>
      </div>
    </div>
    <div class="sync">
      <h3>Smart Sync Polling</h3>
      <p>The web UI automatically detects changes made by the CLI:</p>
      <ul>
        <li>Manual refresh — always fires</li>
        <li>Interaction-driven — on click/navigation</li>
        <li>Tab visibility — when you return to the tab</li>
        <li>Idle polling — 60s active, 5min idle</li>
      </ul>
    </div>
  </div>
</body>
</html>`,
		},
		{
			ProjectID: "personal-context/tutorial",
			Notes: `## Cloud Sync & Backup

Cloud is entirely optional. Everything works locally without it.

### Setting up cloud
` + "```bash" + `
pc setup
` + "```" + `

### How sync works
1. **Push**: Local changes since last sync are upserted to Neon + S3
2. **Pull**: Cloud changes since last sync are upserted to local SQLite
3. **Conflicts**: Last writer wins (by ` + "`updated_at`" + `). Edit wins on timestamp tie.
4. **Auto-sync**: Runs automatically after ` + "`pc add`" + `, ` + "`pc edit`" + `, ` + "`pc delete`" + `, ` + "`pc restore`" + `, ` + "`pc move`" + `. Failure is non-fatal.

### Health checks
` + "```bash" + `
pc doctor
pc verify
pc verify --from-cloud
` + "```",
			HTMLContent: `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #f8fafc; color: #1e293b; width: 1920px; height: 1080px; padding: 60px 110px; display: flex; flex-direction: column; }
  h1 { font-size: 72px; font-weight: 700; color: #db2777; margin-bottom: 8px; }
  .subtitle { color: #64748b; font-size: 28px; margin-bottom: 36px; }
  .flow { display: flex; align-items: center; justify-content: center; gap: 24px; margin-bottom: 36px; }
  .node { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 18px; padding: 24px 40px; text-align: center; min-width: 280px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .node h4 { font-size: 28px; font-weight: 600; color: #db2777; }
  .node p { font-size: 20px; color: #64748b; margin-top: 6px; }
  .arrow { color: #94a3b8; font-size: 36px; }
  .commands { display: grid; grid-template-columns: 1fr 1fr; gap: 22px; flex: 1; }
  .cmd { background: #ffffff; border: 1px solid #e2e8f0; border-radius: 18px; padding: 28px 32px; display: flex; flex-direction: column; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .cmd h3 { font-size: 26px; font-weight: 600; color: #db2777; margin-bottom: 8px; }
  .cmd p { font-size: 21px; color: #64748b; margin-bottom: 12px; line-height: 1.4; }
  pre { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 10px; padding: 14px 20px; font-size: 20px; color: #0f172a; font-family: 'SF Mono', 'Fira Code', monospace; line-height: 1.5; margin-top: auto; }
</style>
</head>
<body>
  <h1>Cloud Sync &amp; Backup</h1>
  <p class="subtitle">Optional cloud sync for multi-device access and nightly backups</p>
  <div class="flow">
    <div class="node">
      <h4>Local SQLite</h4>
      <p>CLI writes here</p>
    </div>
    <div class="arrow">&harr;</div>
    <div class="node">
      <h4>Neon + S3</h4>
      <p>Cloud source of truth</p>
    </div>
    <div class="arrow">&rarr;</div>
    <div class="node">
      <h4>GitHub + S3</h4>
      <p>Nightly backup</p>
    </div>
  </div>
  <div class="commands">
    <div class="cmd">
      <h3>pc setup (cloud)</h3>
      <p>Interactive wizard for Neon URL, S3 bucket, and AWS credentials. Validates connectivity before saving.</p>
      <pre>pc setup
pc setup --remove-cloud</pre>
    </div>
    <div class="cmd">
      <h3>pc sync</h3>
      <p>Bidirectional push-then-pull. Also auto-runs after add, edit, delete, restore, and move.</p>
      <pre>pc sync</pre>
    </div>
    <div class="cmd">
      <h3>pc export / import</h3>
      <p>Git snapshot format for portable backups. Round-trip safe with <code style="color:#db2777; font-weight:600">pc verify</code>.</p>
      <pre>pc export --path ./backup
pc import ./backup
pc verify</pre>
    </div>
    <div class="cmd">
      <h3>pc fetch</h3>
      <p>Download data files from S3 on demand — all records, one record, project, or time window.</p>
      <pre>pc fetch &lt;record-id&gt;
pc fetch --all
pc fetch --project "ml/exp"
pc fetch --recent 2w</pre>
    </div>
  </div>
</body>
</html>`,
		},
	}
}
