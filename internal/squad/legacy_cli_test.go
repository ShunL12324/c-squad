package squad

import "strings"

// The crash subprocess harness exercises the application layer directly. The
// production executable parses through Cobra; this parser is test-only.
func parse(args []string) ([]string, options) {
	p := []string{}
	o := options{}
	bools := map[string]bool{"detach": true, "code": true, "json": true, "force": true, "all": true, "fresh": true, "full": true, "help": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			kv := strings.SplitN(a[2:], "=", 2)
			if len(kv) == 2 {
				if kv[0] == "env" {
					o["env"] += kv[1] + "\x00"
				} else {
					o[kv[0]] = kv[1]
				}
			} else if !bools[kv[0]] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				if kv[0] == "env" {
					o["env"] += args[i] + "\x00"
				} else {
					o[kv[0]] = args[i]
				}
			} else {
				o[kv[0]] = "true"
			}
		} else {
			p = append(p, a)
		}
	}
	return p, o
}

// Run dispatches CLI arguments without the executable name.
// Commands write their output to standard output and return failures to the caller.
func Run(args []string) error {
	// Preserve opaque engine argv beyond --.
	var engineArgs []string
	for i, a := range args {
		if a == "--" {
			engineArgs = args[i+1:]
			args = args[:i]
			break
		}
	}
	p, o := parse(args)
	return Execute(p, o, engineArgs)
}
