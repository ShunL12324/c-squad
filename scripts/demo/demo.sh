#!/usr/bin/env bash
# Records the README animation of csquad in an isolated environment.
#
#   scripts/demo/demo.sh setup    temp HOME, Claude config, csquad install, demo project
#   scripts/demo/demo.sh record   drive claude in tmux and record it (raw, in the temp dir)
#   scripts/demo/demo.sh render   trim it to docs/demo/demo.cast, render docs/images/demo.gif
#   scripts/demo/demo.sh clean    remove the tasks, the sessions and the temp dir
#   scripts/demo/demo.sh run CMD  run a command in the isolated environment
#
# Needs: tmux, asciinema 3, agg, python3, jq, go, claude, and the provider in
# ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN (forwarded, never printed).
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
ROOT=${DEMO_ROOT:-/tmp/csquad-demo}
H=$ROOT/home                    # the temp HOME: ~ in the recording
PROJECT=$H/demo/todo-cli
SOCK=csquad-demo                # own tmux server; the user's tmux is not touched
COLS=120
ROWS=32
MAIN_MODEL=${DEMO_MAIN_MODEL:-sonnet}
TASK_MODEL=${DEMO_TASK_MODEL:-haiku}
RAW=$ROOT/raw.cast
MARKS=$ROOT/marks
CAST=$REPO/docs/demo/demo.cast
GIF=$REPO/docs/images/demo.gif

CLAUDE_BIN=$(command -v claude)
GO_BIN=$(command -v go)

# denv runs a command in the isolated environment only.
denv() {
	env -i \
		HOME="$H" USER=demo LOGNAME=demo SHELL=/bin/bash \
		TERM=xterm-256color COLORTERM=truecolor LANG=C.UTF-8 LC_ALL=C.UTF-8 \
		PATH="$H/.csquad/bin:$(dirname "$CLAUDE_BIN"):$(dirname "$GO_BIN"):/usr/local/bin:/usr/bin:/bin" \
		CLAUDE_CONFIG_DIR="$H/.claude" CSQUAD_HOME="$H/.csquad" \
		ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL:?}" ANTHROPIC_AUTH_TOKEN="${ANTHROPIC_AUTH_TOKEN:?}" \
		DISABLE_AUTOUPDATER=1 CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
		GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod \
		"$@"
}

t() { denv tmux -L "$SOCK" "$@"; }

setup() {
	[ ! -e "$ROOT" ] || { echo "$ROOT exists; run clean first" >&2; exit 1; }
	mkdir -p "$H/.claude" "$PROJECT" "$ROOT/bin"
	(cd "$REPO" && go build -o "$ROOT/bin/csquad" ./cmd/csquad)

	printf '[user]\n\tname = Demo User\n\temail = demo@example.com\n[init]\n\tdefaultBranch = main\n' >"$H/.gitconfig"

	# A neutral user status line: the model and the directory.
	cat >"$H/.claude/statusline.sh" <<'EOF'
#!/bin/sh
jq -r '"\(.model.display_name)  \u001b[34m\(.workspace.current_dir | sub("^" + env.HOME; "~"))\u001b[0m"'
EOF
	chmod +x "$H/.claude/statusline.sh"
	jq -n --arg sl "$H/.claude/statusline.sh" --arg m "$TASK_MODEL" '{
		model: $m,
		statusLine: {type: "command", command: $sl, padding: 0},
		skipDangerousModePermissionPrompt: true,
		permissions: {allow: ["Bash", "Read", "Edit", "Write"]}
	}' >"$H/.claude/settings.json"

	# Onboarding, release notes and trust are accepted before the first start.
	local ver
	ver=$("$CLAUDE_BIN" --version | awk '{print $1}')
	jq -n --arg p "$PROJECT" --arg v "$ver" '{
		hasCompletedOnboarding: true, lastOnboardingVersion: $v, lastReleaseNotesSeen: $v,
		lastClawdEntranceVersion: $v, theme: "dark", autoUpdates: false, numStartups: 5,
		hasSeenAutoDefaultNotice: true, hasSeenAutoModeEntryWarning: true,
		hasResetAutoModeOptInForDefaultOffer: true, bypassPermissionsModeAccepted: true,
		officialMarketplaceAutoInstallAttempted: true,
		projects: {($p): {hasTrustDialogAccepted: true, hasCompletedProjectOnboarding: true,
			projectOnboardingSeenCount: 1}}
	}' >"$H/.claude/.claude.json"

	project
	denv "$ROOT/bin/csquad" install
}

# project writes ~/demo/todo-cli: a small Go todo CLI with three commits.
project() {
	cd "$PROJECT"
	denv git init -q
	cat >go.mod <<'EOF'
module example.com/todo-cli

go 1.22
EOF
	cat >todo.go <<'EOF'
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Item is one todo.
type Item struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// List is the todo list, in the order of add.
type List []Item

// Load reads the list from path; a missing file is an empty list.
func Load(path string) (List, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l List
	return l, json.Unmarshal(b, &l)
}

// Save writes the list to path.
func (l List) Save(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Add appends an open item.
func (l List) Add(text string) List {
	return append(l, Item{Text: text})
}

// Complete marks item n (1-based) done.
func (l List) Complete(n int) error {
	if n < 1 || n > len(l) {
		return fmt.Errorf("no item %d", n)
	}
	l[n-1].Done = true
	return nil
}
EOF
	cat >main.go <<'EOF'
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	path := os.Getenv("TODO_FILE")
	if path == "" {
		path = "todo.json"
	}
	if err := run(path, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "todo:", err)
		os.Exit(1)
	}
}

func run(path string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: todo add TEXT | list | done N")
	}
	l, err := Load(path)
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		l = l.Add(strings.Join(args[1:], " "))
	case "list":
		for i, it := range l {
			mark := " "
			if it.Done {
				mark = "x"
			}
			fmt.Printf("%d. [%s] %s\n", i+1, mark, it.Text)
		}
		return nil
	case "done":
		n, err := strconv.Atoi(strings.Join(args[1:], ""))
		if err != nil {
			return fmt.Errorf("done needs an item number")
		}
		if err := l.Complete(n); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return l.Save(path)
}
EOF
	cat >todo_test.go <<'EOF'
package main

import (
	"path/filepath"
	"testing"
)

func TestCompleteAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "todo.json")
	l := List{{Text: "buy milk"}, {Text: "write docs"}}
	if err := l.Complete(2); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Done || !got[1].Done {
		t.Fatalf("got %+v", got)
	}
}

func TestCompleteOutOfRange(t *testing.T) {
	if err := (List{}).Complete(1); err == nil {
		t.Fatal("want an error")
	}
}
EOF
	cat >README.md <<'EOF'
# todo-cli

A small todo list for the terminal. The list is in `todo.json`, or in `$TODO_FILE`.

```sh
go build -o todo .
./todo add "buy milk"
./todo list
./todo done 1
```
EOF
	printf 'todo\ntodo.json\n' >.gitignore
	denv git add go.mod todo.go main.go .gitignore
	denv git commit -qm "Todo CLI with add, list and done"
	denv git add todo_test.go
	denv git commit -qm "Test done and reload"
	denv git add README.md
	denv git commit -qm "README"
	denv go test ./... >/dev/null
}

# --- record -------------------------------------------------------------

screen() { t capture-pane -p -t demo; }

# busy: Claude is working on a reply.
busy() { screen | grep -q "esc to interrupt"; }

wait_idle() {
	sleep 3
	while busy; do sleep 1; done
}

# typ types text at a human pace, then Enter.
typ() {
	local s=$1 i
	for ((i = 0; i < ${#s}; i++)); do
		t send-keys -t demo -l -- "${s:i:1}"
		sleep "0.0$((RANDOM % 5 + 3))"
	done
	sleep 0.6
	t send-keys -t demo Enter
}

mark() { echo "$1 $(date +%s.%N)" >>"$MARKS"; }

# row prints the task's status line row on screen ("" when it has none).
row() { screen | grep -E "$1 ✻" | head -1; }

# wait_row waits until the row matches a regex. The status line refreshes
# every 10 s, so the screen, not csquad status, decides when to go on.
wait_row() { # task, regex, timeout in s
	local end=$((SECONDS + $3))
	until [[ $(row "$1") =~ $2 ]]; do
		((SECONDS < end)) || { echo "timeout: $1 not $2" >&2; return 1; }
		sleep 1
	done
}

record() {
	rm -f "$MARKS" "$RAW"
	# mouse and focus-events on, else Claude Code shows a hint about them.
	cat >"$ROOT/tmux.conf" <<'EOF'
set -g status off
set -g mouse on
set -g focus-events on
set -g escape-time 0
set -g default-terminal tmux-256color
set -as terminal-features ",xterm-256color:RGB"
EOF
	t -f "$ROOT/tmux.conf" new-session -d -s demo -x "$COLS" -y "$ROWS" -c "$PROJECT" "claude --model $MAIN_MODEL"
	until screen | grep -q "$MAIN_MODEL\|Sonnet\|Haiku"; do sleep 1; done
	sleep 2

	mark start
	denv "$ASCIINEMA" rec --headless -q --window-size "${COLS}x${ROWS}" --idle-time-limit 2 \
		-c "tmux -L $SOCK attach -t demo" "$RAW" &
	local rec=$!
	sleep 2

	typ 'create two tasks: add a `--done` filter to `list`, and add tests for `add`'
	mark fast
	wait_row T1 'working' 120
	wait_row T2 'working' 120
	wait_idle
	mark slow
	sleep 2
	typ 'after T1, a task to add a CHANGELOG entry for `--done`'
	mark fast
	wait_row T3 'waiting T1' 120
	wait_idle
	mark slow
	sleep 2
	mark fast
	wait_row T1 '[^a-z ] +(done|ready|idle)( |$)' 600
	mark slow
	sleep 4
	typ 'finish T1'
	mark fast
	wait_row T1 '^$' 300
	wait_idle
	mark slow
	sleep 5
	mark end
	t detach-client -s demo
	wait "$rec"
}

render() {
	python3 "$REPO/scripts/demo/trim.py" "$RAW" "$MARKS" "$CAST"
	# DejaVu Sans Mono has no ⏵ and ⎿: Noto Sans Symbols 2 and Noto Sans Mono CJK fill in.
	local fonts=$ROOT/fonts
	if [ ! -f "$fonts/NotoSansSymbols2-Regular.ttf" ]; then
		mkdir -p "$fonts"
		curl -fsSL -o "$fonts/NotoSansSymbols2-Regular.ttf" \
			https://github.com/google/fonts/raw/main/ofl/notosanssymbols2/NotoSansSymbols2-Regular.ttf
	fi
	"$AGG" -q --theme github-dark --font-dir "$fonts" \
		--font-family "DejaVu Sans Mono,Noto Sans Mono CJK SC,Noto Sans Symbols 2" \
		--font-size 14 --line-height 1.3 --idle-time-limit 2 --fps-cap 10 --last-frame-duration 4 \
		"$CAST" "$GIF"
	ls -l "$CAST" "$GIF"
}

clean() {
	t kill-server 2>/dev/null || true
	if [ -d "$H" ]; then
		denv csquad status --all --json 2>/dev/null | jq -r '.[]?.id' 2>/dev/null | while read -r n; do
			denv csquad cancel "$n" --discard --all || true
		done
		denv claude agents --json --all 2>/dev/null | jq -r '.[]? | select(.id != null) | .id' 2>/dev/null | while read -r id; do
			denv claude rm "$id" || true
		done
	fi
	rm -rf "${ROOT:?}"
}

ASCIINEMA=${ASCIINEMA:-asciinema}
AGG=${AGG:-agg}
case ${1:-} in
setup | record | render | clean) "$1" ;;
run) shift; cd "$PROJECT" && denv "$@" ;;
*) sed -n '2,11p' "$0" >&2; exit 2 ;;
esac
