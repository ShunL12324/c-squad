#!/usr/bin/env bash
# TEMPORARY CI diagnostic for T400. Remove before a release candidate.
set -euo pipefail

if [[ $(uname -s) != Linux ]]; then
  echo 'T400 diagnostic unsupported: Linux tracepoints required' >&2
  exit 2
fi
for tool in bpftrace sudo timeout setsid stdbuf; do
  command -v "$tool" >/dev/null || { echo "T400 diagnostic unavailable: $tool" >&2; exit 2; }
done

workdir=$(mktemp -d)
trace=$workdir/trace
program=$workdir/program
testlog=$workdir/testlog
# Root creates this file; precreating it under sticky /tmp can be denied by
# protected_regular even to root when its owner is the runner.
root_pidfile=$workdir/root.pid
wrapper_pid=
# The root-owned timeout is the new session's leader. Its PID, process start
# tick, and PGID are checked before any group signal, so sudo's own process
# group and unrelated runner processes cannot be targeted.
root_tracer() {
  sudo -n bash -c '
    set -euo pipefail
    read -r pid start < "$1"
    [[ $pid =~ ^[0-9]+$ && $start =~ ^[0-9]+$ ]] || exit 2
    [[ -r /proc/$pid/stat ]] || exit 1
    actual=$(awk "{print \$22}" "/proc/$pid/stat")
    [[ $actual == "$start" ]] || exit 2
    pgid=$(ps -o pgid= -p "$pid" | tr -d " ")
    [[ $pgid == "$pid" ]] || exit 2
    cmd=$(tr "\0" " " < "/proc/$pid/cmdline")
    [[ -z $cmd || $cmd == *"$2"* ]] || exit 2
    case $3 in
      check) exit 0 ;;
      int) kill -INT -- "-$pid" ;;
      kill) kill -KILL -- "-$pid" ;;
      *) exit 2 ;;
    esac
  ' bash "$root_pidfile" "$program" "$1"
}
stop_tracer() {
  if [[ -n $wrapper_pid ]]; then
    if [[ ! -s $root_pidfile ]]; then
      # The root launcher exits before execing bpftrace if identity creation
      # failed. Reap only a wrapper already confirmed exited; never guess a
      # privileged child PID or signal a process group without identity.
      for _ in {1..30}; do
        wrapper_state=$(ps -o stat= -p "$wrapper_pid" 2>/dev/null || true)
        if [[ -z $wrapper_state || $wrapper_state == Z* ]]; then
          wait "$wrapper_pid" 2>/dev/null || true
          wrapper_pid=
          return 0
        fi
        sleep 0.1
      done
      echo 'T400 diagnostic cleanup unconfirmed: root launcher has no identity file' >&2
      return 2
    fi
    if root_tracer check; then
      root_tracer int || return 2
    else
      check_status=$?
      (( check_status == 1 )) || return 2
    fi
    for _ in {1..50}; do
      if root_tracer check; then
        sleep 0.1
        continue
      else
        check_status=$?
        (( check_status == 1 )) || return 2
        break
      fi
    done
    if root_tracer check; then
      root_tracer kill || return 2
      for _ in {1..50}; do
        if root_tracer check; then
          sleep 0.1
          continue
        else
          check_status=$?
          (( check_status == 1 )) || return 2
          break
        fi
      done
    else
      check_status=$?
      (( check_status == 1 )) || return 2
    fi
    if root_tracer check; then
      echo 'T400 diagnostic cleanup failed: root tracer group still alive' >&2
      return 2
    else
      check_status=$?
      (( check_status == 1 )) || return 2
    fi
    read -r group_pid _ < "$root_pidfile"
    if sudo -n kill -0 -- "-$group_pid" 2>/dev/null; then
      echo 'T400 diagnostic cleanup failed: root tracer group has survivors' >&2
      return 2
    fi
    wait "$wrapper_pid" 2>/dev/null || true
    wrapper_pid=
  fi
}
cleanup() {
  rc=$?
  trap - EXIT
  if stop_tracer; then
    rm -rf -- "$workdir"
  else
    rc=2
    echo "T400 diagnostic artifacts retained for cleanup inspection: $workdir" >&2
  fi
  exit "$rc"
}
trap cleanup EXIT

# Check the runner's actual kernel event schema before compiling. An absent
# field or denied BPF access is a diagnostic limitation, never a test pass.
required=(
  'signal:signal_generate sig pid comm result'
  'signal:signal_deliver sig'
  'syscalls:sys_enter_wait4 upid'
  'syscalls:sys_exit_wait4 ret'
  'syscalls:sys_enter_waitid which upid'
  'syscalls:sys_exit_waitid ret'
)
for item in "${required[@]}"; do
  read -r event fields <<< "$item"
  schema=$(sudo -n timeout --signal=TERM --kill-after=2s 10s bpftrace -lv "tracepoint:$event" 2>&1) || {
    echo "T400 diagnostic unavailable: $event schema query failed" >&2
    printf '%.2048s\n' "$schema" >&2
    exit 2
  }
  for field in $fields; do
    if ! grep -Eq "[[:space:]*]${field}(\[[0-9]+\])?([;[:space:]]|$)" <<< "$schema"; then
      echo "T400 diagnostic unavailable: $event lacks $field" >&2
      printf '%.2048s\n' "$schema" >&2
      exit 2
    fi
  done
done

cat > "$program" <<'BPF'
BEGIN { printf("T400_TRACE_READY\n"); }
tracepoint:signal:signal_generate
/args->sig == 17 && args->comm == "tmux: server"/
{
  printf("GEN ns=%llu sender_pid=%d sender_comm=%s target_pid=%d target_comm=%s result=%d\n",
         nsecs, pid, comm, args->pid, args->comm, args->result);
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
sudo -n setsid bash -c '
  set -euo pipefail
  printf "%s %s\n" "$$" "$(awk "{print \$22}" "/proc/$$/stat")" > "$1"
  chmod 0644 "$1"
  exec timeout --signal=INT --kill-after=5s 210s stdbuf -oL -eL bpftrace -q "$2"
' bash "$root_pidfile" "$program" > "$trace" 2>&1 &
wrapper_pid=$!
ready=0
for _ in {1..150}; do
  if grep -qx T400_TRACE_READY "$trace"; then ready=1; break; fi
  if ! kill -0 "$wrapper_pid" 2>/dev/null; then break; fi
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
if ! root_tracer check; then
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
