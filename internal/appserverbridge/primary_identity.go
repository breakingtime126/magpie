package appserverbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

const primaryIdentityTimeout = 15 * time.Second

var errPrimaryIdentity = errors.New("primary identity inspection unavailable")

// InspectPrimaryIdentity167 is a separate, explicit operation. It starts only
// the pinned primary, never Router, the secondary, a catalog or a turn. The
// operator must trust this executable and its native configuration. A digest
// is not provenance, destination binding or an OS credential boundary.
// Native initialization/status can refresh credentials, contact services and
// update native state even though refreshToken and includeToken are false.
func InspectPrimaryIdentity167(parent context.Context, cfg Config) ([]byte, error) {
	return inspectPrimaryIdentity167(parent, cfg, primaryIdentityTimeout)
}

func inspectPrimaryIdentity167(parent context.Context, cfg Config, timeout time.Duration) (result []byte, resultErr error) {
	if cfg.Validate() != nil || cfg.Backends[0].Provider != "openai" ||
		cfg.ControlPlane != "" || cfg.ControlHome != "" || cfg.BridgeModelCatalogJSON != "" || cfg.BridgeModelSettings != "" ||
		cfg.AuthStatus != "" || cfg.Attestation != "" || cfg.GatewayOAuth != "" || cfg.RequirementsRead != "" ||
		cfg.ExperimentalFeatureList != "" || cfg.StartupProbe != "" || cfg.DesktopPluginOverride != "" || cfg.WorkingDirectory != "" ||
		len(cfg.Backends[0].InheritEnv) != 0 || cfg.Backends[0].Models != nil {
		return nil, errPrimaryIdentity
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	b := cfg.Backends[0]
	if !startupProbeDigestMatches(ctx, b) {
		return nil, primaryIdentityError(ctx)
	}
	// Do not let CommandContext kill the parent before its owned tree is cleaned.
	processCtx, stopProcess := context.WithCancel(context.WithoutCancel(parent))
	defer stopProcess()
	command := proc.CommandContext(processCtx, b.Command, (LaunchOptions{}).backendArgs(b.Provider)...)
	command.Env, command.Dir, command.Stderr = backendEnvironment(b), b.Home, nil
	input, err := command.StdinPipe()
	if err != nil {
		return nil, errPrimaryIdentity
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, errPrimaryIdentity
	}
	if ctx.Err() != nil {
		input.Close()
		output.Close()
		return nil, ctx.Err()
	}
	tree, err := proc.StartTreeStrict(command)
	if err != nil {
		input.Close()
		output.Close()
		return nil, primaryIdentityError(ctx)
	}
	events := make(chan bridgeEvent, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		primaryIdentityScan(ctx, output, events)
	}()
	defer func() {
		cancel()
		input.Close()
		waited := make(chan error, 1)
		go func() { waited <- tree.Wait() }()
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case err := <-waited:
			if err != nil && resultErr == nil {
				result, resultErr = nil, errPrimaryIdentity
			}
		case <-timer.C:
			<-tree.Stop(time.Second)
			<-waited
			if resultErr == nil {
				result, resultErr = nil, errPrimaryIdentity
			}
		}
		// Closing stdout before stopping its writer can block on Windows.
		output.Close()
		<-readDone
	}()
	request := []byte(`{"id":"identity-init","method":"initialize","params":{"clientInfo":{"name":"magpie_identity_check","version":"1"},"capabilities":{}}}`)
	if writeMessage(ctx, input, request) != nil {
		return nil, primaryIdentityError(ctx)
	}
	init, err := primaryIdentityReceive(ctx, events, "identity-init")
	if err != nil || !primaryIdentityInitialize167(init) {
		return nil, primaryIdentityError(ctx)
	}
	if writeMessage(ctx, input, []byte(`{"method":"initialized"}`)) != nil ||
		writeMessage(ctx, input, []byte(`{"id":"identity-status","method":"getAuthStatus","params":{"includeToken":false,"refreshToken":false}}`)) != nil {
		return nil, primaryIdentityError(ctx)
	}
	status, err := primaryIdentityReceive(ctx, events, "identity-status")
	if err != nil {
		return nil, primaryIdentityError(ctx)
	}
	fields, ok := requirementsObject(status, "authMethod", "authToken", "requiresOpenaiAuth")
	if !ok || !requirementsNull(fields["authToken"]) ||
		!trustedAuthStatusResult167(fields, object{"includeToken": json.RawMessage(`false`)}) {
		return nil, errPrimaryIdentity
	}
	// No raw native envelope/error/token is retained in the returned projection.
	// Temporary JSON buffers are ordinary Go memory, not securely wiped storage.
	projected := raw(object{"schema": raw(1), "scope": raw("native-reported-identity-only"),
		"authMethod": fields["authMethod"], "requiresOpenaiAuth": fields["requiresOpenaiAuth"]})
	input.Close()
	// Require clean protocol EOF, not a successful response followed by a new
	// request, notification, malformed line or a peer that refuses shutdown.
	select {
	case event := <-events:
		if event.err != io.EOF {
			return nil, errPrimaryIdentity
		}
	case <-ctx.Done():
		return nil, primaryIdentityError(ctx)
	}
	return projected, nil
}

func primaryIdentityError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errPrimaryIdentity
}

func primaryIdentityScan(ctx context.Context, input io.Reader, events chan<- bridgeEvent) {
	send := func(event bridgeEvent) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), startupProbeLimit+1)
	for scanner.Scan() {
		if len(scanner.Bytes()) > startupProbeLimit {
			send(bridgeEvent{err: errPrimaryIdentity})
			return
		}
		send(bridgeEvent{line: bytes.Clone(scanner.Bytes())})
		if ctx.Err() != nil {
			return
		}
	}
	if scanner.Err() != nil {
		send(bridgeEvent{err: errPrimaryIdentity})
	} else {
		send(bridgeEvent{err: io.EOF})
	}
}

func primaryIdentityReceive(ctx context.Context, events <-chan bridgeEvent, id string) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		return nil, errPrimaryIdentity
	case event := <-events:
		if event.err != nil || !authStatusResponseJSON(event.line) {
			return nil, errPrimaryIdentity
		}
		root, err := startupObject(event.line)
		if err != nil || len(root) != 2 || !requirementsEnum(root["id"], id) || root["result"] == nil {
			return nil, errPrimaryIdentity
		}
		return root["result"], nil
	}
}

func primaryIdentityInitialize167(data json.RawMessage) bool {
	fields, ok := requirementsObject(data, "userAgent", "codexHome", "platformFamily", "platformOs")
	if !ok {
		return false
	}
	for _, value := range fields {
		var s string
		if json.Unmarshal(value, &s) != nil || s == "" || !startupText(s) {
			return false
		}
	}
	// These fields identify the native response shape, not provenance/version.
	// Version scope rests on the operator-reviewed executable pin, as for the
	// existing independently opted-in compatibility modes.
	return true
}
