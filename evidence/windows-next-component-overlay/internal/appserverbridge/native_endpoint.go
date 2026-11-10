package appserverbridge

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeendpoint"
)

// RunWithNativeEndpoint is a staged, trusted in-process composition seam. There
// is no JSON/environment opt-in and no production caller. The caller must prove
// exact native handle retention, executable/catalog provenance and independent
// Reference trust before use. Synthetic Windows socket tests are not that proof.
// Ownership of endpoint and supplied streams transfers immediately, including
// on route/reference validation failure.
// The ordinary Run/RunWithOptions path does not allocate an endpoint or relay.
func RunWithNativeEndpoint(parent context.Context, cfg Config, options LaunchOptions, endpoint *nativeendpoint.Endpoint, reference nativechannel.Reference, input io.ReadCloser, output io.WriteCloser) error {
	if endpoint == nil {
		return errors.New("native endpoint is required")
	}
	return runWithTrustedEndpoint(parent, cfg, options, endpoint, reference, input, output)
}

type endpointOwner interface {
	net.Listener
	Start(*exec.Cmd) error
	Shutdown(context.Context) error
}

type secondaryEndpoint struct {
	endpoint       endpointOwner
	streams        *ownedEndpointStreams
	frontend       *ownedEndpointStreams
	primaryStreams *ownedEndpointStreams
	relay          *nativechannel.Relay
	server         *http.Server
	cancel         context.CancelFunc
	served         chan struct{}
	once           sync.Once
	closeErr       error
	watchDone      chan struct{}
	stopWatch      func() bool
	watchErr       error // read only after watchDone closes
	started        bool  // written/read only by runner goroutine before cleanup begins
}

func runWithTrustedEndpoint(parent context.Context, cfg Config, options LaunchOptions, endpoint endpointOwner, reference nativechannel.Reference, input io.ReadCloser, output io.WriteCloser) (result error) {
	ctx, cancel := context.WithCancel(parent)
	owned := &secondaryEndpoint{endpoint: endpoint, cancel: cancel, served: make(chan struct{}), watchDone: make(chan struct{})}
	// Endpoint owns startup even while Start is in flight. Parent cancellation
	// must revoke it without waiting for Start to return to the runner.
	owned.stopWatch = context.AfterFunc(parent, func() {
		defer close(owned.watchDone)
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		owned.watchErr = endpoint.Shutdown(stopCtx)
	})
	defer func() { result = errors.Join(result, owned.close()) }()
	// Stream ownership transfers at entry, before route/reference admission.
	// Retained cleanup owns every supplied handle even when admission fails.
	if input != nil || output != nil {
		owned.frontend = newOwnedEndpointStreams(output, input)
	}
	if input == nil || output == nil {
		return errors.New("bridge requires input and output")
	}
	input, output = owned.frontend, owned.frontend
	if cfg.SharedNativeRoute == nil || len(cfg.Backends) != 2 || cfg.SharedNativeRoute.GatewayAuth != SharedNativeLocalGateway167 {
		return errors.New("owned endpoint requires the shared local gateway contract")
	}
	// Replace only a local copy before every config, router, argument and
	// environment check. Persisted configuration never supplies this authority.
	route := *cfg.SharedNativeRoute
	cfg.SharedNativeRoute = &route
	address := endpoint.Addr()
	if address == nil {
		return errors.New("owned endpoint address unavailable")
	}
	relay, err := nativechannel.NewRelay(ctx, reference, address.String())
	if err != nil {
		return err
	}
	owned.relay = relay
	route.GatewayURL = "http://" + address.String() + "/v1"
	owned.server = &http.Server{Handler: relay, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	return runWithEndpoint(ctx, cfg, options, input, output, owned)
}

func (s *secondaryEndpoint) start(command *exec.Cmd) error {
	if err := s.endpoint.Start(command); err != nil {
		return errors.Join(errors.New("owned secondary startup failed"), s.close())
	}
	s.started = true
	go func() {
		defer close(s.served)
		// Any serving exit cancels the entire bridge. There is no replacement
		// listener, reconnect, fallback backend or detached request lifetime.
		_ = s.server.Serve(s.endpoint)
		s.cancel()
	}()
	return nil
}

func (s *secondaryEndpoint) close() error {
	s.once.Do(func() {
		s.cancel()
		watchStopped := s.stopWatch()
		if s.primaryStreams != nil {
			s.primaryStreams.retainCleanup()
		}
		if s.frontend != nil {
			s.frontend.retainCleanup()
		}
		if s.streams != nil {
			s.streams.retainCleanup()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Endpoint owns process Wait/Kill and keeps its bind until whole-tree drain.
		// It also interrupts Accept and closes accepted sockets before we join HTTP.
		s.closeErr = s.endpoint.Shutdown(ctx)
		if !watchStopped {
			select {
			case <-s.watchDone:
				s.closeErr = errors.Join(s.closeErr, s.watchErr)
			case <-ctx.Done():
				s.closeErr = errors.Join(s.closeErr, ctx.Err())
			}
		}
		if s.server != nil {
			// Server.Close can wait for Serve's listener cleanup. Retain that
			// owner if an unproven endpoint cannot finish within this deadline.
			closed := make(chan error, 1)
			go func() { closed <- s.server.Close() }()
			select {
			case err := <-closed:
				s.closeErr = errors.Join(s.closeErr, err)
			case <-ctx.Done():
				s.closeErr = errors.Join(s.closeErr, errors.New("owned HTTP server remains quarantined"))
			}
		}
		if s.relay != nil {
			s.closeErr = errors.Join(s.closeErr, s.relay.Close(ctx))
		}
		if s.streams != nil {
			s.closeErr = errors.Join(s.closeErr, s.streams.waitReleased(ctx))
		}
		if s.frontend != nil {
			s.closeErr = errors.Join(s.closeErr, s.frontend.waitReleased(ctx))
		}
		if s.primaryStreams != nil {
			s.closeErr = errors.Join(s.closeErr, s.primaryStreams.waitReleased(ctx))
		}
		if s.started {
			select {
			case <-s.served:
			case <-ctx.Done():
				s.closeErr = errors.Join(s.closeErr, ctx.Err())
			}
		}
	})
	return s.closeErr
}
