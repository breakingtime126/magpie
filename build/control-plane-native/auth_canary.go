package main

import (
	"net/http"
	"strings"
)

// Public, inert test strings, not credentials. Only the standalone self-authored
// profile installs them into its freshly constructed child environment. Nothing
// is read from the runner's credential environment or written to an auth store.
const syntheticAmbientKey = "PUBLIC-SYNTHETIC-AMBIENT-NOT-A-CREDENTIAL"

func syntheticCredential(name string) (key, value string, err error) {
	switch name {
	case "alpha":
		return "MAGPIE_SYNTHETIC_ALPHA_KEY", "PUBLIC-SYNTHETIC-ALPHA-NOT-A-CREDENTIAL", nil
	case "beta":
		return "MAGPIE_SYNTHETIC_BETA_KEY", "PUBLIC-SYNTHETIC-BETA-NOT-A-CREDENTIAL", nil
	default:
		return "", "", failure("fixture_invalid")
	}
}

func syntheticAuthEnvironment() []string {
	return syntheticAuthEnvironmentRenewed(false)
}

func syntheticAuthEnvironmentRenewed(renewed bool) []string {
	env := []string{"OPENAI_API_KEY=" + syntheticAmbientKey}
	for _, name := range []string{"alpha", "beta"} {
		key, value, _ := syntheticCredential(name)
		if renewed {
			value += "-RENEWED"
		}
		env = append(env, key+"="+value)
	}
	return env
}

func nativeSyntheticAuthConfig(name, endpoint, catalog string) ([]byte, error) {
	key, _, err := syntheticCredential(name)
	if err != nil {
		return nil, err
	}
	config, err := nativeConfigForModel(name+"_provider", endpoint, catalog, "test-"+name)
	if err != nil {
		return nil, err
	}
	// nativeConfigForModel ends inside this provider's table. Only its public
	// variable name is added; the test value never appears in config.toml.
	return append(config, []byte("env_key = "+string(jsonBytes(key))+"\n")...), nil
}

func fixtureAuthAllowed(headers http.Header, expected string) bool {
	var authorization []string
	for key, values := range headers {
		switch {
		case strings.EqualFold(key, "Authorization"):
			// Count even empty/duplicate fields. Get would only inspect one value.
			if len(values) == 0 {
				return false
			}
			authorization = append(authorization, values...)
		case strings.EqualFold(key, "Proxy-Authorization"), strings.EqualFold(key, "Cookie"),
			strings.EqualFold(key, "X-Api-Key"), strings.EqualFold(key, "X-Oai-Attestation"),
			strings.EqualFold(key, "ChatGPT-Account-Id"), strings.EqualFold(key, "OpenAI-Organization"),
			strings.EqualFold(key, "OpenAI-Project"):
			return false
		}
	}
	if expected == "" {
		return len(authorization) == 0
	}
	return len(authorization) == 1 && authorization[0] == expected
}

// Called once, only after the owning native generation exited cleanly. The
// HTTP guard rechecks under the count lock, so a request inspected before this
// change cannot commit an old credential afterward. No real token is rotated.
func (f *fixture) rotateSyntheticAuth() error {
	_, value, err := syntheticCredential(f.name)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authRotated || f.requests != 1 || f.failures != 0 || f.expectedAuth != "Bearer "+value {
		return failure("fixture_failed")
	}
	f.expectedAuth = "Bearer " + value + "-RENEWED"
	f.authRotated = true
	return nil
}
