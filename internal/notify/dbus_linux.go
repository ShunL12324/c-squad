//go:build linux

package notify

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"
)

func (d DBus) Send(ctx context.Context, m Message) error {
	conn, err := dbus.Connect(d.Address, dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer conn.Close()
	icon := d.Icon
	if _, err := os.Stat(icon); err != nil {
		icon = ""
	}
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	return obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		AppID, uint32(0), icon, m.Title, m.Body, []string{}, map[string]dbus.Variant{}, int32(-1)).Err
}
