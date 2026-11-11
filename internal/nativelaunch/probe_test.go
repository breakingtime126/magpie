package nativelaunch

import (
	"bytes"
	"context"
	"errors"
	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeprofile"
	"strings"
	"testing"
)

type fakeProbeLease struct {
	fakeLease
	finish    int
	finishErr error
}

func (l *fakeProbeLease) FinishProbe(context.Context) error { l.finish++; return l.finishErr }

type fakeProbeEndpoint struct {
	calls, stops  int
	err, closeErr error
	onProbe       func()
	reference     nativechannel.Reference
}

func (e *fakeProbeEndpoint) shutdown(context.Context) error { e.stops++; return e.closeErr }
func (e *fakeProbeEndpoint) probe(_ context.Context, _ appserverbridge.Config, _ appserverbridge.LaunchOptions, _ appserverbridge.StartupClientInfo, r nativechannel.Reference) ([]byte, error) {
	e.calls++
	e.reference = r
	if e.onProbe != nil {
		e.onProbe()
	}
	return []byte("genuine synthetic result"), e.err
}
func TestAuthenticatedProbeCompletionAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "tampered", "dead", "probe-failure", "finish-failure", "cleanup-failure", "owner-cleanup", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _, record := launchFixture(t)
			cfg.StartupProbe = appserverbridge.StartupProbeCompareUnrestricted167
			for i := range cfg.Backends {
				cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
			}
			if mode == "disabled" {
				cfg.StartupProbe = ""
			}
			p, err := nativeprofile.New(record.Generation, cfg)
			if err != nil {
				t.Fatal(err)
			}
			record.ProfileSHA256 = p.SHA256()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := &fakeProbeLease{fakeLease: fakeLease{ctx: ctx, record: record}}
			e := &fakeProbeEndpoint{}
			data := p.Bytes()
			switch mode {
			case "tampered":
				data = bytes.Replace(data, []byte(`"provider":"openai"`), []byte(`"provider":"changed"`), 1)
			case "dead":
				cancel()
			case "probe-failure":
				e.err = errors.New("probe fail")
			case "finish-failure":
				l.finishErr = errors.New("revision changed")
			case "cleanup-failure":
				e.closeErr = errors.New("not drained")
			case "owner-cleanup":
				l.closeErr = errors.New("owner not drained")
			}
			deps := probeDependencies{connect: func(context.Context, string) (probeLease, error) { return l, nil }, allocate: func() (probeEndpoint, error) { return e, nil }}
			out, err := probeStartup(context.Background(), bytes.NewReader(data), appserverbridge.LaunchOptions{}, appserverbridge.StartupClientInfo{Name: "synthetic", Version: "1"}, deps)
			if mode == "success" {
				if err != nil || string(out) != "genuine synthetic result" || l.finish != 1 || e.reference != record.Reference {
					t.Fatal("valid operation lost", err)
				}
			} else if err == nil || len(out) != 0 {
				t.Fatal("failure exposed success", mode, err)
			}
			if (mode == "tampered" || mode == "dead" || mode == "disabled") && e.calls != 0 {
				t.Fatal("unauthorized probe ran")
			}
			if mode == "probe-failure" && l.finish != 0 {
				t.Fatal("failed native probe requested acceptance")
			}
			if l.closed != 1 {
				t.Fatal("owner cleanup lost")
			}
		})
	}
}
