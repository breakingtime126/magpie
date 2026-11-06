package appserverbridge

import (
	"bytes"
	"strings"
	"testing"
)

func TestStartupProbeProfilesHaveExactVersions(t *testing.T) {
	for _, mode := range []string{"", "compare-unrestricted", "compare-unrestricted-v0.167", "compare-unrestricted-v0.167.0-alpha.3", probe167Mode + " ", strings.ToUpper(probe167Mode)} {
		if StartupProbeSupported(mode) {
			t.Fatal("unknown or implicit startup profile was enabled")
		}
		for _, fixture := range []string{probeSuccessFixture, probe167SuccessFixture} {
			if got, err := validateStartupProbe([]byte(fixture), mode); err != errStartupProbe || got != nil {
				t.Fatal("unknown profile accepted a valid result from a reviewed version")
			}
		}
	}
	for _, profile := range []struct {
		mode    string
		version string
		fixture string
	}{
		{StartupProbeCompareUnrestricted, "0.166.0-alpha.6", probeSuccessFixture},
		{probe167Mode, "0.167.0-alpha.3.2", probe167SuccessFixture},
	} {
		t.Run(profile.mode, func(t *testing.T) {
			if !StartupProbeSupported(profile.mode) {
				t.Fatal("reviewed explicit profile was disabled")
			}
			if _, err := validateStartupProbe([]byte(profile.fixture), profile.mode); err != nil {
				t.Fatal("exact profile version was rejected")
			}
			for _, version := range []string{"0.166.0-alpha.6", "0.167.0-alpha.3.2", "0.167.0-alpha.3", "0.167.0-alpha.3.3", "0.167.0-alpha.4", "0.167.0", "v" + profile.version, profile.version + " ", ""} {
				if version == profile.version {
					continue
				}
				data := strings.Replace(profile.fixture, profile.version, version, 1)
				if got, err := validateStartupProbe([]byte(data), profile.mode); err != errStartupProbe || got != nil {
					t.Fatal("profile accepted a different exact backend version")
				}
			}
		})
	}
}

func TestStartupProbe167CompleteStrictSchema(t *testing.T) {
	for _, input := range []string{
		probe167SuccessFixture + "\n",
		` {"configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null},"appServerVersion":"0.167.0-alpha.3.2"} `,
		strings.Replace(probe167SuccessFixture, "0.167", `0.\u003167`, 1),
		probe167SuccessFixture + strings.Repeat(" ", startupProbeLimit-len(probe167SuccessFixture)),
	} {
		got, err := validateStartupProbe([]byte(input), probe167Mode)
		if err != nil || !bytes.Equal(got, []byte(probe167SuccessFixture)) {
			t.Fatal("complete equivalent new-version result changed")
		}
	}
	bad := []string{
		``, `null`, `[]`, `{}`, `true`,
		`{"error":{"code":-32600,"message":"private-canary"}}`,
		probe167SuccessFixture + `{}`,
		probe167SuccessFixture + "private-canary",
		probe167SuccessFixture + strings.Repeat(" ", startupProbeLimit+1-len(probe167SuccessFixture)),
		strings.Replace(probe167SuccessFixture, `"0.167.0-alpha.3.2"`, `null`, 1),
		strings.Replace(probe167SuccessFixture, `"appServerVersion"`, `"AppServerVersion"`, 1),
		strings.Replace(probe167SuccessFixture, `"appServerVersion":`, `"appServerVersion":"0.167.0-alpha.3.2","appServerVersion":`, 1),
		strings.Replace(probe167SuccessFixture, `"appServerVersion":`, `"\u0061ppServerVersion":"0.167.0-alpha.3.2","appServerVersion":`, 1),
		strings.Replace(probe167SuccessFixture, `"configRequirements":`, `"configRequirements":null,"configRequirements":`, 1),
		strings.Replace(probe167SuccessFixture, `"configRequirements"`, `"ConfigRequirements"`, 1),
		strings.Replace(probe167SuccessFixture, `"requirements":null`, `"requirements":null,"requirements":null`, 1),
		strings.Replace(probe167SuccessFixture, `"requirements":null`, `"requirements":null,"\u0072equirements":null`, 1),
		strings.Replace(probe167SuccessFixture, `"requirements":null,`, ``, 1),
		strings.Replace(probe167SuccessFixture, `"requirements":null`, `"Requirements":null`, 1),
		strings.Replace(probe167SuccessFixture, `"requirements":null`, `"requirements":null,"extra":"private-canary"`, 1),
		strings.Replace(probe167SuccessFixture, `,"supportsIndependentSpeedModes":true`, ``, 1),
		strings.Replace(probe167SuccessFixture, `:true`, `:false`, 1),
		strings.Replace(probe167SuccessFixture, `:true`, `:null`, 1),
		strings.Replace(probe167SuccessFixture, `:true`, `:"true"`, 1),
		strings.Replace(probe167SuccessFixture, `"supportsIndependentSpeedModes":true`, `"supportsIndependentSpeedModes":true,"supportsIndependentSpeedModes":true`, 1),
		strings.Replace(probe167SuccessFixture, "{", `{"id":1,"result":null,`, 1),
		strings.Replace(probe167SuccessFixture, "{", `{"error":null,`, 1),
		strings.Replace(probe167SuccessFixture, "alpha", "\xffalpha", 1),
		strings.Replace(probe167SuccessFixture, "alpha", `\ud800`, 1),
	}
	for _, value := range []string{
		`{}`, `[]`, `true`, `false`, `0`, `"null"`,
		`{"allowedLoginMethods":["chatgpt"]}`,
		`{"network":{"enabled":false}}`,
		`{"permissions":{"network":{"enabled":false}}}`,
		`{"otel":{"log_sse_deltas":true}}`,
		`{"modelProviders":{"private-canary":"provider"}}`,
	} {
		bad = append(bad, strings.Replace(probe167SuccessFixture, `"requirements":null`, `"requirements":`+value, 1))
	}
	for _, value := range []string{`null`, `{}`, `[]`, `true`, `"private-canary"`} {
		bad = append(bad, `{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":`+value+`}`)
	}
	for _, input := range bad {
		if result, err := validateStartupProbe([]byte(input), probe167Mode); err != errStartupProbe || result != nil {
			t.Fatal("unsupported new-profile envelope or non-null restriction was accepted or exposed")
		}
	}
}
