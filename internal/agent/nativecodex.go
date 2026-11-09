package agent

import (
	"errors"

	"github.com/yetone/magpie/internal/buildvariant"
)

// ErrNativeCodexLegacyWrite reports a legacy gateway action that this build
// must not perform on Codex. It does not mean native integration succeeded.
var ErrNativeCodexLegacyWrite = errors.New("legacy Codex gateway wiring is unavailable in the native Codex build; use the explicit native Codex controls")

// LegacyCodexWritesBlocked covers local Codex, WSL twins and profile copies.
// ListsFor is the same family identity used by the existing model catalog.
func (a *Agent) LegacyCodexWritesBlocked() bool {
	return buildvariant.NativeCodex && a != nil && a.ListsFor() == "codex"
}

// protectNativeCodex also guards direct hooks, not only the public Apply and
// Connect methods. Call it after the local/WSL wrappers have been installed.
func protectNativeCodex(a *Agent) *Agent {
	if !a.LegacyCodexWritesBlocked() {
		return a
	}
	deny := func() error { return ErrNativeCodexLegacyWrite }
	for i := range a.Fields {
		if a.Fields[i].Set != nil {
			a.Fields[i].Set = func(string) error { return ErrNativeCodexLegacyWrite }
		}
	}
	if a.Sync != nil {
		a.Sync = deny
	}
	if a.Unwire != nil {
		a.Unwire = deny
	}
	if a.Restore != nil {
		a.Restore = deny
	}
	if a.EndFailover != nil {
		a.EndFailover = deny
	}
	if a.Follow != nil {
		a.Follow = deny
	}
	if a.Join != nil {
		a.Join = func() (bool, error) { return false, ErrNativeCodexLegacyWrite }
	}
	if a.RenameRefs != nil {
		a.RenameRefs = func(string, string) (bool, error) { return false, ErrNativeCodexLegacyWrite }
	}
	if a.move != nil {
		a.move = func(string, string) error { return ErrNativeCodexLegacyWrite }
	}
	return a
}
