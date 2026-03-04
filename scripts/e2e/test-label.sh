#!/usr/bin/env bash
# E2E Test: --label Feature for Labeled Sessions
# Tests that ntm spawn --label creates labeled sessions (e.g., myproject--frontend)
# sharing the same project directory, and validates coexistence, JSON fields,
# targeted operations, backwards compatibility, and invalid label rejection.
#
# The "--" separator is the label delimiter in session names.

set -uo pipefail
# Note: NOT using -e so that assertion failures and test errors
# do not cause early exit. All tests run and results are summarized.

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFIX="e2e-lbl-$$"
LOGFILE="${SCRIPT_DIR}/test-label-$(date +%Y%m%d-%H%M%S).log"
VERBOSE=0

# Counters — use $((VAR + 1)) form, NOT ((VAR++)) which crashes under
# pipefail when the value is 0.
PASS=0
FAIL=0
TOTAL=0

# Track sessions for cleanup
CREATED_SESSIONS=()

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------

for arg in "$@"; do
    case "$arg" in
        --verbose|-v)
            VERBOSE=1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Helper functions
# ---------------------------------------------------------------------------

log() {
    local msg="[$(date '+%Y-%m-%dT%H:%M:%S')] $*"
    echo "$msg" >> "$LOGFILE"
    if [[ $VERBOSE -eq 1 ]]; then
        echo "$msg" >&2
    fi
}

info() {
    local msg="[$(date '+%Y-%m-%dT%H:%M:%S')] [INFO] $*"
    echo "$msg" >> "$LOGFILE"
    echo "$msg" >&2
}

error() {
    local msg="[$(date '+%Y-%m-%dT%H:%M:%S')] [ERROR] $*"
    echo "$msg" >> "$LOGFILE"
    echo "$msg" >&2
}

assert() {
    local description="$1"
    local actual="$2"
    local expected="$3"

    TOTAL=$((TOTAL + 1))

    if [[ "$actual" == "$expected" ]]; then
        PASS=$((PASS + 1))
        local msg="[PASS] $description"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 0
    else
        FAIL=$((FAIL + 1))
        local msg="[FAIL] $description (expected: '$expected', got: '$actual')"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 1
    fi
}

assert_contains() {
    local description="$1"
    local haystack="$2"
    local needle="$3"

    TOTAL=$((TOTAL + 1))

    if [[ "$haystack" == *"$needle"* ]]; then
        PASS=$((PASS + 1))
        local msg="[PASS] $description"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 0
    else
        FAIL=$((FAIL + 1))
        local msg="[FAIL] $description (substring '$needle' not found in output)"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 1
    fi
}

assert_not_empty() {
    local description="$1"
    local value="$2"

    TOTAL=$((TOTAL + 1))

    if [[ -n "$value" ]]; then
        PASS=$((PASS + 1))
        local msg="[PASS] $description (not empty)"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 0
    else
        FAIL=$((FAIL + 1))
        local msg="[FAIL] $description (was empty)"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 1
    fi
}

assert_fails() {
    local description="$1"
    shift
    local exit_code=0
    local output
    output=$("$@" 2>&1) || exit_code=$?

    TOTAL=$((TOTAL + 1))

    if [[ $exit_code -ne 0 ]]; then
        PASS=$((PASS + 1))
        local msg="[PASS] $description (exited $exit_code as expected)"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 0
    else
        FAIL=$((FAIL + 1))
        local msg="[FAIL] $description (expected failure but got exit 0)"
        echo "$msg" >> "$LOGFILE"
        echo "$msg" >&2
        return 1
    fi
}

# Spawn helper that pipes 'y' for directory creation prompts and tracks sessions
spawn_labeled() {
    local session="$1"
    shift
    CREATED_SESSIONS+=("$session")
    log "Spawning: ntm spawn $session $*"
    local output
    local rc=0
    output=$(echo "y" | ntm spawn "$session" "$@" 2>&1) || rc=$?
    log "spawn output ($rc): $output"
    return $rc
}

spawn_unlabeled() {
    local session="$1"
    shift
    CREATED_SESSIONS+=("$session")
    log "Spawning (unlabeled): ntm spawn $session $*"
    local output
    local rc=0
    output=$(echo "y" | ntm spawn "$session" "$@" 2>&1) || rc=$?
    log "spawn output ($rc): $output"
    return $rc
}

wait_for_session() {
    local session="$1"
    local timeout="${2:-10}"
    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        if tmux has-session -t "$session" 2>/dev/null; then
            return 0
        fi
        sleep 0.5
        elapsed=$((elapsed + 1))
    done
    return 1
}

# Get JSON list output from ntm
ntm_list_json() {
    ntm list --json 2>/dev/null
}

# Count sessions matching a prefix in ntm list JSON
count_sessions_with_prefix() {
    local prefix="$1"
    local json
    json=$(ntm_list_json)
    echo "$json" | jq -r --arg p "$prefix" '[.sessions[] | select(.name | startswith($p))] | length' 2>/dev/null || echo "0"
}

# Get a field from a named session in the ntm list JSON
get_session_field() {
    local session_name="$1"
    local field="$2"
    local json
    json=$(ntm_list_json)
    echo "$json" | jq -r --arg n "$session_name" --arg f "$field" '.sessions[] | select(.name == $n) | .[$f]' 2>/dev/null || echo ""
}

# ---------------------------------------------------------------------------
# Cleanup trap
# ---------------------------------------------------------------------------

cleanup() {
    info "--- Cleanup ---"
    # Kill all tracked sessions
    for session in "${CREATED_SESSIONS[@]}"; do
        log "Killing session: $session"
        tmux kill-session -t "$session" 2>/dev/null || true
    done
    # Also kill any remaining sessions matching the prefix
    local remaining
    remaining=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep "^${PREFIX}" || true)
    for session in $remaining; do
        log "Killing leftover session: $session"
        tmux kill-session -t "$session" 2>/dev/null || true
    done
    info "Cleanup complete"
}

trap cleanup EXIT

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------

preflight() {
    info "=== E2E TEST: label feature ==="
    info "Log file: $LOGFILE"
    info "Session prefix: $PREFIX"

    local missing=0
    if ! command -v ntm &>/dev/null; then
        error "ntm binary not found in PATH"
        missing=1
    fi
    if ! command -v tmux &>/dev/null; then
        error "tmux not installed"
        missing=1
    fi
    if ! command -v jq &>/dev/null; then
        error "jq not installed"
        missing=1
    fi
    if [[ $missing -ne 0 ]]; then
        error "Missing prerequisites, aborting"
        exit 1
    fi
    log "Prerequisites OK: ntm=$(command -v ntm), tmux=$(tmux -V), jq=$(jq --version)"
}

# ---------------------------------------------------------------------------
# Test 1: spawn --label creates correctly named session
# ---------------------------------------------------------------------------

test_spawn_labeled_session() {
    info ""
    info "--- Test 1: spawn --label creates session PREFIX--label ---"

    local base="$PREFIX"
    local expected_session="${PREFIX}--test-fe"

    if ! spawn_labeled "$base" --label test-fe --cc=1; then
        error "spawn with --label test-fe failed"
        assert "spawn --label test-fe creates session" "spawn_failed" "$expected_session"
        return
    fi

    sleep 2

    if tmux has-session -t "$expected_session" 2>/dev/null; then
        assert "spawn --label test-fe creates session ${expected_session}" "$expected_session" "$expected_session"
    else
        # Check what sessions exist
        local actual
        actual=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep "^${PREFIX}" || echo "none")
        assert "spawn --label test-fe creates session ${expected_session}" "$actual" "$expected_session"
    fi
}

# ---------------------------------------------------------------------------
# Test 2: second spawn with different label creates separate session
# ---------------------------------------------------------------------------

test_spawn_second_label() {
    info ""
    info "--- Test 2: second spawn with --label test-be creates separate session ---"

    local base="$PREFIX"
    local expected_session="${PREFIX}--test-be"

    if ! spawn_labeled "$base" --label test-be --cc=1; then
        error "spawn with --label test-be failed"
        assert "spawn --label test-be creates session" "spawn_failed" "$expected_session"
        return
    fi

    sleep 2

    if tmux has-session -t "$expected_session" 2>/dev/null; then
        assert "spawn --label test-be creates separate session ${expected_session}" "$expected_session" "$expected_session"
    else
        local actual
        actual=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep "^${PREFIX}--test-be" || echo "none")
        assert "spawn --label test-be creates separate session ${expected_session}" "$actual" "$expected_session"
    fi
}

# ---------------------------------------------------------------------------
# Test 3: both labeled sessions coexist
# ---------------------------------------------------------------------------

test_labeled_sessions_coexist() {
    info ""
    info "--- Test 3: both labeled sessions coexist (count == 2) ---"

    local count
    count=$(count_sessions_with_prefix "${PREFIX}--")

    assert "two labeled sessions coexist" "$count" "2"
}

# ---------------------------------------------------------------------------
# Test 4: CRITICAL — both labeled sessions share the same project directory
# ---------------------------------------------------------------------------

test_shared_project_directory() {
    info ""
    info "--- Test 4: CRITICAL — both labeled sessions share project directory ---"

    local dir_fe
    local dir_be
    dir_fe=$(get_session_field "${PREFIX}--test-fe" "working_directory")
    dir_be=$(get_session_field "${PREFIX}--test-be" "working_directory")

    log "Frontend working_directory: $dir_fe"
    log "Backend working_directory: $dir_be"

    assert_not_empty "frontend session has working_directory" "$dir_fe"
    assert_not_empty "backend session has working_directory" "$dir_be"

    if [[ -n "$dir_fe" && -n "$dir_be" ]]; then
        assert "both labeled sessions share same project directory" "$dir_fe" "$dir_be"
    fi
}

# ---------------------------------------------------------------------------
# Test 5: JSON output includes label and base_project fields
# ---------------------------------------------------------------------------

test_json_fields() {
    info ""
    info "--- Test 5: JSON output includes label and base_project fields ---"

    local json
    json=$(ntm_list_json)
    log "Full list JSON: $json"

    # Check label field for test-fe session
    local label_fe
    label_fe=$(echo "$json" | jq -r --arg n "${PREFIX}--test-fe" '.sessions[] | select(.name == $n) | .label' 2>/dev/null || echo "")
    assert "frontend session has label=test-fe" "$label_fe" "test-fe"

    # Check label field for test-be session
    local label_be
    label_be=$(echo "$json" | jq -r --arg n "${PREFIX}--test-be" '.sessions[] | select(.name == $n) | .label' 2>/dev/null || echo "")
    assert "backend session has label=test-be" "$label_be" "test-be"

    # Check base_project field
    local base_fe
    base_fe=$(echo "$json" | jq -r --arg n "${PREFIX}--test-fe" '.sessions[] | select(.name == $n) | .base_project' 2>/dev/null || echo "")
    assert "frontend session has base_project=${PREFIX}" "$base_fe" "$PREFIX"

    local base_be
    base_be=$(echo "$json" | jq -r --arg n "${PREFIX}--test-be" '.sessions[] | select(.name == $n) | .base_project' 2>/dev/null || echo "")
    assert "backend session has base_project=${PREFIX}" "$base_be" "$PREFIX"
}

# ---------------------------------------------------------------------------
# Test 6: send to specific labeled session works
# ---------------------------------------------------------------------------

test_send_to_labeled_session() {
    info ""
    info "--- Test 6: send to specific labeled session works ---"

    local session="${PREFIX}--test-fe"
    local rc=0

    ntm send "$session" --all "echo label-test-ping" >/dev/null 2>&1 || rc=$?

    if [[ $rc -eq 0 ]]; then
        assert "send to labeled session succeeds" "0" "0"
    else
        assert "send to labeled session succeeds" "$rc" "0"
    fi
}

# ---------------------------------------------------------------------------
# Test 7: kill removes only targeted session (the other survives)
# ---------------------------------------------------------------------------

test_kill_targeted_session() {
    info ""
    info "--- Test 7: kill removes only targeted session ---"

    local target="${PREFIX}--test-be"
    local survivor="${PREFIX}--test-fe"

    log "Killing: $target"
    ntm kill "$target" --force 2>/dev/null || true

    sleep 1

    # Target should be gone
    local target_exists="no"
    if tmux has-session -t "$target" 2>/dev/null; then
        target_exists="yes"
    fi
    assert "killed session ${target} is gone" "$target_exists" "no"

    # Survivor should still be alive
    local survivor_exists="no"
    if tmux has-session -t "$survivor" 2>/dev/null; then
        survivor_exists="yes"
    fi
    assert "surviving session ${survivor} still exists" "$survivor_exists" "yes"

    # Clean up survivor for next tests
    log "Killing survivor for clean slate: $survivor"
    ntm kill "$survivor" --force 2>/dev/null || true
    sleep 1
}

# ---------------------------------------------------------------------------
# Test 8: spawn without --label still works (backwards compatibility)
# ---------------------------------------------------------------------------

test_spawn_without_label() {
    info ""
    info "--- Test 8: spawn without --label still works (backwards compat) ---"

    local session="${PREFIX}-nolabel"

    if ! spawn_unlabeled "$session" --cc=1; then
        error "spawn without --label failed"
        assert "spawn without --label creates session" "spawn_failed" "$session"
        return
    fi

    sleep 2

    if tmux has-session -t "$session" 2>/dev/null; then
        assert "spawn without --label creates session" "$session" "$session"
    else
        assert "spawn without --label creates session" "missing" "$session"
    fi
}

# ---------------------------------------------------------------------------
# Test 9: unlabeled and labeled coexist (count == 2 with prefix match)
# ---------------------------------------------------------------------------

test_labeled_unlabeled_coexist() {
    info ""
    info "--- Test 9: unlabeled and labeled sessions coexist ---"

    # Create a labeled session alongside the existing unlabeled one
    local base="$PREFIX"
    local labeled_session="${PREFIX}--coexist"

    if ! spawn_labeled "$base" --label coexist --cc=1; then
        error "spawn with --label coexist failed"
        assert "labeled session created for coexistence test" "spawn_failed" "$labeled_session"
        return
    fi

    sleep 2

    # Count all sessions matching PREFIX (includes unlabeled and labeled)
    local count
    count=$(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -c "^${PREFIX}" || echo "0")

    log "Total sessions with prefix ${PREFIX}: $count"

    # We expect at least 2: the nolabel and the coexist
    if [[ $count -ge 2 ]]; then
        assert "unlabeled and labeled sessions coexist (count >= 2)" "true" "true"
    else
        assert "unlabeled and labeled sessions coexist (count >= 2)" "$count" ">=2"
    fi

    # Verify both specific sessions exist
    local has_unlabeled="no"
    local has_labeled="no"
    if tmux has-session -t "${PREFIX}-nolabel" 2>/dev/null; then
        has_unlabeled="yes"
    fi
    if tmux has-session -t "${PREFIX}--coexist" 2>/dev/null; then
        has_labeled="yes"
    fi

    assert "unlabeled session exists" "$has_unlabeled" "yes"
    assert "labeled session exists alongside it" "$has_labeled" "yes"

    # Cleanup these sessions
    ntm kill "${PREFIX}-nolabel" --force 2>/dev/null || true
    ntm kill "${PREFIX}--coexist" --force 2>/dev/null || true
    sleep 1
}

# ---------------------------------------------------------------------------
# Test 10: invalid labels are rejected
# ---------------------------------------------------------------------------

test_invalid_labels() {
    info ""
    info "--- Test 10: invalid labels are rejected ---"

    # Label containing "--" (the delimiter itself)
    assert_fails "label containing '--' is rejected" \
        ntm spawn "$PREFIX" --label "bad--label" --cc=1

    # Label starting with a dash
    assert_fails "label starting with '-' is rejected" \
        ntm spawn "$PREFIX" --label "-badstart" --cc=1

    # Label containing a space
    assert_fails "label containing space is rejected" \
        ntm spawn "$PREFIX" --label "has space" --cc=1
}

# ---------------------------------------------------------------------------
# Test 11: list shows label info when labels present
# ---------------------------------------------------------------------------

test_list_shows_label_info() {
    info ""
    info "--- Test 11: list shows label info when labels present ---"

    # Create a fresh labeled session for this test
    local base="$PREFIX"
    local expected_session="${PREFIX}--listcheck"

    if ! spawn_labeled "$base" --label listcheck --cc=1; then
        error "spawn with --label listcheck failed"
        assert "labeled session created for list test" "spawn_failed" "$expected_session"
        return
    fi

    sleep 2

    # Check JSON list output
    local json
    json=$(ntm_list_json)
    log "List JSON for label check: $json"

    # Verify the session appears with label info
    local found_label
    found_label=$(echo "$json" | jq -r --arg n "$expected_session" '.sessions[] | select(.name == $n) | .label' 2>/dev/null || echo "")
    assert "list JSON contains label field for labeled session" "$found_label" "listcheck"

    # Also check plain text list output for label information
    local list_text
    list_text=$(ntm list 2>/dev/null || echo "")
    log "List text output: $list_text"

    if [[ -n "$list_text" ]]; then
        # The list output should mention the labeled session name
        assert_contains "list text output contains labeled session name" "$list_text" "$expected_session"
    fi

    # Check ntm status on the labeled session for pane info
    local status_json
    status_json=$(ntm status "$expected_session" --json 2>/dev/null || echo "{}")
    log "Status JSON: $status_json"

    if echo "$status_json" | jq . >/dev/null 2>&1; then
        assert "status --json returns valid JSON for labeled session" "valid" "valid"
    else
        assert "status --json returns valid JSON for labeled session" "invalid" "valid"
    fi

    # Cleanup
    ntm kill "$expected_session" --force 2>/dev/null || true
    sleep 1
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

print_summary() {
    info ""
    info "==========================================="
    info " TEST SUMMARY: label feature"
    info "==========================================="
    info " TOTAL:   $TOTAL"
    info " PASSED:  $PASS"
    info " FAILED:  $FAIL"
    info " Log:     $LOGFILE"
    info "==========================================="

    if [[ $FAIL -gt 0 ]]; then
        info " RESULT: FAIL"
        return 1
    else
        info " RESULT: PASS"
        return 0
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    preflight

    test_spawn_labeled_session
    test_spawn_second_label
    test_labeled_sessions_coexist
    test_shared_project_directory
    test_json_fields
    test_send_to_labeled_session
    test_kill_targeted_session
    test_spawn_without_label
    test_labeled_unlabeled_coexist
    test_invalid_labels
    test_list_shows_label_info

    print_summary
}

main "$@"
