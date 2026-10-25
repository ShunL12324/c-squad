package notify

// DBus calls org.freedesktop.Notifications.Notify on the session bus.
type DBus struct {
	Address string // the session bus
	Icon    string // a PNG file; left out when missing
}

func (d DBus) Name() string { return "freedesktop notification (D-Bus session bus)" }
