package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandHelpPrintsUsageWithoutRunning(t *testing.T) {
	for _, args := range [][]string{{"install", "--help"}, {"update", "-h"}, {"launch", "--title", "x", "--help"}} {
		var out, errb bytes.Buffer
		code := run(args, strings.NewReader(""), &out, &errb)
		if code != 0 || !strings.HasPrefix(out.String(), "usage: csquad "+args[0]) || errb.Len() > 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errb.String())
		}
	}
}
