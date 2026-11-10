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

func TestManagerPreparedGenerationRetainsExactProfileAndRecovery(t *testing.T) {
	plan := fixturePlan(t)
	prepared, err := nativeprofile.New(strings.Repeat("3", 32), plan.Config)
	if err != nil {
		t.Fatal(err)
	}
	plan.PreparedProfile = prepared
	generation := &fixtureGeneration{plan: plan}
	store := &fakeSelection{}
	calls := 0
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), store, func(_, lifetime context.Context) (Generation, error) {
		calls++
		generation.lifetime = lifetime
		return generation, nil
	})
	defer m.Close(context.Background())
	request, cancel := context.WithCancel(context.Background())
	status, err := m.Connect(request)
	cancel()
	if err != nil || !status.SelectionConfigured || generation.lifetime.Err() != nil {
		t.Fatal(status, err)
	}
	data, err := os.ReadFile(store.value.Profile.Text)
	if err != nil || !bytes.Equal(data, prepared.Bytes()) {
		t.Fatal("manager changed owner-authenticated profile bytes", err)
	}
	if _, err := m.Connect(context.Background()); err != nil || calls != 1 {
		t.Fatal("unchanged generation was not reused", err)
	}
	if err := os.WriteFile(store.value.Profile.Text, []byte("damaged synthetic profile"), 0600); err != nil {
		t.Fatal(err)
	}
	if status, err := m.Connect(context.Background()); err == nil || status.SelectionConfigured || !status.RecoveryRequired || calls != 1 {
		t.Fatal("damaged prepared profile was reused", status, err)
	}
	if _, err := m.Disconnect(context.Background()); err != nil || generation.closed != 0 {
		t.Fatal("recovery lost owner or required intact profile", err)
	}
	if err := m.Close(context.Background()); err != nil || generation.closed != 1 {
		t.Fatal("shutdown did not drain retained owner", err)
	}
}
