# Helper functions for CI/release workflow consistency tests in scripts/test-release.sh.

run_workflow_consistency_tests() {
  section "Workflow Consistency Tests"

  local ci_workflow="$ROOT_DIR/.github/workflows/ci.yml"
  local release_workflow="$ROOT_DIR/.github/workflows/release.yml"
  local formula_template="$ROOT_DIR/.github/templates/personal-context-formula.rb"

  if [[ ! -f "$ci_workflow" ]]; then
    fail "ci.yml not found"
    return
  fi

  if [[ ! -f "$release_workflow" ]]; then
    fail "release.yml not found"
    return
  fi

  if [[ ! -f "$formula_template" ]]; then
    fail "personal-context-formula.rb template not found at .github/templates/"
    return
  fi

  local stable_tag_check_line publish_release_line
  stable_tag_check_line=$(grep -n 'name: Validate stable release tag format' "$release_workflow" | head -n1 | cut -d: -f1 || true)
  publish_release_line=$(grep -n 'name: Publish release' "$release_workflow" | head -n1 | cut -d: -f1 || true)

  if [[ -z "$stable_tag_check_line" ]]; then
    fail "workflow-consistency: missing stable release tag validation step"
  elif [[ -z "$publish_release_line" ]]; then
    fail "workflow-consistency: missing publish release step"
  elif (( stable_tag_check_line < publish_release_line )); then
    pass "workflow-consistency: stable tag validation runs before publish release"
  else
    fail "workflow-consistency: stable tag validation must run before publish release"
  fi

  if grep -q 'Formula/personal-context.rb' "$release_workflow"; then
    pass "workflow-consistency: release workflow updates personal-context formula"
  else
    fail "workflow-consistency: release workflow does not reference Formula/personal-context.rb"
  fi

  if grep -q 'Personal structured vault for searchable knowledge, data, files, and slides' "$formula_template" &&
    grep -q 'license "PolyForm-Noncommercial-1.0.0"' "$formula_template"; then
    pass "workflow-consistency: bootstrap formula uses approved description and license"
  else
    fail "workflow-consistency: bootstrap formula is missing approved description or license"
  fi

  if grep -q 'cp .github/templates/personal-context-formula.rb' "$release_workflow"; then
    pass "workflow-consistency: release workflow copies the bootstrap template"
  else
    fail "workflow-consistency: release workflow does not reference .github/templates/personal-context-formula.rb"
  fi

  if grep -q 'bump-personal-context-' "$release_workflow"; then
    pass "workflow-consistency: tap bump branch uses personal-context prefix"
  else
    fail "workflow-consistency: tap bump branch does not use personal-context prefix"
  fi

  if [[ -f "$ROOT_DIR/CHANGELOG.md" ]]; then
    pass "workflow-consistency: CHANGELOG.md exists"
  else
    fail "workflow-consistency: CHANGELOG.md missing (required by release workflow for release notes)"
  fi

  if [[ -f "$ROOT_DIR/cli/go.mod" ]]; then
    pass "workflow-consistency: cli/go.mod exists"
  else
    fail "workflow-consistency: cli/go.mod missing"
  fi
}
