package nativeconnect

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type userEnvironment struct{}

func SystemSelection() Store { return userEnvironment{} }

var selectionNames = [2]string{"CODEX_CLI_PATH", "MAGPIE_APP_SERVER_BRIDGE_CONFIG"}

func readValue(key registry.Key, name string) (Value, error) {
	text, kind, err := key.GetStringValue(name)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return Value{}, nil
	}
	value := Value{true, kind, text}
	if err != nil || !validValue(value) {
		return Value{}, ErrUnavailable
	}
	return value, nil
}

func (userEnvironment) Read() (Selection, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return Selection{}, nil
	}
	if err != nil {
		return Selection{}, ErrUnavailable
	}
	defer key.Close()
	cli, err := readValue(key, selectionNames[0])
	if err != nil {
		return Selection{}, err
	}
	profile, err := readValue(key, selectionNames[1])
	return Selection{cli, profile}, err
}

func writeValue(key registry.Key, name string, value Value) error {
	if !value.Present {
		err := key.DeleteValue(name)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		return err
	}
	if value.Kind == registry.EXPAND_SZ {
		return key.SetExpandStringValue(name, value.Text)
	}
	return key.SetStringValue(name, value.Text)
}

func (s userEnvironment) Replace(before, after Selection) error {
	actual, err := s.Read()
	if err != nil || actual != before || !validSelection(after) {
		return ErrConflict
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return ErrUnavailable
	}
	defer key.Close()
	oldValues := [2]Value{before.CLI, before.Profile}
	newValues := [2]Value{after.CLI, after.Profile}
	for i, name := range selectionNames {
		// Recheck immediately before each individual write. Windows registry
		// has no compare-and-swap value API; unrelated writers can still race.
		current, err := readValue(key, name)
		if err != nil || current != oldValues[i] {
			return ErrUncertain
		}
		if current != newValues[i] && writeValue(key, name, newValues[i]) != nil {
			return ErrUncertain
		}
	}
	flush := windows.NewLazySystemDLL("advapi32.dll").NewProc("RegFlushKey")
	status, _, _ := flush.Call(uintptr(key))
	if status != 0 {
		return ErrUncertain
	}
	// Inform Explorer/new launchers. Never change this process's environment,
	// restart Explorer, terminate Desktop, or claim existing processes updated.
	message, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return ErrUncertain
	}
	send := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var result uintptr
	ok, _, _ := send.Call(0xffff, 0x001a, 0, uintptr(unsafe.Pointer(message)), 0x0002, 2000, uintptr(unsafe.Pointer(&result)))
	if ok == 0 {
		return ErrUncertain
	}
	return nil
}
