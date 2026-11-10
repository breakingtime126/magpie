// Package nativeprofile owns the exact-byte staged launch-profile envelope.
// Parsing is not authentication. Only a live verified bootstrap owner supplies
// the expected profile digest, and the profile never contains its own digest.
package nativeprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/yetone/magpie/internal/appserverbridge"
)

const Limit = 1 << 20

var ErrProfile = errors.New("native launch profile unavailable or changed")

type locator struct {
	Version    int    `json:"version"`
	Generation string `json:"generation"`
}
type envelope struct {
	Bootstrap locator         `json:"native_bootstrap"`
	Config    json.RawMessage `json:"config"`
}

// Prepared is an immutable host-validated envelope. Its zero value is invalid.
// Copies of its exported bytes cannot modify it. It is not proof of a live
// owner or trusted launch: those facts come from the bootstrap lease.
type Prepared struct{ data []byte }

func New(generation string, cfg appserverbridge.Config) (*Prepared, error) {
	if !validGeneration(generation) || cfg.Validate() != nil {
		return nil, ErrProfile
	}
	config, err := json.Marshal(cfg)
	if err != nil {
		return nil, ErrProfile
	}
	data, err := json.Marshal(envelope{locator{1, generation}, config})
	if err != nil || len(data) > Limit {
		return nil, ErrProfile
	}
	if _, err := parse(data); err != nil {
		return nil, err
	}
	return &Prepared{data: bytes.Clone(data)}, nil
}
func (p *Prepared) Bytes() []byte {
	if p == nil {
		return nil
	}
	return bytes.Clone(p.data)
}
func (p *Prepared) SHA256() string {
	if p == nil {
		return ""
	}
	if _, err := parse(p.data); err != nil {
		return ""
	}
	sum := sha256.Sum256(p.data)
	return hex.EncodeToString(sum[:])
}
func (p *Prepared) Generation() string {
	if p == nil {
		return ""
	}
	e, err := parse(p.data)
	if err != nil {
		return ""
	}
	return e.Bootstrap.Generation
}

// ValidateFor validates the actual stored configuration and requires exact
// canonical equality with Plan.Config. The Controller uses this before writes;
// callers cannot substitute arbitrary unvalidated bytes or a divergent Plan.
func (p *Prepared) ValidateFor(cfg appserverbridge.Config) error {
	if p == nil {
		return ErrProfile
	}
	e, err := parse(p.data)
	if err != nil {
		return err
	}
	if _, err := appserverbridge.LoadConfig(bytes.NewReader(e.Config)); err != nil {
		return ErrProfile
	}
	expected, err := json.Marshal(cfg)
	if err != nil || !bytes.Equal(expected, e.Config) {
		return ErrProfile
	}
	return nil
}

// Read consumes one bounded byte sequence. It does not open paths, parse config,
// discover a bootstrap owner, or invoke any native process.
func Read(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, ErrProfile
	}
	data, err := io.ReadAll(io.LimitReader(input, Limit+1))
	if err != nil || len(data) == 0 || len(data) > Limit {
		return nil, ErrProfile
	}
	return data, nil
}

// Locator parses only in memory, with no filesystem/config validation or owner
// trust. Its result may only discover a pipe; it is never an expected peer path.
// This explicit format has no legacy fallback on errors or unknown versions.
func Locator(data []byte) (string, error) {
	e, err := parse(data)
	if err != nil {
		return "", err
	}
	return e.Bootstrap.Generation, nil
}

// Inspect supports selection-only recovery without probing a pipe or validating
// native filesystem paths. Non-envelope legacy profiles preserve their previous
// inspection behavior. A reserved marker, even null, selects strict parsing.
func Inspect(data []byte) (selected bool, err error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return false, nil
	}
	if _, ok := fields["native_bootstrap"]; !ok {
		return false, nil
	}
	_, err = parse(data)
	return true, err
}

// LoadConfig performs the existing filesystem-aware validation. A launcher must
// authenticate the live owner and verify these identical raw bytes first.
func LoadConfig(data []byte) (appserverbridge.Config, error) {
	e, err := parse(data)
	if err != nil {
		return appserverbridge.Config{}, err
	}
	return appserverbridge.LoadConfig(bytes.NewReader(e.Config))
}
func validGeneration(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16 && strings.ToLower(s) == s
}
func strict(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return ErrProfile
	}
	return nil
}
func parse(data []byte) (envelope, error) {
	var e envelope
	if len(data) == 0 || len(data) > Limit || strict(data, &e) != nil || e.Bootstrap.Version != 1 || !validGeneration(e.Bootstrap.Generation) {
		return envelope{}, ErrProfile
	}
	// Decode schema without Config.Validate. Canonical equality rejects duplicate
	// fields and alternate spellings, including unknown or null envelope objects.
	var cfg appserverbridge.Config
	if len(e.Config) == 0 || e.Config[0] != '{' || strict(e.Config, &cfg) != nil {
		return envelope{}, ErrProfile
	}
	canonicalConfig, err := json.Marshal(cfg)
	if err != nil || !bytes.Equal(canonicalConfig, e.Config) {
		return envelope{}, ErrProfile
	}
	canonical, err := json.Marshal(e)
	if err != nil || !bytes.Equal(canonical, data) {
		return envelope{}, ErrProfile
	}
	return e, nil
}
