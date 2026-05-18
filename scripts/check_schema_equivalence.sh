#!/usr/bin/env bash
# check_schema_equivalence.sh
#
# Structural equivalence guard: parses the canonical Postgres schema
# (schema/schema.sql) and the SQLite schema (cli/internal/sqlite/sqlite_schema.sql),
# then asserts that both define the same tables, columns, indexes, unique
# constraints, and search-index structures.
#
# Dialect differences (type names, CHECK syntax, non-search trigger syntax,
# IF NOT EXISTS) are expected and ignored. SQLite FTS triggers and Postgres
# TSVECTOR/GIN structures are pinned because they back user-visible search.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_SCHEMA="$repo_root/schema/schema.sql"
SQLITE_SCHEMA="$repo_root/cli/internal/sqlite/sqlite_schema.sql"

for f in "$POSTGRES_SCHEMA" "$SQLITE_SCHEMA"; do
  if [[ ! -f "$f" ]]; then
    echo "schema equivalence: missing file $f" >&2
    exit 1
  fi
done

errors=0

# ─── Postgres-only exceptions ─────────────────────────────────────────────────
# Tables, columns, and indexes that exist only in the Postgres schema (multi-user
# auth support). SQLite is single-user (local mode) and has no equivalent.
POSTGRES_ONLY_TABLES="api_keys users"
POSTGRES_ONLY_COLUMNS_projects="user_id"
POSTGRES_ONLY_COLUMNS_devices="user_id"
POSTGRES_ONLY_COLUMNS_project_paths="user_id"
POSTGRES_ONLY_COLUMNS_records="user_id search_vector"
POSTGRES_ONLY_COLUMNS_chat_session="user_id"
POSTGRES_ONLY_COLUMNS_chat_item="search_vector"
POSTGRES_ONLY_COLUMNS_sync_version="user_id"
SQLITE_ONLY_COLUMNS_sync_version="id"
POSTGRES_ONLY_INDEXES="idx_api_keys_hash idx_api_keys_user idx_devices_user idx_projects_user idx_records_user idx_records_fts idx_chat_item_fts"

# Helper: remove Postgres-only entries from a newline-separated list.
filter_pg_only() {
  local list="$1"
  shift
  local excludes=("$@")
  local result="$list"
  for ex in "${excludes[@]}"; do
    result=$(echo "$result" | grep -vxF "$ex")
  done
  echo "$result"
}

# ─── Extract table names ───────────────────────────────────────────────────────
# Matches: CREATE TABLE [IF NOT EXISTS] <name> (
extract_tables() {
  grep -iE '^CREATE\s+TABLE' "$1" \
    | sed -E 's/^CREATE[[:space:]]+TABLE[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?//' \
    | sed -E 's/[[:space:]]*\(.*//' \
    | sort
}

pg_tables_raw=$(extract_tables "$POSTGRES_SCHEMA")
sq_tables=$(extract_tables "$SQLITE_SCHEMA")

# Filter Postgres-only tables before comparison
# shellcheck disable=SC2086
pg_tables=$(filter_pg_only "$pg_tables_raw" $POSTGRES_ONLY_TABLES)

if [[ "$pg_tables" != "$sq_tables" ]]; then
  echo "schema equivalence FAILED: table lists differ" >&2
  echo "  Postgres (filtered): $(echo "$pg_tables" | tr '\n' ', ')" >&2
  echo "  SQLite:              $(echo "$sq_tables" | tr '\n' ', ')" >&2
  errors=$((errors + 1))
else
  echo "tables: OK ($(echo "$pg_tables" | wc -l | tr -d ' ') shared tables, $POSTGRES_ONLY_TABLES Postgres-only)"
fi

# ─── Extract columns per table ────────────────────────────────────────────────
# For each table, find the CREATE TABLE block and extract column names.
# A column line is one that starts (after leading whitespace) with an identifier
# followed by a space and more tokens, and does NOT start with a constraint
# keyword (UNIQUE, CHECK, PRIMARY, CONSTRAINT, FOREIGN).
extract_columns() {
  local file="$1"
  local table="$2"

  awk -v tbl="$table" '
    BEGIN { inside=0; paren_depth=0 }
    {
      line = $0
      # Detect start of the target CREATE TABLE
      if (!inside) {
        lower = tolower(line)
        # Match CREATE TABLE [IF NOT EXISTS] <table> (
        if (match(lower, /create[[:space:]]+table[[:space:]]+(if[[:space:]]+not[[:space:]]+exists[[:space:]]+)?/) && index(lower, tbl) > 0) {
          inside = 1
          # Count parens on this opening line (skip it for column extraction)
          n = split(line, chars, "")
          for (i = 1; i <= n; i++) {
            if (chars[i] == "(") paren_depth++
            if (chars[i] == ")") paren_depth--
          }
          next
        }
        next
      }
      # Inside the CREATE TABLE block
      # Track paren depth
      n = split(line, chars, "")
      for (i = 1; i <= n; i++) {
        if (chars[i] == "(") paren_depth++
        if (chars[i] == ")") paren_depth--
      }
      # Column definitions only appear at paren_depth 1 (top level of CREATE TABLE body)
      if (paren_depth == 1) {
        trimmed = line
        gsub(/^[[:space:]]+/, "", trimmed)
        # Skip constraint-only lines (UNIQUE, CHECK, PRIMARY KEY, CONSTRAINT, FOREIGN KEY)
        if (match(trimmed, /^(UNIQUE|CHECK|PRIMARY[[:space:]]+KEY|CONSTRAINT|FOREIGN[[:space:]]+KEY)[[:space:]]*[\(]/)) { next }
        # Column: starts with an identifier (letters/digits/underscores) followed by space + more tokens
        if (match(trimmed, /^[A-Za-z_][A-Za-z0-9_]*[[:space:]]+/)) {
          col = trimmed
          sub(/[[:space:]].*/, "", col)
          print col
        }
      }
      # Exit when we close the CREATE TABLE paren
      if (paren_depth <= 0) {
        inside = 0
        next
      }
    }
  ' "$file" | sort
}

for table in $pg_tables; do
  pg_cols=$(extract_columns "$POSTGRES_SCHEMA" "$table")
  sq_cols=$(extract_columns "$SQLITE_SCHEMA" "$table")

  # Filter Postgres-only columns for this table
  varname="POSTGRES_ONLY_COLUMNS_${table}"
  pg_only_cols="${!varname:-}"
  if [[ -n "$pg_only_cols" ]]; then
    # shellcheck disable=SC2086
    pg_cols=$(filter_pg_only "$pg_cols" $pg_only_cols)
  fi

  # Filter SQLite-only columns for this table
  varname="SQLITE_ONLY_COLUMNS_${table}"
  sq_only_cols="${!varname:-}"
  if [[ -n "$sq_only_cols" ]]; then
    # shellcheck disable=SC2086
    sq_cols=$(filter_pg_only "$sq_cols" $sq_only_cols)
  fi

  if [[ "$pg_cols" != "$sq_cols" ]]; then
    echo "schema equivalence FAILED: columns differ for table '$table'" >&2
    echo "  Postgres (filtered): $(echo "$pg_cols" | tr '\n' ', ')" >&2
    echo "  SQLite (filtered):   $(echo "$sq_cols" | tr '\n' ', ')" >&2
    errors=$((errors + 1))
  else
    col_count=$(echo "$pg_cols" | wc -l | tr -d ' ')
    extra=""
    if [[ -n "$pg_only_cols" ]]; then
      extra=" (+$pg_only_cols Postgres-only)"
    fi
    if [[ -n "$sq_only_cols" ]]; then
      extra="$extra (+$sq_only_cols SQLite-only)"
    fi
    echo "columns ($table): OK ($col_count shared columns$extra)"
  fi
done

# ─── Extract index names ──────────────────────────────────────────────────────
# Matches: CREATE [UNIQUE] INDEX [IF NOT EXISTS] <name> ON ...
extract_indexes() {
  grep -iE '^CREATE\s+(UNIQUE\s+)?INDEX' "$1" \
    | sed -E 's/^CREATE[[:space:]]+(UNIQUE[[:space:]]+)?INDEX[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?//' \
    | sed -E 's/[[:space:]]+ON.*//' \
    | sort
}

pg_indexes_raw=$(extract_indexes "$POSTGRES_SCHEMA")
sq_indexes=$(extract_indexes "$SQLITE_SCHEMA")

# Filter Postgres-only indexes before comparison
# shellcheck disable=SC2086
pg_indexes=$(filter_pg_only "$pg_indexes_raw" $POSTGRES_ONLY_INDEXES)

if [[ "$pg_indexes" != "$sq_indexes" ]]; then
  echo "schema equivalence FAILED: index lists differ" >&2
  echo "  Postgres (filtered): $(echo "$pg_indexes" | tr '\n' ', ')" >&2
  echo "  SQLite:              $(echo "$sq_indexes" | tr '\n' ', ')" >&2
  errors=$((errors + 1))
else
  idx_count=$(echo "$pg_indexes" | wc -l | tr -d ' ')
  pg_only_count=$(echo "$POSTGRES_ONLY_INDEXES" | wc -w | tr -d ' ')
  echo "indexes: OK ($idx_count shared indexes, $pg_only_count Postgres-only)"
fi

# ─── Extract UNIQUE constraints ───────────────────────────────────────────────
extract_unique() {
  grep -iE '^\s+UNIQUE\s*\(' "$1" \
    | sed -E 's/[[:space:]]+/ /g; s/^ //; s/,?$//' \
    | sed -E 's/\(user_id, /\(/g' \
    | tr '[:upper:]' '[:lower:]' \
    | sort
}

pg_unique=$(extract_unique "$POSTGRES_SCHEMA")
sq_unique=$(extract_unique "$SQLITE_SCHEMA")

if [[ "$pg_unique" != "$sq_unique" ]]; then
  echo "schema equivalence FAILED: UNIQUE constraints differ" >&2
  echo "  Postgres: $(echo "$pg_unique" | tr '\n' ' | ')" >&2
  echo "  SQLite:   $(echo "$sq_unique" | tr '\n' ' | ')" >&2
  errors=$((errors + 1))
else
  uniq_count=$(echo "$pg_unique" | wc -l | tr -d ' ')
  echo "unique constraints: OK ($uniq_count constraints)"
fi

# ─── Search index structures ─────────────────────────────────────────────────
# SQLite full-text search uses FTS5 virtual tables and triggers, while Postgres
# uses generated TSVECTOR columns plus GIN indexes. The generic table/index
# extraction above intentionally ignores SQLite virtual tables, so pin the
# search structures explicitly.
assert_contains() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if ! grep -qiE "$pattern" "$file"; then
    echo "schema equivalence FAILED: missing $label" >&2
    errors=$((errors + 1))
  fi
}

assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+VIRTUAL[[:space:]]+TABLE[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+records_fts[[:space:]]+USING[[:space:]]+fts5' "SQLite records_fts virtual table"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+VIRTUAL[[:space:]]+TABLE[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+chat_item_fts[[:space:]]+USING[[:space:]]+fts5' "SQLite chat_item_fts virtual table"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+records_fts_after_insert' "SQLite records FTS insert trigger"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+records_fts_after_update' "SQLite records FTS update trigger"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+records_fts_after_delete' "SQLite records FTS delete trigger"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+chat_item_fts_after_insert' "SQLite chat item FTS insert trigger"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+chat_item_fts_after_update' "SQLite chat item FTS update trigger"
assert_contains "$SQLITE_SCHEMA" '^CREATE[[:space:]]+TRIGGER[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+chat_item_fts_after_delete' "SQLite chat item FTS delete trigger"
assert_contains "$POSTGRES_SCHEMA" 'search_vector[[:space:]]+TSVECTOR' "Postgres search_vector columns"
assert_contains "$POSTGRES_SCHEMA" '^CREATE[[:space:]]+INDEX[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+idx_records_fts[[:space:]]+ON[[:space:]]+records[[:space:]]+USING[[:space:]]+GIN' "Postgres records GIN index"
assert_contains "$POSTGRES_SCHEMA" '^CREATE[[:space:]]+INDEX[[:space:]]+IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+idx_chat_item_fts[[:space:]]+ON[[:space:]]+chat_item[[:space:]]+USING[[:space:]]+GIN' "Postgres chat item GIN index"
if [[ $errors -eq 0 ]]; then
  echo "search indexes: OK (SQLite FTS5 tables/triggers, Postgres TSVECTOR/GIN)"
fi

# ─── Summary ──────────────────────────────────────────────────────────────────

if [[ $errors -gt 0 ]]; then
  echo "" >&2
  echo "schema equivalence check FAILED with $errors error(s)" >&2
  exit 1
fi

echo ""
echo "schema equivalence check passed"
