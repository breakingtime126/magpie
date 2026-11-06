package appserverbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/proc"
)

// StartupProbeCompareUnrestricted opts into the reviewed 0.166.0-alpha.6
// startup inspection, which can refresh authentication and update policy caches.
const StartupProbeCompareUnrestricted = "compare-unrestricted-v0.166"

// StartupProbeCompareUnrestricted167 opts into only the reviewed
// 0.167.0-alpha.3.2 startup inspection. It does not enable any JSONL auth or
// requirements compatibility mode, which remain scoped to 0.166.0-alpha.6.
const StartupProbeCompareUnrestricted167 = "compare-unrestricted-v0.167.0-alpha.3.2"

// StartupProbeSupported reports whether mode selects an explicitly reviewed
// exact-version profile. An empty, partial or unknown mode is never enabled.
func StartupProbeSupported(mode string) bool {
	return startupProbeVersionForMode(mode) != ""
}

func startupProbeVersionForMode(mode string) string {
	switch mode {
	case StartupProbeCompareUnrestricted:
		return "0.166.0-alpha.6"
	case StartupProbeCompareUnrestricted167:
		return "0.167.0-alpha.3.2"
	default:
		return ""
	}
}

const startupProbeLimit = 64 << 10
const startupClientInfoLimit = 4 << 10
const startupProbeTimeout = 30 * time.Second

var errStartupProbe = errors.New("startup probe failed or returned unsupported configuration")
var errStartupClientInfo = errors.New("invalid startup client info")

// StartupClientInfo is the reviewed identity passed to the official one-shot
// command. A nil title preserves the official absent/null optional value.
type StartupClientInfo struct {
	Name    string  `json:"name"`
	Title   *string `json:"title,omitempty"`
	Version string  `json:"version"`
}

// ParseStartupClientInfo accepts one bounded object, with unique, known keys.
// Errors never include supplied fields or values.
func ParseStartupClientInfo(value string) (StartupClientInfo, error) {
	var info StartupClientInfo
	if len(value) > startupClientInfoLimit {
		return info, errStartupClientInfo
	}
	fields, err := startupObject([]byte(value))
	if err != nil || len(fields) < 2 || len(fields) > 3 {
		return info, errStartupClientInfo
	}
	for key := range fields {
		if key != "name" && key != "title" && key != "version" {
			return info, errStartupClientInfo
		}
	}
	if json.Unmarshal(fields["name"], &info.Name) != nil || json.Unmarshal(fields["version"], &info.Version) != nil {
		return StartupClientInfo{}, errStartupClientInfo
	}
	if title, ok := fields["title"]; ok && json.Unmarshal(title, &info.Title) != nil {
		return StartupClientInfo{}, errStartupClientInfo
	}
	if _, err := startupClientInfoJSON(info); err != nil {
		return StartupClientInfo{}, errStartupClientInfo
	}
	return info, nil
}

func startupClientInfoJSON(info StartupClientInfo) ([]byte, error) {
	if info.Name == "" || info.Version == "" || !startupText(info.Name) || !startupText(info.Version) ||
		(info.Title != nil && !startupText(*info.Title)) {
		return nil, errStartupClientInfo
	}
	data, err := json.Marshal(info)
	if err != nil || len(data) > startupClientInfoLimit {
		return nil, errStartupClientInfo
	}
	return data, nil
}

func startupText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		// encoding/json replaces malformed surrogate escapes with RuneError.
		// Reject that ambiguous representation as well as control characters.
		if unicode.IsControl(r) || r == utf8.RuneError {
			return false
		}
	}
	return true
}

// startupObject does not use the JSONL parser: one-shot inspection has a
// smaller bound and must reject duplicate keys before any value is trusted.
func startupObject(data []byte) (map[string]json.RawMessage, error) {
	if len(data) > startupProbeLimit || !utf8.Valid(data) {
		return nil, errStartupProbe
	}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errStartupProbe
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || !startupText(name) {
			return nil, errStartupProbe
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errStartupProbe
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errStartupProbe
		}
		fields[name] = value
	}
	if last, err := d.Token(); err != nil || last != json.Delim('}') {
		return nil, errStartupProbe
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errStartupProbe
	}
	return fields, nil
}

type startupProbeRunner func(context.Context, Backend, string, []string) ([]byte, error)

// ProbeStartup runs at most two official one-shot commands and returns only a
// complete, equal, reviewed unrestricted result, without a trailing newline.
// Commands are sequential and never retried. This is not transactional: even
// a rejected result can follow official authentication refresh or cache writes.
// It does not initialize JSONL, sign out, or change either child's requirements.
func ProbeStartup(parent context.Context, cfg Config, options LaunchOptions, info StartupClientInfo) ([]byte, error) {
	return probeStartup(parent, cfg, options, info, startupProbeTimeout, runStartupProbe)
}

func probeStartup(parent context.Context, cfg Config, options LaunchOptions, info StartupClientInfo, timeout time.Duration, run startupProbeRunner) ([]byte, error) {
	if !StartupProbeSupported(cfg.StartupProbe) {
		return nil, errors.New("startup probe is disabled")
	}
	if cfg.Validate() != nil {
		return nil, errors.New("invalid startup probe configuration")
	}
	clientInfo, err := startupClientInfoJSON(info)
	if err != nil {
		return nil, errStartupClientInfo
	}
	workingDirectory := cfg.WorkingDirectory
	if workingDirectory != "" {
		workingDirectory, err = filepath.EvalSymlinks(workingDirectory)
		if err != nil {
			return nil, errors.New("cannot resolve startup probe working directory")
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// Verify both explicitly reviewed binaries before either authority can
	// refresh credentials or write caches. A digest is not a signature or a
	// defense against executable replacement by another same-user process.
	for _, b := range cfg.Backends {
		if ctx.Err() != nil || !startupProbeDigestMatches(ctx, b) {
			return nil, errStartupProbe
		}
	}
	var accepted []byte
	for _, b := range cfg.Backends {
		if ctx.Err() != nil {
			return nil, errStartupProbe
		}
		directory := workingDirectory
		if directory == "" {
			directory = b.Home
		}
		args := append(options.backendArgs(b.Provider), "--print-startup-config", "--startup-client-info", string(clientInfo))
		data, err := run(ctx, b, directory, args)
		if err != nil || ctx.Err() != nil {
			return nil, errStartupProbe
		}
		result, err := validateStartupProbe(data, cfg.StartupProbe)
		if err != nil || (accepted != nil && !bytes.Equal(accepted, result)) {
			return nil, errStartupProbe
		}
		accepted = result
	}
	if ctx.Err() != nil {
		return nil, errStartupProbe
	}
	return accepted, nil
}

func startupProbeDigestMatches(ctx context.Context, b Backend) bool {
	want, err := hex.DecodeString(b.StartupProbeSHA256)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	f, err := os.Open(b.Command)
	if err != nil {
		return false
	}
	defer f.Close()
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	for {
		if ctx.Err() != nil {
			return false
		}
		n, err := f.Read(buffer)
		if n != 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
	}
	return bytes.Equal(hash.Sum(nil), want)
}

// Both reviewed builds have these complete public success fields. Version is
// exact for the selected profile, not inferred from an untrusted response.
// Even an empty requirements object is unsupported. Never redact it into null
// or return only one backend's policy as their shared state.
func validateStartupProbe(data []byte, mode string) ([]byte, error) {
	wantVersion := startupProbeVersionForMode(mode)
	if wantVersion == "" {
		return nil, errStartupProbe
	}
	root, err := startupObject(data)
	if err != nil || len(root) != 2 {
		return nil, errStartupProbe
	}
	var version string
	if json.Unmarshal(root["appServerVersion"], &version) != nil || version != wantVersion {
		return nil, errStartupProbe
	}
	requirements, err := startupObject(root["configRequirements"])
	if err != nil || len(requirements) != 2 ||
		!bytes.Equal(bytes.TrimSpace(requirements["requirements"]), []byte("null")) ||
		!bytes.Equal(bytes.TrimSpace(requirements["supportsIndependentSpeedModes"]), []byte("true")) {
		return nil, errStartupProbe
	}
	// Marshal the complete validated objects, sorting keys but neither
	// removing metadata nor synthesizing any requirement or success field.
	root["appServerVersion"], err = json.Marshal(version)
	if err != nil {
		return nil, errStartupProbe
	}
	root["configRequirements"], err = json.Marshal(requirements)
	if err != nil {
		return nil, errStartupProbe
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, errStartupProbe
	}
	return canonical, nil
}

// Use our own OS pipe rather than StdoutPipe: concurrent Wait must not close
// unread stdout. Both stdio endpoints are native files, so exec owns no copy
// goroutines and a descendant cannot keep an internal stderr copier alive.
func runStartupProbe(ctx context.Context, b Backend, directory string, args []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, errStartupProbe
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, errStartupProbe
	}
	defer reader.Close()
	command := proc.CommandContext(context.WithoutCancel(ctx), b.Command, args...)
	command.Env = backendEnvironment(b)
	command.Dir = directory
	command.Stdin = nil
	command.Stderr = nil
	command.Stdout = writer
	tree, err := proc.StartTreeStrict(command)
	writer.Close()
	if err != nil {
		return nil, errStartupProbe
	}
	type readResult struct {
		data []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	waitDone := make(chan error, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(reader, startupProbeLimit+1))
		readDone <- readResult{data: data, err: err}
	}()
	// Tree.Wait is not idempotent. This is its only call on every path.
	go func() { waitDone <- tree.Wait() }()
	var result readResult
	var waitErr error
	readFinished, waitFinished := false, false
	defer func() {
		reader.Close()
		stopped := tree.Stop(0)
		if !waitFinished {
			<-waitDone
		}
		<-stopped
		if !readFinished {
			<-readDone
		}
	}()
	for !readFinished || !waitFinished {
		select {
		case <-ctx.Done():
			return nil, errStartupProbe
		case result = <-readDone:
			readFinished = true
			if result.err != nil || len(result.data) > startupProbeLimit {
				return nil, errStartupProbe
			}
		case waitErr = <-waitDone:
			waitFinished = true
			if waitErr != nil {
				return nil, errStartupProbe
			}
		}
	}
	if ctx.Err() != nil {
		return nil, errStartupProbe
	}
	return result.data, nil
}
