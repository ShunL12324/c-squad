#!/usr/bin/env bash
# TEMPORARY CI diagnostic for T400. Remove before a release candidate.
set -euo pipefail

if [[ $(uname -s) != Linux ]]; then
  echo 'T400 diagnostic unsupported: Linux tracepoints required' >&2
  exit 2
fi
for tool in bpftrace sudo timeout setsid; do
  command -v "$tool" >/dev/null || { echo "T400 diagnostic unavailable: $tool" >&2; exit 2; }
done

trace=$(mktemp)
program=$(mktemp)
testlog=$(mktemp)
tracer_pid=
stop_tracer() {
  if [[ -n $tracer_pid ]]; then
    kill -INT -- "-$tracer_pid" 2>/dev/null || true
    for _ in {1..20}; do
      kill -0 "$tracer_pid" 2>/dev/null || break
      sleep 0.1
    done
    kill -KILL -- "-$tracer_pid" 2>/dev/null || true
    wait "$tracer_pid" 2>/dev/null || true
    tracer_pid=
  fi
}
cleanup() {
  stop_tracer
  rm -f "$trace" "$program" "$testlog"
}
trap cleanup EXIT

# Check the runner's actual kernel event schema before compiling. An absent
# field or denied BPF access is a diagnostic limitation, never a test pass.
required=(
  'signal:signal_generate: sig pid comm result'
  'signal:signal_deliver: sig'
  'syscalls:sys_enter_wait4: upid'
  'syscalls:sys_exit_wait4: ret'
  'syscalls:sys_enter_waitid: which upid'
  'syscalls:sys_exit_waitid: ret'
)
for item in "${required[@]}"; do
  read -r event fields <<< "$item"
  schema=$(sudo -n bpftrace -lv "tracepoint:$event" 2>&1) || {
    echo "T400 diagnostic unavailable: $event: $schema" >&2
    exit 2
  }
  for field in $fields; do
    if ! grep -Eq "[[:space:]*]${field}([;[:space:]]|$)" <<< "$schema"; then
      echo "T400 diagnostic unavailable: $event lacks $field" >&2
      exit 2
    fi
  done
done

cat > "$program" <<'BPF'
BEGIN { printf("T400_TRACE_READY\n"); }
tracepoint:signal:signal_generate
/args->sig == 17 && str(args->comm) == "tmux: server"/
{
  printf("GEN ns=%llu sender_pid=%d sender_comm=%s target_pid=%d target_comm=%s result=%d\n",
         nsecs, pid, comm, args->pid, str(args->comm), args->result);
}
tracepoint:signal:signal_deliver
/args->sig == 17 && comm == "tmux: server"/
{
  printf("DELIVER ns=%llu target_pid=%d target_comm=%s\n", nsecs, pid, comm);
}
tracepoint:syscalls:sys_enter_wait4 /comm == "tmux: server"/
{ printf("WAIT4_ENTER ns=%llu server_pid=%d child_arg=%d\n", nsecs, pid, args->upid); }
tracepoint:syscalls:sys_exit_wait4 /comm == "tmux: server"/
{ printf("WAIT4_EXIT ns=%llu server_pid=%d ret=%d\n", nsecs, pid, args->ret); }
tracepoint:syscalls:sys_enter_waitid /comm == "tmux: server"/
{ printf("WAITID_ENTER ns=%llu server_pid=%d which=%d child_arg=%d\n", nsecs, pid, args->which, args->upid); }
tracepoint:syscalls:sys_exit_waitid /comm == "tmux: server"/
{ printf("WAITID_EXIT ns=%llu server_pid=%d ret=%d\n", nsecs, pid, args->ret); }
BPF

# One tracer for the whole focused invocation; no ptrace or per-fixture attach.
setsid sudo -n timeout --signal=INT --kill-after=5s 210s bpftrace -q "$program" > "$trace" 2>&1 &
tracer_pid=$!
ready=0
for _ in {1..150}; do
  if grep -qx T400_TRACE_READY "$trace"; then ready=1; break; fi
  if ! kill -0 "$tracer_pid" 2>/dev/null; then break; fi
  sleep 0.1
done
if (( ! ready )); then
  echo 'T400 diagnostic unavailable: tracer did not become ready' >&2
  head -c 4096 "$trace" >&2
  exit 2
fi
echo "T400 tracer ready: $(uname -sr); $(tmux -V); $(go version)"

status=0
timeout --signal=TERM --kill-after=5s 180s go test -race ./internal/squad -run '^TestShutdownCommandSurvivesFormatCharactersInPaths$' -count=20 -failfast -v > "$testlog" 2>&1 || status=$?
if ! kill -0 "$tracer_pid" 2>/dev/null; then
  echo 'T400 diagnostic incomplete: tracer exited during focused test' >&2
  head -c 4096 "$trace" >&2
  exit 2
fi
stop_tracer
echo "T400 focused test exit=$status"
if (( status != 0 )); then
  tail -c 65536 "$testlog"
  echo 'T400 bounded kernel events (last 400 lines):'
  grep -E '^(GEN|DELIVER|WAIT4_|WAITID_)' "$trace" | tail -n 400 || true
  echo "T400 event lines total=$(grep -Ec '^(GEN|DELIVER|WAIT4_|WAITID_)' "$trace" || true)"
else
  tail -n 8 "$testlog"
  echo "T400 event lines observed=$(grep -Ec '^(GEN|DELIVER|WAIT4_|WAITID_)' "$trace" || true)"
fi
if grep -Eiq 'lost events|perf buffer lost|dropped events' "$trace"; then
  echo 'T400 diagnostic incomplete: tracer reported lost events' >&2
  exit 2
fi
if ! grep -Eq '^(GEN|DELIVER|WAIT4_|WAITID_)' "$trace"; then
  echo 'T400 diagnostic incomplete: no tmux server events observed' >&2
  exit 2
fi
exit "$status"
