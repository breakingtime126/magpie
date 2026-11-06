package appserverbridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/proc"
)

const writeTimeout = 10 * time.Second

type bridgeEvent struct {
	backend string
	line    []byte
	err     error
}

type childServer struct {
	tree   *proc.Tree
	input  io.WriteCloser
	output io.ReadCloser
}

// Run owns input and output for the duration of the bridge and closes them on
// exit. Their Close methods must unblock pending reads and writes. EOF from the
// client is normal shutdown; losing either backend fails the entire connection.
// It never restarts a backend or redirects requests to a different account.
func Run(parent context.Context, cfg Config, input io.ReadCloser, output io.WriteCloser) error {
	return RunWithOptions(parent, cfg, LaunchOptions{}, input, output)
}

// RunWithOptions preserves the reviewed Desktop startup controls on both
// children. It has the same stream ownership and shutdown contract as Run.
func RunWithOptions(parent context.Context, cfg Config, options LaunchOptions, input io.ReadCloser, output io.WriteCloser) error {
	if input == nil || output == nil {
		return errors.New("bridge requires input and output")
	}
	if options.CodexAppToolsMCPEnabled && cfg.DesktopPluginOverride != DesktopPluginCodexAppTools167 {
		return errors.New("Desktop plugin override is disabled")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.RequirementsRead != "" || cfg.Attestation != "" || cfg.GatewayOAuth != "" || cfg.AuthStatus != "" || cfg.DesktopPluginOverride != "" {
		// Check both reviewed executables before either process can read or
		// refresh account/policy state. This is not an OS security boundary.
		for _, backend := range cfg.Backends {
			if !startupProbeDigestMatches(parent, backend) {
				return errors.New("reviewed backend digest verification failed")
			}
		}
	}
	workingDirectory := cfg.WorkingDirectory
	if workingDirectory != "" {
		var err error
		workingDirectory, err = filepath.EvalSymlinks(workingDirectory)
		if err != nil {
			return errors.New("cannot resolve working_directory")
		}
	}
	router, err := NewRouter(cfg.Backends)
	if err != nil {
		return err
	}
	router.configRequirementsRead = cfg.RequirementsRead != ""
	router.configRequirementsNull167 = cfg.RequirementsRead == ConfigRequirementsCompareNull167
	router.auth = cfg.authCompatibility()
	ctx, cancel := context.WithCancel(parent)
	processContext, stopProcesses := context.WithCancel(context.WithoutCancel(parent))
	defer stopProcesses()
	events := make(chan bridgeEvent)
	children := map[string]*childServer{}
	var readers sync.WaitGroup
	defer func() {
		cancel()
		input.Close()
		output.Close()
		for _, child := range children {
			child.input.Close()
			child.output.Close()
		}
		stopChildren(children)
		readers.Wait()
	}()
	for _, b := range cfg.Backends {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		command := proc.CommandContext(processContext, b.Command, options.backendArgs(b.Provider)...)
		command.Env = backendEnvironment(b)
		command.Dir = b.Home
		if workingDirectory != "" {
			command.Dir = workingDirectory
		}
		// A nil Stderr connects the child directly to the null device. Unlike
		// an io.Discard copy pipe, descendants cannot keep a copier alive.
		// Backend diagnostics may contain credentials or model input.
		command.Stderr = nil
		in, err := command.StdinPipe()
		if err != nil {
			return fmt.Errorf("cannot prepare backend %s input", b.ID)
		}
		out, err := command.StdoutPipe()
		if err != nil {
			in.Close()
			return fmt.Errorf("cannot prepare backend %s output", b.ID)
		}
		tree, err := proc.StartTreeStrict(command)
		if err != nil {
			in.Close()
			out.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("cannot start backend %s", b.ID)
		}
		children[b.ID] = &childServer{tree: tree, input: in, output: out}
		readers.Add(1)
		go func(id string, src io.Reader) { defer readers.Done(); scanMessages(ctx, src, id, events) }(b.ID, out)
	}
	readers.Add(1)
	go func() { defer readers.Done(); scanMessages(ctx, input, "", events) }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-events:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if event.err != nil {
				if event.backend == "" && errors.Is(event.err, io.EOF) {
					return nil
				}
				if event.backend == "" {
					return errors.New("cannot read client JSONL stream")
				}
				return fmt.Errorf("backend %s stream ended or exceeded its limit", event.backend)
			}
			var deliveries []Delivery
			if event.backend == "" {
				deliveries, err = router.Client(event.line)
			} else {
				deliveries, err = router.Server(event.backend, event.line)
			}
			if err != nil {
				return errors.New("app-server bridge protocol failure")
			}
			for _, delivery := range deliveries {
				target := output
				if delivery.Backend != "" {
					child, ok := children[delivery.Backend]
					if !ok {
						return errors.New("bridge routed to an unknown backend")
					}
					target = child.input
				}
				if err := writeMessage(ctx, target, delivery.Message); err != nil {
					return err
				}
			}
		}
	}
}

func scanMessages(ctx context.Context, input io.Reader, backend string, events chan<- bridgeEvent) {
	send := func(event bridgeEvent) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), MaxMessageBytes+1)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > MaxMessageBytes {
			send(bridgeEvent{backend: backend, err: errors.New("message too large")})
			return
		}
		if !send(bridgeEvent{backend: backend, line: append([]byte(nil), line...)}) {
			return
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	send(bridgeEvent{backend: backend, err: err})
}

// A stalled pipe cannot prevent cancellation or keep the other backend alive.
// Close releases the writer before its goroutine is joined.
func writeMessage(ctx context.Context, output io.WriteCloser, message []byte) error {
	if len(message) > MaxMessageBytes {
		return errors.New("bridge output message exceeds limit")
	}
	data := append(append([]byte(nil), message...), '\n')
	done := make(chan error, 1)
	go func() {
		n, err := output.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		done <- err
	}()
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return errors.New("cannot write bridge stream")
		}
		return nil
	case <-ctx.Done():
		output.Close()
		<-done
		return ctx.Err()
	case <-timer.C:
		output.Close()
		<-done
		return errors.New("bridge stream write timed out")
	}
}

func backendEnvironment(b Backend) []string {
	env := []string{
		"HOME=" + b.Home, "USERPROFILE=" + b.Home, "CODEX_HOME=" + b.CodexHome,
		"XDG_CONFIG_HOME=" + filepath.Join(b.Home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(b.Home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(b.Home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(b.Home, ".local", "state"),
		"APPDATA=" + filepath.Join(b.Home, "AppData", "Roaming"),
		"LOCALAPPDATA=" + filepath.Join(b.Home, "AppData", "Local"),
		"TMPDIR=" + b.Home, "TMP=" + b.Home, "TEMP=" + b.Home,
	}
	if runtime.GOOS == "windows" {
		if root := appdir.Getenv("SystemRoot"); root != "" {
			env = append(env, "SystemRoot="+root, "PATH="+filepath.Join(root, "System32"))
		}
	} else {
		env = append(env, "PATH=/usr/bin:/bin")
	}
	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE", "TERM"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	for _, key := range b.InheritEnv {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// Give an EOF-aware app server a short opportunity to release its own locks.
// Then use proc's owned session/job, never a name-based process-wide kill.
func stopChildren(children map[string]*childServer) {
	var waiters sync.WaitGroup
	for _, child := range children {
		waiters.Add(1)
		go func(child *childServer) { defer waiters.Done(); _ = child.tree.Wait() }(child)
	}
	done := make(chan struct{})
	go func() { waiters.Wait(); close(done) }()
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}
	stopped := make([]<-chan struct{}, 0, len(children))
	for _, child := range children {
		stopped = append(stopped, child.tree.Stop(time.Second))
	}
	for _, stop := range stopped {
		<-stop
	}
	<-done
}
