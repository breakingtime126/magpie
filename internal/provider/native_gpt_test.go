package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/catalog"
)

func TestNativeGPTBuildBoundary(t *testing.T) {
	for _, id := range []string{"codex", ChatGPTAPIID} {
		if nativeGPTBuiltinBlocked(id) != buildvariant.NativeCodex {
			t.Fatal("incorrect build boundary")
		}
	}
	for _, id := range []string{"", "aihub", "openai", "plugin:codex", "codex-copy", "claude"} {
		if nativeGPTBuiltinBlocked(id) {
			t.Fatal("unrelated provider blocked")
		}
	}
	// Purely synthetic account callbacks; no transport is used.
	calls := 0
	p := Provider{Account: &Account{Agent: "codex", sign: func(context.Context, *http.Request, []byte) error {
		calls++
		return nil
	}}}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	err := p.Sign(context.Background(), req, Protocol("responses"), nil)
	if buildvariant.NativeCodex {
		if !errors.Is(err, errNativeGPTBuiltin) || calls != 0 {
			t.Fatal("blocked account callback reached")
		}
	}
	if buildvariant.NativeCodex {
		p.Account.fetch = func(context.Context) ([]catalog.Model, error) {
			calls++
			return nil, nil
		}
		if _, err := p.fetchList(context.Background()); !errors.Is(err, errNativeGPTBuiltin) || calls != 0 {
			t.Fatal("blocked account list callback reached")
		}
		if _, _, err := renewSaved(context.Background(), "codex", "synthetic", false, ""); !errors.Is(err, errNativeGPTBuiltin) {
			t.Fatal("saved token lookup was not rejected")
		}
	}
	p.Account.Agent = "aihub"
	// Test the predicate separately: ordinary provider settings/proxy behavior
	// remains original and is not replaced with a fake success in this test.
	if nativeGPTBuiltinBlocked(p.Account.Agent) {
		t.Fatal("AIHub was included in builtin boundary")
	}
}

type nativeGPTNoTransport struct{ calls int }

func (n *nativeGPTNoTransport) RoundTrip(*http.Request) (*http.Response, error) {
	n.calls++
	return nil, errors.New("synthetic transport must not be called")
}

func TestNativeGPTSourcesAndSinksRejectCanaries(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("nativecodex build only")
	}
	n := &nativeGPTNoTransport{}
	old, oldSIWC := http.DefaultClient, siwcClient
	http.DefaultClient, siwcClient = &http.Client{Transport: n}, &http.Client{Transport: n}
	t.Cleanup(func() { http.DefaultClient, siwcClient = old, oldSIWC })
	ctx := context.Background()
	home := t.TempDir()
	canary := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"PUBLIC-FILE-CANARY","refresh_token":"PUBLIC-REFRESH-CANARY"}}`)
	path := filepath.Join(home, "auth.json")
	if err := os.WriteFile(path, canary, 0600); err != nil {
		t.Fatal(err)
	}
	if b, _, err := readCodexLogin(home, false); !errors.Is(err, errNativeGPTBuiltin) || b != nil {
		t.Fatal("native auth file imported")
	}
	called := false
	sign := codexSign(func(context.Context) (string, string, error) {
		called = true
		return "PUBLIC-MEMORY-CANARY", "synthetic", nil
	})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	if !errors.Is(sign(ctx, req, nil), errNativeGPTBuiltin) || called || req.Header.Get("Authorization") != "" {
		t.Fatal("memory token read or applied")
	}
	if _, err := codexRefresh(ctx, map[string]any{"tokens": map[string]any{"refresh_token": "PUBLIC-CANARY"}}, "synthetic"); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("refresh not blocked")
	}
	if _, _, _, _, _, err := codexWindows(ctx, "PUBLIC-CANARY", "synthetic"); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("usage not blocked")
	}
	if _, err := consumeCodexReset(ctx, "https://example.invalid", "PUBLIC-CANARY", "synthetic", "credit", "id"); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("reset not blocked")
	}
	if _, err := siwcPostToken(ctx, url.Values{"refresh_token": {"PUBLIC-CANARY"}}); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("SIWC token request not blocked")
	}
	if err := siwcRevoke(ctx, siwcCreds{Refresh: "PUBLIC-CANARY"}); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("SIWC revocation not blocked")
	}
	if _, err := StartSignInWith("codex", SignInOptions{}); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("legacy login started")
	}
	if _, err := ImportLogins(ctx, "codex", []string{string(canary)}); !errors.Is(err, errNativeGPTBuiltin) {
		t.Fatal("legacy import started")
	}
	if n.calls != 0 {
		t.Fatal("blocked builtin reached a transport")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, canary) {
		t.Fatal("native source file changed")
	}
}

func TestNativeGPTSavedRecordsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logins.json")
	old := []byte(`[{"agent":"codex","user":"synthetic","auth":{"token":"PUBLIC-KEPT"},"futureField":7},{"agent":"chatgpt-api","user":"synthetic2","auth":{"token":"PUBLIC-KEPT2"}}]`)
	proposed := []byte(`[{"agent":"claude","user":"other"},{"agent":"codex","user":"synthetic","auth":{"token":"PUBLIC-KEPT"}},{"agent":"chatgpt-api","user":"synthetic2","auth":{"token":"PUBLIC-KEPT2"}}]`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := preserveNativeGPTRecords(path, proposed)
	if err != nil {
		t.Fatal(err)
	}
	if !buildvariant.NativeCodex {
		if !bytes.Equal(out, proposed) {
			t.Fatal("ordinary build serialization changed")
		}
		return
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(out, &rows) != nil || len(rows) != 3 || bytes.Contains(out, []byte("PUBLIC-NOT-IMPORTED")) || !bytes.Contains(out, []byte("PUBLIC-KEPT2")) || !bytes.Contains(out, []byte("futureField")) {
		t.Fatal("unrelated save lost or imported protected records")
	}
	for _, changed := range []string{`[]`, `[{"agent":"codex","user":"injected"}]`, `[{"agent":"codex","user":"synthetic","auth":{"token":"CHANGED"}}]`} {
		if _, err := preserveNativeGPTRecords(path, []byte(changed)); err == nil {
			t.Fatal("protected mutation falsely succeeded")
		}
	}
	if err := os.WriteFile(path, []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := preserveNativeGPTRecords(path, proposed); err == nil {
		t.Fatal("unreadable old state treated as empty")
	}
}

func nativeGPTWriteTestPath(t *testing.T) string {
	t.Helper()
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("MAGPIE_NO_ACCOUNTS", "")
	path := loginsPath()
	if path != filepath.Join(home, ".config", "magpie", "logins.json") {
		t.Fatal("account write fixture is not in its temporary home")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	oldHook := beforeLoginsWrite
	beforeLoginsWrite = func() {}
	t.Cleanup(func() { beforeLoginsWrite = oldHook })
	return path
}

func TestNativeGPTWriteLoginsPreservesDuplicateRawRecords(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("nativecodex build only")
	}
	path := nativeGPTWriteTestPath(t)
	old := []byte(`[{"agent":"codex","user":"same","auth":{"token":"PUBLIC-DUPLICATE"},"unknownField":{"order":1}},{"agent":"chatgpt-api","user":"middle","auth":{"token":"PUBLIC-MIDDLE"},"unknownField":"middle"},{"agent":"codex","user":"same","auth":{"token":"PUBLIC-DUPLICATE"},"unknownField":{"order":2}}]`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	// Do not call parseLogins: its original deduplication is a different
	// behavior, for which this build refuses a protected-record change.
	var proposed []savedLogin
	if json.Unmarshal(old, &proposed) != nil {
		t.Fatal("invalid synthetic records")
	}
	proposed = append(proposed, savedLogin{Agent: "claude", User: "unrelated"})
	if err := writeLogins(proposed); err != nil {
		t.Fatal("unrelated account write failed")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows, previous []json.RawMessage
	if json.Unmarshal(after, &rows) != nil || json.Unmarshal(old, &previous) != nil || len(rows) != 4 {
		t.Fatal("duplicate records were lost")
	}
	for i, want := range previous {
		var compactWant, compactGot bytes.Buffer
		if json.Compact(&compactWant, want) != nil || json.Compact(&compactGot, rows[i+1]) != nil || !bytes.Equal(compactWant.Bytes(), compactGot.Bytes()) {
			t.Fatal("raw protected fields or relative ordering changed")
		}
	}
}

func TestNativeGPTWriteLoginsRefusesLateProtectedChange(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("nativecodex build only")
	}
	for _, tc := range []struct{ name, newer string }{
		{"deleted", `[]`},
		{"changed", `[{"agent":"codex","user":"same","auth":{"token":"PUBLIC-NEWER"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newer := tc.newer
			path := nativeGPTWriteTestPath(t)
			old := []byte(`[{"agent":"codex","user":"same","auth":{"token":"PUBLIC-OLDER"}}]`)
			if err := os.WriteFile(path, old, 0600); err != nil {
				t.Fatal(err)
			}
			var proposed []savedLogin
			if json.Unmarshal(old, &proposed) != nil {
				t.Fatal("invalid synthetic records")
			}
			proposed = append(proposed, savedLogin{Agent: "claude", User: "unrelated"})
			hookCalls := 0
			beforeLoginsWrite = func() {
				hookCalls++
				if err := os.WriteFile(path, []byte(newer), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeLogins(proposed); !errors.Is(err, errNativeGPTBuiltin) || hookCalls != 1 {
				t.Fatal("late protected change did not reject the actual write")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(newer)) {
				t.Fatal("external deletion or update was rolled back")
			}
		})
	}
}
