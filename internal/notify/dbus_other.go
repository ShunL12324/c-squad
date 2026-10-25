//go:build !linux

package notify

import (
	"context"
	"errors"
)

func (DBus) Send(context.Context, Message) error {
	return errors.New("D-Bus notifications are built for Linux only")
}
