# Sourced only for an interactive, unbound npm invocation. Never edit shell rc.
# A marker outside the Node prefix keeps upgrades/nvm switches from nagging.
notice_state=${XDG_STATE_HOME:-${HOME:+$HOME/.local/state}}
if [ -n "$notice_state" ]; then
    notice_parent=$notice_state/csquad
    if mkdir -p "$notice_parent" 2>/dev/null &&
       mkdir "$notice_parent/npm-completion-notice-v1" 2>/dev/null; then
        printf '%s\n' 'csquad (npm): shell completion needs one-time setup.' \
            'Run: csquad completion install' \
            'Then run the printed loading line in your shell and add it to your shell startup file.' \
            'npm cannot activate completion in an already-open shell; no shell configuration was changed.' >&2
    fi
fi
