package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixtureConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(dir, "codex-fixture")
	if runtime.GOOS == "windows" {
		command += ".exe"
	}
	if err := os.WriteFile(command, data, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := Config{}
	for _, id := range []string{"first", "second"} {
		home, codex := filepath.Join(dir, id+"-home"), filepath.Join(dir, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, Backend{ID: id, Provider: "openai", Command: command, Home: home, CodexHome: codex})
	}
	return cfg
}

func TestLoadConfigStrictAndReadOnly(t *testing.T) {
	cfg := fixtureConfig(t)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"unknown root":    append([]byte(`{"typo":1,`), data[1:]...),
		"unknown backend": bytes.Replace(data, []byte(`"provider"`), []byte(`"provdier"`), 1),
		"second object":   append(append([]byte(nil), data...), []byte(` {}`)...),
		"null":            []byte(`null`),
		"oversized":       bytes.Repeat([]byte(" "), (1<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	for _, b := range cfg.Backends {
		for _, path := range []string{b.Home, b.CodexHome} {
			entries, err := os.ReadDir(path)
			if err != nil || len(entries) != 0 {
				t.Fatalf("configuration touched private home: %v %v", entries, err)
			}
		}
	}
}

func TestConfigRejectsUnsafePathsAndNames(t *testing.T) {
	base := fixtureConfig(t)
	cases := map[string]func(*Config){
		"one backend":          func(c *Config) { c.Backends = c.Backends[:1] },
		"duplicate id":         func(c *Config) { c.Backends[1].ID = c.Backends[0].ID },
		"id delimiter":         func(c *Config) { c.Backends[0].ID = "first:second" },
		"provider injection":   func(c *Config) { c.Backends[0].Provider = "openai\"\nkey=1" },
		"relative command":     func(c *Config) { c.Backends[0].Command = "codex" },
		"recursive executable": func(c *Config) { c.Backends[0].Command, _ = os.Executable() },
		"relative home":        func(c *Config) { c.Backends[0].Home = "home" },
		"missing home":         func(c *Config) { c.Backends[0].Home = filepath.Join(c.Backends[0].Home, "missing") },
		"same backend homes":   func(c *Config) { c.Backends[0].CodexHome = c.Backends[0].Home },
		"shared homes":         func(c *Config) { c.Backends[1].Home = c.Backends[0].Home },
		"overlapping homes":    func(c *Config) { c.Backends[0].Home = filepath.Dir(c.Backends[1].Home) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Config{Backends: append([]Backend(nil), base.Backends...)}
			change(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("accepted unsafe configuration")
			}
		})
	}
}

func TestConfigRejectsHomeAndExecutableAliases(t *testing.T) {
	cfg := fixtureConfig(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(cfg.Backends[0].Home, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cfg.Backends[1].Home = alias
	if err := cfg.Validate(); err == nil {
		t.Fatal("accepted symlink-shared home")
	}
	cfg = fixtureConfig(t)
	self, _ := os.Executable()
	alias = filepath.Join(t.TempDir(), "recursive")
	if err := os.Symlink(self, alias); err != nil {
		t.Fatal(err)
	}
	cfg.Backends[0].Command = alias
	if err := cfg.Validate(); err == nil {
		t.Fatal("accepted recursive executable symlink")
	}
}

func TestConfigRejectsScriptsAndEnvironmentOverrides(t *testing.T) {
	cfg := fixtureConfig(t)
	script := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Backends[0].Command = script
	if err := cfg.Validate(); err == nil {
		t.Fatal("accepted shell wrapper")
	}
	cfg = fixtureConfig(t)
	for _, key := range []string{"HOME", "codex_home", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "HTTPS_PROXY", "NODE_OPTIONS", "XDG_CONFIG_HOME", "PATH", "BAD=VALUE", "CODEX_API_KEY", "CODEX_CONFIG_OVERRIDE", "codex_future_routing", "OPENAI_BASE_URL", "OPENAI_API_BASE", "openai_base_url"} {
		cfg.Backends[0].InheritEnv = []string{key}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted reserved environment %s", key)
		}
	}
	cfg.Backends[0].InheritEnv = []string{"EXPLICIT_PROVIDER_KEY"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBackendEnvironmentDoesNotInheritCredentialsOrProxy(t *testing.T) {
	cfg := fixtureConfig(t)
	t.Setenv("OPENAI_API_KEY", "secret-not-for-child")
	t.Setenv("HTTPS_PROXY", "https://private-proxy.invalid")
	t.Setenv("LD_PRELOAD", "private-loader")
	t.Setenv("EXPLICIT_PROVIDER_KEY", "explicit-test-value")
	b := cfg.Backends[0]
	b.InheritEnv = []string{"EXPLICIT_PROVIDER_KEY"}
	env := strings.Join(backendEnvironment(b), "\n")
	for _, unexpected := range []string{"secret-not-for-child", "private-proxy", "private-loader"} {
		if strings.Contains(env, unexpected) {
			t.Fatalf("inherited %s", unexpected)
		}
	}
	for _, expected := range []string{"HOME=" + b.Home, "CODEX_HOME=" + b.CodexHome, "EXPLICIT_PROVIDER_KEY=explicit-test-value"} {
		if !strings.Contains(env, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
}

func TestConfigValidatesEveryModelBeforeStartingBackends(t *testing.T) {
	cfg := fixtureConfig(t)
	valid := json.RawMessage(`{"id":"custom","model":"custom","displayName":"Custom","description":"","defaultReasoningEffort":"medium","hidden":false,"supportsPersonality":false,"isDefault":true,"inputModalities":["text"],"supportedReasoningEfforts":[{"reasoningEffort":"medium","description":"Medium"}]}`)
	cfg.Backends[0].Models = []json.RawMessage{valid}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid descriptor rejected: %v", err)
	}
	for _, bad := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{}`), json.RawMessage(`{"id":"custom"}`), bytes.Replace(valid, []byte(`"hidden":false`), []byte(`"hidden":"false"`), 1)} {
		cfg.Backends[1].Models = []json.RawMessage{valid, bad}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted invalid descriptor %s", bad)
		}
		input := io.NopCloser(strings.NewReader(""))
		reader, output := io.Pipe()
		if err := Run(context.Background(), cfg, input, output); err == nil {
			t.Fatal("Run accepted invalid model configuration")
		}
		input.Close()
		reader.Close()
		output.Close()
		for _, b := range cfg.Backends {
			if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
				t.Fatalf("backend %s started before model validation: %v", b.ID, err)
			}
		}
	}
}

func TestSharedWorkingDirectoryValidation(t *testing.T) {
	cfg := fixtureConfig(t)
	shared := filepath.Join(filepath.Dir(cfg.Backends[0].Home), "shared-start")
	if err := os.Mkdir(shared, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.WorkingDirectory = shared
	encoded, _ := json.Marshal(cfg)
	loaded, err := LoadConfig(bytes.NewReader(encoded))
	if err != nil || loaded.WorkingDirectory != shared {
		t.Fatalf("explicit working directory rejected: %v", err)
	}
	entries, err := os.ReadDir(shared)
	if err != nil || len(entries) != 0 {
		t.Fatal("validation changed the selected directory")
	}
	invalid := []string{"relative", filepath.Join(shared, "missing"), cfg.Backends[0].Command}
	for _, b := range cfg.Backends {
		for _, home := range []string{b.Home, b.CodexHome} {
			nested := filepath.Join(home, "nested-start")
			if err := os.Mkdir(nested, 0700); err != nil {
				t.Fatal(err)
			}
			invalid = append(invalid, home, nested, filepath.Dir(home))
		}
	}
	for _, directory := range invalid {
		cfg.WorkingDirectory = directory
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted unsafe working directory %q", directory)
		}
	}
	cfg.WorkingDirectory = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal("legacy per-home startup rejected", err)
	}
}

func TestSharedWorkingDirectoryRejectsStateAliasAndMissingPathBeforeLaunch(t *testing.T) {
	cfg := fixtureConfig(t)
	alias := filepath.Join(filepath.Dir(cfg.Backends[0].Home), "shared-alias")
	t.Run("alias", func(t *testing.T) {
		if err := os.Symlink(cfg.Backends[1].CodexHome, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		cfg.WorkingDirectory = alias
		if err := cfg.Validate(); err == nil {
			t.Fatal("working directory alias reached private state")
		}
	})
	cfg.WorkingDirectory = filepath.Join(filepath.Dir(alias), "not-created")
	input := io.NopCloser(strings.NewReader(""))
	reader, output := io.Pipe()
	defer reader.Close()
	defer output.Close()
	if err := Run(context.Background(), cfg, input, output); err == nil {
		t.Fatal("missing shared working directory accepted")
	}
	for _, b := range cfg.Backends {
		if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
			t.Fatal("started a child after invalid working directory")
		}
	}
	if _, err := os.Stat(cfg.WorkingDirectory); !os.IsNotExist(err) {
		t.Fatal("created the missing directory")
	}
}
