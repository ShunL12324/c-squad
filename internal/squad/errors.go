package squad

import "errors"

// Shared domain failures support errors.Is through contextual wrapping and errors.Join.
// Detailed operation errors stay at the call site; wording is not a machine protocol.
var (
	ErrTeamStopped     = errors.New("team stopped")
	ErrStaleGeneration = errors.New("stale member generation or stopped team")
	ErrMasterRequired  = errors.New("only master may perform this operation")
	ErrNotFound        = errors.New("resource not found")
	ErrMemberLimit     = errors.New("member limit reached")
)
