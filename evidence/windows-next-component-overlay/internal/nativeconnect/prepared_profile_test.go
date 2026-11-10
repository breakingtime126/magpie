package nativeconnect

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativeprofile"
)

func TestNativeConnectPreparedProfileExactBytesAndSelectionOnlyInspect(t *testing.T) {
	plan := fixturePlan(t)
	prepared, err := nativeprofile.New(strings.Repeat("1", 32), plan.Config)
	if err != nil {
		t.Fatal(err)
	}
	plan.PreparedProfile = prepared
	store := &fakeSelection{}
	c := New(filepath.Join(t.TempDir(), "connection"), store, func(context.Context) (Plan, error) { return plan, nil })
	if _, err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(store.value.Profile.Text)
	if err != nil || !bytes.Equal(actual, prepared.Bytes()) {
		t.Fatal("Controller reencoded prepared bytes", err)
	}
	j, err := c.readJournal()
	if err != nil || j.ProfileSHA256 != prepared.SHA256() {
		t.Fatal("journal/owner profile digest diverged", err)
	}
	if err := os.Remove(plan.Config.Backends[0].Command); err != nil {
		t.Fatal(err)
	}
	restarted := New(c.root, store, nil)
	status, err := restarted.Inspect()
	if err != nil || !status.SelectionConfigured || status.BackendHandshakeVerified || status.ChatVerified {
		t.Fatal("Inspect performed config/probe work or overclaimed", status, err)
	}
	if _, err := restarted.Disconnect(); err != nil {
		t.Fatal("recovery depended on runtime", err)
	}
}
func TestNativeConnectRejectsPreparedDivergenceBeforeWrites(t *testing.T) {
	for _, mode := range []string{"divergent", "zero", "invalid-runtime"} {
		t.Run(mode, func(t *testing.T) {
			plan := fixturePlan(t)
			p, err := nativeprofile.New(strings.Repeat("2", 32), plan.Config)
			if err != nil {
				t.Fatal(err)
			}
			plan.PreparedProfile = p
			if mode == "divergent" {
				plan.Config.Backends[0].StartupProbeSHA256 = strings.Repeat("d", 64)
			}
			if mode == "zero" {
				plan.PreparedProfile = &nativeprofile.Prepared{}
			}
			if mode == "invalid-runtime" {
				if err := os.Remove(plan.Config.Backends[0].Command); err != nil {
					t.Fatal(err)
				}
			}
			root := filepath.Join(t.TempDir(), "not-created")
			store := &fakeSelection{}
			c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
			if _, err := c.Connect(context.Background()); err == nil || store.writes != 0 {
				t.Fatal("invalid prepared profile selected")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("invalid prepared profile wrote state")
			}
		})
	}
}
