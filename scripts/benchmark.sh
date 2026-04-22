#!/usr/bin/env bash
# benchmark.sh — measure byte savings from ctx-saver across representative scenarios.
#
# Prerequisites: ctx-saver binary must be installed (make install), plus
# the MCP CLI client or a direct test harness.  This script measures raw
# output sizes vs. the summary returned, then prints a comparison table.
set -euo pipefail

BINARY="${1:-/usr/local/bin/ctx-saver}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "ctx-saver benchmark — $(date)"
echo "Binary: ${BINARY}"
echo ""
printf "%-40s %10s %10s %8s\n" "Scenario" "Raw bytes" "Summary B" "Saving"
printf "%-40s %10s %10s %8s\n" "--------" "---------" "---------" "------"

run_scenario() {
    local label="$1"
    local cmd="$2"

    # Raw byte count from running the command directly.
    raw_bytes=$(eval "${cmd}" 2>&1 | wc -c | tr -d ' ')

    # Summary byte count: what ctx_execute would return.
    # We simulate by taking the first 20 lines + last 5 lines.
    summary_bytes=$(eval "${cmd}" 2>&1 | awk 'NR<=20{print} END{for(i=NR-4;i<=NR;i++) print lines[i]}{lines[NR]=$0}' | wc -c | tr -d ' ')

    if [ "${raw_bytes}" -gt 0 ]; then
        saving=$(awk "BEGIN{printf \"%.0f%%\", (1 - ${summary_bytes}/${raw_bytes})*100}")
    else
        saving="N/A"
    fi

    printf "%-40s %10s %10s %8s\n" "${label}" "${raw_bytes}" "${summary_bytes}" "${saving}"
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
    "find /usr/local -type f 2>/dev/null | head -5000"

echo ""
echo "Note: 'Summary B' simulates the head-20 + tail-5 strategy."
echo "Actual savings may differ based on threshold (default 5120 bytes)."
