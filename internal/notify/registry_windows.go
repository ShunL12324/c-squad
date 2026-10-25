//go:build windows

package notify

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// SystemRegistry is HKEY_CURRENT_USER.
func SystemRegistry() Registry { return currentUser{} }

type currentUser struct{}

func (currentUser) Values(key string) (map[string]string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer k.Close()
	names, err := k.ReadValueNames(0)
	if err != nil {
		return nil, true, err
	}
	out := map[string]string{}
	for _, n := range names {
		if v, _, err := k.GetStringValue(n); err == nil {
			out[n] = v
		}
	}
	return out, true, nil
}

func (currentUser) SetValues(key string, values map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for n, v := range values {
		if err := k.SetStringValue(n, v); err != nil {
			return err
		}
	}
	return nil
}

func (currentUser) DeleteKey(key string) error {
	err := registry.DeleteKey(registry.CURRENT_USER, key)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}
