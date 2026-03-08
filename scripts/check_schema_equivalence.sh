#!/usr/bin/env bash
# check_schema_equivalence.sh
#
# Structural equivalence guard: parses the canonical Postgres schema
# (schema/schema.sql) and the SQLite schema (cli/internal/sqlite/sqlite_schema.sql),
# then asserts that both define the same tables, columns, indexes, and unique constraints.
#
# Dialect differences (type names, CHECK syntax, trigger syntax, IF NOT EXISTS)
# are expected and ignored. The guard checks structural alignment only.

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

# ─── Extract table names ───────────────────────────────────────────────────────
# Matches: CREATE TABLE [IF NOT EXISTS] <name> (
extract_tables() {
  grep -iE '^CREATE\s+TABLE' "$1" \
    | sed -E 's/^CREATE[[:space:]]+TABLE[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?//' \
    | sed -E 's/[[:space:]]*\(.*//' \
    | sort
}

pg_tables=$(extract_tables "$POSTGRES_SCHEMA")
sq_tables=$(extract_tables "$SQLITE_SCHEMA")

if [[ "$pg_tables" != "$sq_tables" ]]; then
  echo "schema equivalence FAILED: table lists differ" >&2
  echo "  Postgres: $(echo "$pg_tables" | tr '\n' ', ')" >&2
  echo "  SQLite:   $(echo "$sq_tables" | tr '\n' ', ')" >&2
  errors=$((errors + 1))
else
  echo "tables: OK ($(echo "$pg_tables" | wc -l | tr -d ' ') tables)"
fi

# ─── Extract columns per table ────────────────────────────────────────────────
# For each table, find the CREATE TABLE block and extract column names.
# A column line is one that starts (after leading whitespace) with an identifier
# followed by a type keyword (TEXT, INTEGER, SERIAL, BIGINT, DATE, TIMESTAMPTZ).
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
      # We are at paren_depth 1 for column definitions
      # Column lines start with whitespace + identifier + type
      trimmed = line
      gsub(/^[[:space:]]+/, "", trimmed)
      # Skip constraint-only lines (UNIQUE, CHECK, PRIMARY KEY as standalone)
      if (match(trimmed, /^(UNIQUE|CHECK|PRIMARY[[:space:]]+KEY)[[:space:]]*\(/)) next
      # Column: starts with a lowercase identifier followed by space + type
      if (match(trimmed, /^[a-z_]+[[:space:]]+(TEXT|INTEGER|SERIAL|BIGINT|DATE|TIMESTAMPTZ)/)) {
        col = trimmed
        sub(/[[:space:]].*/, "", col)
        print col
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

  if [[ "$pg_cols" != "$sq_cols" ]]; then
    echo "schema equivalence FAILED: columns differ for table '$table'" >&2
    echo "  Postgres: $(echo "$pg_cols" | tr '\n' ', ')" >&2
    echo "  SQLite:   $(echo "$sq_cols" | tr '\n' ', ')" >&2
    errors=$((errors + 1))
  else
    col_count=$(echo "$pg_cols" | wc -l | tr -d ' ')
    echo "columns ($table): OK ($col_count columns)"
  fi
done

# ─── Extract index names ──────────────────────────────────────────────────────
# Matches: CREATE INDEX [IF NOT EXISTS] <name> ON ...
extract_indexes() {
  grep -iE '^CREATE\s+INDEX' "$1" \
    | sed -E 's/^CREATE[[:space:]]+INDEX[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?//' \
    | sed -E 's/[[:space:]]+ON.*//' \
    | sort
}

pg_indexes=$(extract_indexes "$POSTGRES_SCHEMA")
sq_indexes=$(extract_indexes "$SQLITE_SCHEMA")

if [[ "$pg_indexes" != "$sq_indexes" ]]; then
  echo "schema equivalence FAILED: index lists differ" >&2
  echo "  Postgres: $(echo "$pg_indexes" | tr '\n' ', ')" >&2
  echo "  SQLite:   $(echo "$sq_indexes" | tr '\n' ', ')" >&2
  errors=$((errors + 1))
else
  idx_count=$(echo "$pg_indexes" | wc -l | tr -d ' ')
  echo "indexes: OK ($idx_count indexes)"
fi

# ─── Extract UNIQUE constraints ───────────────────────────────────────────────
extract_unique() {
  grep -iE '^\s+UNIQUE\s*\(' "$1" \
    | sed -E 's/[[:space:]]+/ /g; s/^ //; s/,?$//' \
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

# ─── Summary ──────────────────────────────────────────────────────────────────

if [[ $errors -gt 0 ]]; then
  echo "" >&2
  echo "schema equivalence check FAILED with $errors error(s)" >&2
  exit 1
fi

echo ""
echo "schema equivalence check passed"
