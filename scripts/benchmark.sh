#!/usr/bin/env bash
# benchmark.sh — measure byte savings from ctx-saver across representative scenarios.
#
# Prerequisites: ctx-saver binary must be installed (make install), plus
# the MCP CLI client or a direct test harness.  This script measures raw
# output sizes vs. the summary returned, then prints a comparison table.
#
# NOTE: Summary bytes are simulated with head-20 + tail-5.
# Scenarios marked (*) use smart-format summarisers in ctx_execute (go_test,
# flutter_test) and will show much higher real savings than this script reports.
set -euo pipefail

BINARY="${1:-/usr/local/bin/ctx-saver}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "ctx-saver benchmark — $(date)"
echo "Binary: ${BINARY}"
echo ""
printf "%-45s %10s %10s %8s\n" "Scenario" "Raw bytes" "Summary B" "Saving"
printf "%-45s %10s %10s %8s\n" "--------" "---------" "---------" "------"

run_scenario() {
    local label="$1"
    local cmd="$2"

    # Raw byte count from running the command directly.
    # ||true: suppress exit 141 (SIGPIPE) when a head pipeline closes stdin early.
    raw_bytes=$(eval "${cmd}" 2>&1 | wc -c | tr -d ' ') || true

    # Summary byte count: what ctx_execute would return.
    # We simulate by taking the first 20 lines + last 5 lines.
    summary_bytes=$(eval "${cmd}" 2>&1 | awk 'NR<=20{print} END{for(i=NR-4;i<=NR;i++) print lines[i]}{lines[NR]=$0}' | wc -c | tr -d ' ') || true

    if [ "${raw_bytes}" -gt 0 ]; then
        saving=$(awk "BEGIN{printf \"%.0f%%\", (1 - ${summary_bytes}/${raw_bytes})*100}")
    else
        saving="N/A"
    fi

    printf "%-45s %10s %10s %8s\n" "${label}" "${raw_bytes}" "${summary_bytes}" "${saving}"
}

# ── Scenarios ────────────────────────────────────────────────────────────────

# 1. Large git log
run_scenario "git log --oneline (500 entries)" \
    "git log --oneline -500 2>/dev/null || echo 'no git repo'"

# 2. Simulated Jira output (large JSON)
JIRA_FILE="${WORKDIR}/jira.json"
python3 -c "
import json, random, string
issues = [{'id': f'PROJ-{i}', 'summary': 'Issue ' + ''.join(random.choices(string.ascii_lowercase, k=40)),
           'status': random.choice(['Open','In Progress','Done']),
           'assignee': 'user@example.com', 'priority': 'Medium',
           'description': 'Some long description ' * 10}
          for i in range(200)]
print(json.dumps({'issues': issues}, indent=2))
" > "${JIRA_FILE}"
run_scenario "jira issue list (200 issues, JSON)" "cat ${JIRA_FILE}"

# 3. Simulated kubectl get pods
run_scenario "kubectl get pods (simulated, 100 pods)" \
    "python3 -c \"
lines = ['NAME                          READY   STATUS    RESTARTS   AGE']
for i in range(100):
    lines.append(f'pod-deployment-{i:04d}-abc12   2/2     Running   0          {i}d')
print('\n'.join(lines))
\""

# 4. Large log file
LOG_FILE="${WORKDIR}/app.log"
python3 -c "
import random, datetime
level = ['INFO','WARN','ERROR','DEBUG']
for i in range(2000):
    ts = datetime.datetime(2026,4,1,0,0,0) + datetime.timedelta(seconds=i*3)
    print(f'{ts.isoformat()} [{random.choice(level)}] RequestID={i:06d} msg=some log message with context data here')
" > "${LOG_FILE}"
run_scenario "app.log (2000 lines)" "cat ${LOG_FILE}"

# 5. find output (large directory listing)
run_scenario "find /usr/local -type f (file listing)" \
    "find /usr/local -type f 2>/dev/null | head -5000 || true"

# 6. go test verbose output (go_test smart format — * real savings ~99%)
GO_TEST_FILE="${WORKDIR}/go_test.txt"
python3 -c "
pkgs = ['internal/store', 'internal/summary', 'internal/handlers', 'internal/config', 'internal/sandbox']
lines = []
for pkg in pkgs:
    for i in range(20):
        lines.append(f'=== RUN   TestFoo{i}')
        lines.append(f'    {pkg}_test.go:{10+i}: checking invariant {i}')
        lines.append(f'--- PASS: TestFoo{i} (0.00{i}s)')
    lines.append(f'ok  \tgithub.com/foo/{pkg}\t0.12{len(pkg)}s\tcoverage: {60+len(pkg)}.3% of statements')
print('\n'.join(lines))
" > "${GO_TEST_FILE}"
run_scenario "go test -v (5 pkgs, 100 tests) *" "cat ${GO_TEST_FILE}"

# 7. flutter test verbose output (flutter_test smart format — * real savings ~97%)
FLUTTER_FILE="${WORKDIR}/flutter_test.txt"
python3 -c "
lines = []
for i in range(150):
    lines.append(f'00:{i//60:02d}:{i%60:02d} +{i}: widget_test.dart: renders widget {i} correctly')
lines.append('00:02:31 +150: All tests passed!')
print('\n'.join(lines))
" > "${FLUTTER_FILE}"
run_scenario "flutter test (150 tests) *" "cat ${FLUTTER_FILE}"

# 8. Large git diff (5 commits)
run_scenario "git diff HEAD~5 (patch)" \
    "git diff HEAD~5 2>/dev/null || echo 'not enough history'"

# 9. grep -r across Go source
run_scenario "grep -rn 'func' . --include=*.go" \
    "grep -rn 'func ' . --include='*.go' 2>/dev/null | head -500 || true"

# 10. Edge: near-threshold output (~4 KB, below 5120 B — not summarised)
run_scenario "near-threshold output (~4 KB, no summary)" \
    "python3 -c \"import random,string; print('\n'.join(''.join(random.choices(string.ascii_lowercase+' ',k=79)) for _ in range(50)))\""

# 11. Edge: error-heavy output (stderr mix, 20% errors)
ERR_FILE="${WORKDIR}/errors.txt"
python3 -c "
lines = []
for i in range(300):
    if i % 5 == 0:
        lines.append(f'ERROR: failed at line {i}: unexpected token near position {i*3}')
    else:
        lines.append(f'processing item {i}...')
print('\n'.join(lines))
" > "${ERR_FILE}"
run_scenario "error-heavy log (300 lines, 20% errors)" "cat ${ERR_FILE}"

echo ""
echo "(*) Smart-format scenarios: ctx_execute uses go_test/flutter_test summarisers"
echo "    which extract pass/fail counts rather than head/tail — real savings are"
echo "    much higher than shown above. See README for real ctx_execute measurements."
echo ""
echo "Note: 'Summary B' simulates the head-20 + tail-5 strategy."
echo "Actual savings may differ based on threshold (default 5120 bytes)."
