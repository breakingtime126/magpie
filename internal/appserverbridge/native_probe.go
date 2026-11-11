package appserverbridge

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeendpoint"
	"github.com/yetone/magpie/internal/proc"
)

type probeEndpoint interface {
	net.Listener
	Start(*exec.Cmd) error
	WaitForExit(context.Context) error
	Shutdown(context.Context) error
}

// ProbeStartupWithNativeEndpoint requires an authenticated generation reference
// and exact profile snapshot. It owns endpoint on entry, including on failure.
// Only genuine successful pinned native probe output can become its result.
func ProbeStartupWithNativeEndpoint(parent context.Context, cfg Config, options LaunchOptions, info StartupClientInfo, endpoint *nativeendpoint.Endpoint, reference nativechannel.Reference) ([]byte, error) {
	return probeWithNativeEndpoint(parent, cfg, options, info, endpoint, reference)
}

func probeWithNativeEndpoint(parent context.Context, cfg Config, options LaunchOptions, info StartupClientInfo, endpoint probeEndpoint, reference nativechannel.Reference) (result []byte, resultErr error) {
	if endpoint == nil {
		return nil, errStartupProbe
	}
	ctx, cancel := context.WithTimeout(parent, startupProbeTimeout)
	defer cancel()
	var relay *nativechannel.Relay
	var server *http.Server
	var served chan error
	watchDone := make(chan struct{})
	var watchErr error
	stopWatch := context.AfterFunc(ctx, func() {
		defer close(watchDone)
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		watchErr = endpoint.Shutdown(closeCtx)
	})
	defer func() {
		stopped := stopWatch()
		cancel()
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		cleanupErr := endpoint.Shutdown(closeCtx)
		if !stopped {
			select {
			case <-watchDone:
				cleanupErr = errors.Join(cleanupErr, watchErr)
			case <-closeCtx.Done():
				cleanupErr = errors.Join(cleanupErr, closeCtx.Err())
			}
		}
		if server != nil {
			closed := make(chan error, 1)
			go func() { closed <- server.Close() }()
			select {
			case err := <-closed:
				cleanupErr = errors.Join(cleanupErr, err)
			case <-closeCtx.Done():
				cleanupErr = errors.Join(cleanupErr, closeCtx.Err())
			}
		}
		if served != nil {
			select {
			case err := <-served:
				if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, nativeendpoint.ErrState) && !errors.Is(err, net.ErrClosed) {
					cleanupErr = errors.Join(cleanupErr, err)
				}
			case <-closeCtx.Done():
				cleanupErr = errors.Join(cleanupErr, closeCtx.Err())
			}
		}
		if relay != nil {
			cleanupErr = errors.Join(cleanupErr, relay.Close(closeCtx))
		}
		resultErr = errors.Join(resultErr, cleanupErr)
		if resultErr != nil {
			result = nil
		}
	}()
	if ctx.Err() != nil || cfg.SharedNativeRoute == nil || cfg.SharedNativeRoute.GatewayAuth != SharedNativeLocalGateway167 || len(cfg.Backends) != 2 || cfg.StartupProbe != StartupProbeCompareUnrestricted167 {
		return nil, errStartupProbe
	}
	address := endpoint.Addr()
	if address == nil {
		return nil, errStartupProbe
	}
	var err error
	relay, err = nativechannel.NewRelay(ctx, reference, address.String())
	if err != nil {
		return nil, errStartupProbe
	}
	route := *cfg.SharedNativeRoute
	cfg.SharedNativeRoute = &route
	route.GatewayURL = "http://" + address.String() + "/v1"
	if cfg.Validate() != nil {
		return nil, errStartupProbe
	}
	environments := map[string][]string{}
	for _, b := range cfg.Backends {
		env, err := cfg.sharedNativeEnvironment167(b, os.Environ())
		if err != nil {
			return nil, errStartupProbe
		}
		environments[b.ID] = env
	}
	runner := func(runCtx context.Context, b Backend, directory string, args []string) ([]byte, error) {
		if b.ID == cfg.Backends[0].ID {
			return runProductionPrimaryProbe(runCtx, b, directory, args, environments[b.ID])
		}
		return runOwnedStartupProbe(runCtx, b, directory, args, environments[b.ID], endpoint, func() {
			server = &http.Server{Handler: relay, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
			served = make(chan error, 1)
			go func() { served <- server.Serve(endpoint) }()
		})
	}
	result, resultErr = probeStartup(ctx, cfg, options, info, startupProbeTimeout, runner)
	if resultErr != nil {
		return nil, resultErr
	}
	// Recheck mutable files after execution; a successful child is not a lease
	// extension or evidence that the generation's configuration stayed intact.
	if ctx.Err() != nil || cfg.Validate() != nil {
		return nil, errStartupProbe
	}
	for _, b := range cfg.Backends {
		if !startupProbeDigestMatches(ctx, b) {
			return nil, errStartupProbe
		}
	}
	return result, nil
}

func runOwnedStartupProbe(ctx context.Context, b Backend, directory string, args, environment []string, endpoint probeEndpoint, started func()) ([]byte, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, errStartupProbe
	}
	defer reader.Close()
	command := proc.Command(b.Command, args...)
	command.Env = environment
	command.Dir = directory
	command.Stdout = writer
	err = endpoint.Start(command)
	writer.Close()
	if err != nil {
		return nil, errStartupProbe
	}
	started()
	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(reader, startupProbeLimit+1))
		reads <- readResult{data, err}
	}()
	defer func() { reader.Close(); <-reads }()
	// Keep the sole read result available for deferred goroutine drainage.
	var r readResult
	select {
	case <-ctx.Done():
		return nil, errStartupProbe
	case r = <-reads:
		reads <- r
	}
	if r.err != nil || len(r.data) > startupProbeLimit {
		return nil, errStartupProbe
	}
	if endpoint.WaitForExit(ctx) != nil || ctx.Err() != nil {
		return nil, errStartupProbe
	}
	return r.data, nil
}
