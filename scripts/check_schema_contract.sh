#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

required_paths=(
  "schema/schema.sql"
  "schema/schema-types.ts"
)

for required_path in "${required_paths[@]}"; do
  if [[ ! -f "$required_path" ]]; then
    echo "schema contract violation: missing canonical file '$required_path'" >&2
    exit 1
  fi
done

has_reference() {
  local workspace="$1"
  local needle="$2"

  if command -v rg >/dev/null 2>&1; then
    rg \
      --fixed-strings \
      --glob '!**/*.md' \
      --glob '!**/node_modules/**' \
      --glob '!**/.next/**' \
      --glob '!**/coverage/**' \
      --glob '!**/test-results/**' \
      "$needle" \
      "$workspace" >/dev/null
    return $?
  fi

  grep -R \
    --fixed-strings \
    --exclude='*.md' \
    --exclude-dir='node_modules' \
    --exclude-dir='.next' \
    --exclude-dir='coverage' \
    --exclude-dir='test-results' \
    -- "$needle" "$workspace" >/dev/null
}

for workspace in cli web; do
  if ! has_reference "$workspace" "schema/schema.sql"; then
    echo "schema contract violation: '$workspace' does not reference schema/schema.sql in executable/config files" >&2
    exit 1
  fi

  if ! has_reference "$workspace" "schema/schema-types.ts"; then
    echo "schema contract violation: '$workspace' does not reference schema/schema-types.ts in executable/config files" >&2
    exit 1
  fi
done

if find cli web \
  \( -path '*/node_modules/*' -o -path '*/.next/*' -o -path '*/coverage/*' -o -path '*/test-results/*' \) -prune -o \
  -type f \( -name 'schema.sql' -o -name 'schema-types.ts' \) -print | grep -q .; then
  echo "schema contract violation: workspace-local schema duplicates found under cli/ or web/" >&2
  exit 1
fi

echo "schema contract check passed"
