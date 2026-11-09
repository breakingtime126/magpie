package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSyntheticAuthUsesOnlyFixedDistinctCanaries(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range []string{"alpha", "beta"} {
		key, value, err := syntheticCredential(name)
		if err != nil || key == "" || value == "" || seen[key] || seen[value] || value == syntheticAmbientKey {
			t.Fatal("synthetic credential identity invalid")
		}
		seen[key], seen[value] = true, true
		config, err := nativeSyntheticAuthConfig(name, "http://127.0.0.1:1234", "synthetic-catalog")
		if err != nil || !bytes.HasSuffix(config, []byte("env_key = \""+key+"\"\n")) ||
			!bytes.Contains(config, []byte("requires_openai_auth = false")) || bytes.Contains(config, []byte(value)) {
			t.Fatal("synthetic native config mixed credential name and value")
		}
		other := "alpha"
		if name == other {
			other = "beta"
		}
		otherKey, otherValue, _ := syntheticCredential(other)
		if bytes.Contains(config, []byte(otherKey)) || bytes.Contains(config, []byte(otherValue)) {
			t.Fatal("synthetic native config crossed provider")
		}
	}
	if _, _, err := syntheticCredential("other"); err == nil {
		t.Fatal("unknown credential owner accepted")
	}
	if _, err := nativeSyntheticAuthConfig("alpha", "https://example.com", "synthetic"); err == nil {
		t.Fatal("non-loopback credential recipient accepted")
	}
	// Ambient values are never consulted: only the public constants are emitted.
	t.Setenv("MAGPIE_SYNTHETIC_ALPHA_KEY", "SYNTHETIC-UNEXPECTED-AMBIENT")
	t.Setenv("OPENAI_API_KEY", "SYNTHETIC-UNEXPECTED-AMBIENT")
	env := syntheticAuthEnvironment()
	if len(env) != 3 || strings.Contains(strings.Join(env, "\n"), "SYNTHETIC-UNEXPECTED-AMBIENT") {
		t.Fatal("ambient credential data was copied")
	}
}

func TestSyntheticAuthHeaderGuardRejectsAmbiguityAndIdentity(t *testing.T) {
	_, value, _ := syntheticCredential("alpha")
	expected := "Bearer " + value
	if !fixtureAuthAllowed(http.Header{"Authorization": {expected}}, expected) ||
		!fixtureAuthAllowed(http.Header{}, "") {
		t.Fatal("valid header case rejected")
	}
	for _, headers := range []http.Header{
		{}, {"Authorization": {""}}, {"Authorization": nil},
		{"Authorization": {expected, expected}},
		{"Authorization": {expected}, "authorization": {expected}},
		{"Authorization": {expected + ", " + expected}},
		{"Authorization": {"Bearer " + syntheticAmbientKey}},
	} {
		if fixtureAuthAllowed(headers, expected) {
			t.Fatal("missing, wrong or ambiguous authorization accepted")
		}
	}
	for _, key := range []string{"Proxy-Authorization", "Cookie", "X-Api-Key", "X-Oai-Attestation",
		"ChatGPT-Account-Id", "OpenAI-Organization", "OpenAI-Project"} {
		for _, value := range []string{"", "PUBLIC-SYNTHETIC-REJECT"} {
			if fixtureAuthAllowed(http.Header{"Authorization": {expected}, strings.ToLower(key): {value}}, expected) {
				t.Fatal("identity/proof header accepted")
			}
		}
	}
	if fixtureAuthAllowed(http.Header{"Authorization": {expected}}, "") {
		t.Fatal("legacy unauthenticated fixture accepted authorization")
	}
}

func TestSyntheticAuthHTTPRequestsStayWithTheirOwner(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for _, name := range []string{"alpha", "beta"} {
		t.Run(name, func(t *testing.T) {
			_, value, _ := syntheticCredential(name)
			expected := "Bearer " + value
			f := newFixtureWithAuth(name, "test-"+name, "", expected)
			defer f.close()
			for round := 1; round <= 2; round++ {
				body := jsonBytes(map[string]any{"model": "test-" + name, "stream": true,
					"input": fmt.Sprintf("synthetic-%s-turn-%d", name, round)})
				req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", expected)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil || resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte("fixture-answer-"+name)) {
					t.Fatal("own synthetic credential request failed")
				}
			}
			if n, failures := f.counts(); n != 2 || failures != 0 {
				t.Fatal("authenticated fixture accounting failed")
			}
		})
	}
	_, alpha, _ := syntheticCredential("alpha")
	_, beta, _ := syntheticCredential("beta")
	for _, auth := range [][]string{nil, {"Bearer " + beta}, {"Bearer " + syntheticAmbientKey},
		{"Bearer " + alpha, "Bearer " + alpha}} {
		f := newFixtureWithAuth("alpha", "test-alpha", "", "Bearer "+alpha)
		req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses",
			strings.NewReader(`{"model":"test-alpha","stream":true,"input":"synthetic-alpha-turn-1"}`))
		if err != nil {
			f.close()
			t.Fatal(err)
		}
		for _, value := range auth {
			req.Header.Add("Authorization", value)
		}
		resp, err := client.Do(req)
		if err != nil {
			f.close()
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		n, failures := f.counts()
		f.close()
		if resp.StatusCode != http.StatusBadRequest || n != 0 || failures != 1 {
			t.Fatal("wrong or missing credential request accepted")
		}
	}
}
