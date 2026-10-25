//go:build !darwin

package notify

import (
	"errors"
	"time"
)

// Post is macOS only.
func Post(Message, time.Duration) error { return errors.New("notify post runs on macOS only") }
