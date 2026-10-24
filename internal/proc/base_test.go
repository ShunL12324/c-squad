package proc

import (
	"strings"
	"testing"
)

func TestBaseKeepsOnlyTheAllowlist(t *testing.T) {
	env := Env{"PATH=/bin", "HOME=/h", "LC_ALL=C", "https_proxy=p", "HTTPS_PROXY=P", "CSQUAD_HOME=/c", "CODEX_HOME=/x", `SystemRoot=C:\Windows`,
		"ANTHROPIC_AUTH_TOKEN=secret", "ANTHROPIC_BASE_URL=u", "CLAUDE_CODE_SESSION_ID=s", "OPENAI_API_KEY=k", "GIT_WORK_TREE=/g", "PATHX=1"}
	got := strings.Join(env.Base(), " ")
	if got != `PATH=/bin HOME=/h LC_ALL=C https_proxy=p HTTPS_PROXY=P CSQUAD_HOME=/c CODEX_HOME=/x SystemRoot=C:\Windows` {
		t.Errorf("%s", got)
	}
}
