//go:build darwin

package notify

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var loadFrameworks = sync.OnceValue(func() error {
	for _, f := range []string{"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/UserNotifications.framework/UserNotifications"} {
		if _, err := purego.Dlopen(f, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return fmt.Errorf("cannot load %s: %v", f, err)
		}
	}
	return nil
})

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func cString(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), cString(s))
}

func goString(id objc.ID) string {
	if id == 0 {
		return ""
	}
	p := objc.Send[*byte](id, sel("UTF8String"))
	if p == nil {
		return ""
	}
	var out []byte
	for q := p; *q != 0; q = (*byte)(unsafe.Add(unsafe.Pointer(q), 1)) {
		out = append(out, *q)
	}
	return string(out)
}

func errorText(err objc.ID) string {
	if err == 0 {
		return ""
	}
	return goString(err.Send(sel("localizedDescription")))
}

// UNAuthorizationStatus values.
const (
	notDetermined = 0
	denied        = 1
)

// authorizationStatus reads csquad's notification permission.
func authorizationStatus(center objc.ID) (int, error) {
	status := make(chan int, 1)
	blk := objc.NewBlock(func(_ objc.Block, settings objc.ID) { status <- objc.Send[int](settings, sel("authorizationStatus")) })
	defer blk.Release()
	center.Send(sel("getNotificationSettingsWithCompletionHandler:"), blk)
	select {
	case s := <-status:
		return s, nil
	case <-time.After(10 * time.Second):
		return 0, fmt.Errorf("the notification center did not answer")
	}
}

// Post shows m with UNUserNotificationCenter. It must run inside
// csquad.app (the bundle's csquad). The first time, macOS asks the user
// for permission; Post waits for the answer up to wait. The question stays
// on screen only while this process runs: macOS removes it when the
// process ends, and does not ask again.
func Post(m Message, wait time.Duration) error {
	if err := loadFrameworks(); err != nil {
		return err
	}
	bundle := objc.ID(objc.GetClass("NSBundle")).Send(sel("mainBundle"))
	if id := goString(bundle.Send(sel("bundleIdentifier"))); id != BundleID {
		return fmt.Errorf("not running inside csquad.app (bundle id %q)", id)
	}
	center := objc.ID(objc.GetClass("UNUserNotificationCenter")).Send(sel("currentNotificationCenter"))
	if center == 0 {
		return fmt.Errorf("no notification center")
	}
	status, err := authorizationStatus(center)
	if err != nil {
		return err
	}
	if status == denied {
		return fmt.Errorf("notifications for csquad are off: turn them on in System Settings › Notifications › csquad")
	}
	if status != notDetermined {
		wait = 10 * time.Second // already answered: no question shows
	}
	type result struct {
		ok  bool
		err string
	}
	granted := make(chan result, 1)
	auth := objc.NewBlock(func(_ objc.Block, ok bool, err objc.ID) { granted <- result{ok, errorText(err)} })
	defer auth.Release()
	// UNAuthorizationOptionSound | UNAuthorizationOptionAlert
	center.Send(sel("requestAuthorizationWithOptions:completionHandler:"), uint64(1<<1|1<<2), auth)
	select {
	case r := <-granted:
		if !r.ok {
			if r.err == "" {
				r.err = "not allowed"
			}
			return fmt.Errorf("%s: allow csquad in System Settings › Notifications", r.err)
		}
	case <-time.After(wait):
		return fmt.Errorf("no answer to the permission request yet: allow csquad when macOS asks, or in System Settings › Notifications")
	}
	content := objc.ID(objc.GetClass("UNMutableNotificationContent")).Send(sel("new"))
	defer content.Send(sel("release"))
	content.Send(sel("setTitle:"), nsString(m.Title))
	content.Send(sel("setBody:"), nsString(m.Body))
	id := nsString(fmt.Sprintf("csquad-%d", time.Now().UnixNano()))
	req := objc.ID(objc.GetClass("UNNotificationRequest")).Send(sel("requestWithIdentifier:content:trigger:"), id, content, objc.ID(0))
	added := make(chan string, 1)
	done := objc.NewBlock(func(_ objc.Block, err objc.ID) { added <- errorText(err) })
	defer done.Release()
	center.Send(sel("addNotificationRequest:withCompletionHandler:"), req, done)
	select {
	case e := <-added:
		if e != "" {
			return fmt.Errorf("%s", e)
		}
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("the notification center did not answer")
	}
}
