// Package prompts holds the system prompts csquad appends to Claude Code
// sessions: the console the user publishes tasks from, and queued workers.
package prompts

import _ "embed"

// Console is appended to the interactive session started by bare csquad.
//
//go:embed console.md
var Console string

// Worker is appended to every background session the dispatcher launches.
//
//go:embed worker.md
var Worker string
