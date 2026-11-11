package nativebootstrap

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"golang.org/x/sys/windows"
)

func ownPeer(t *testing.T) Peer {
	t.Helper()
	p, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return Peer{Path: p}
}
func uniqueRecord(t *testing.T) Record {
	t.Helper()
	r := testRecord(t)
	var n [16]byte
	if _, e := rand.Read(n[:]); e != nil {
		t.Fatal(e)
	}
	r.Generation = hex.EncodeToString(n[:])
	return r
}
func closeLease(t *testing.T, l *Lease) {
	t.Helper()
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := l.Close(ctx); err != nil {
		t.Error("lease did not drain", err)
	}
}

type accepted struct {
	lease *Lease
	err   error
}

func acceptAsync(ctx context.Context, o *Owner) <-chan accepted {
	ch := make(chan accepted, 1)
	go func() { l, e := o.Accept(ctx); ch <- accepted{l, e} }()
	return ch
}
func getAccepted(t *testing.T, ch <-chan accepted) accepted {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(7 * time.Second):
		t.Fatal("accept timeout")
		return accepted{}
	}
}
func TestWindowsRealPipeLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := uniqueRecord(t)
	peer := ownPeer(t)
	checks := 0
	peer.Check = func(_ context.Context, p Process) error {
		checks++
		if p.PID != windows.GetCurrentProcessId() || p.NativeHandle == 0 || p.Created == 0 {
			return ErrUnavailable
		}
		return nil
	}
	o, e := Listen(r, peer)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	if duplicate, e := Listen(r, peer); e == nil {
		duplicate.Close()
		t.Fatal("name collision accepted")
	}
	ac := acceptAsync(ctx, o)
	client, e := Connect(ctx, r.Generation, ownPeer(t))
	if e != nil {
		t.Fatal(e)
	}
	defer closeLease(t, client)
	a := getAccepted(t, ac)
	if a.err != nil {
		t.Fatal(a.err)
	}
	defer closeLease(t, a.lease)
	if client.Record() != r || checks < 3 {
		t.Fatal("record or reciprocal checks missing", checks)
	}
	if _, e := o.Accept(ctx); e == nil {
		t.Fatal("accepted second connection")
	}
	closeLease(t, a.lease)
	select {
	case <-client.Context().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pipe break did not cancel client")
	}
	if _, e := Connect(ctx, r.Generation, ownPeer(t)); e == nil {
		t.Fatal("reconnected dead generation")
	}
}
func TestWindowsWrongOwnerAndPolicy(t *testing.T) {
	for _, side := range []string{"server-path", "client-policy", "server-policy"} {
		t.Run(side, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			r := uniqueRecord(t)
			serverPolicy, clientPolicy := ownPeer(t), ownPeer(t)
			if side == "server-path" {
				p := filepath.Join(t.TempDir(), "wrong-owner.exe")
				if e := os.WriteFile(p, []byte("not executable"), 0600); e != nil {
					t.Fatal(e)
				}
				clientPolicy.Path = p
			}
			deny := func(context.Context, Process) error { return ErrUnavailable }
			if side == "client-policy" {
				serverPolicy.Check = deny
			}
			if side == "server-policy" {
				clientPolicy.Check = deny
			}
			o, e := Listen(r, serverPolicy)
			if e != nil {
				t.Fatal(e)
			}
			defer o.Close()
			ac := acceptAsync(ctx, o)
			if l, e := Connect(ctx, r.Generation, clientPolicy); e == nil {
				closeLease(t, l)
				t.Fatal("released reference despite owner rejection")
			}
			a := getAccepted(t, ac)
			if a.err == nil {
				closeLease(t, a.lease)
				t.Fatal("server succeeded after rejection")
			}
		})
	}
}
func TestWindowsAcceptCancellation(t *testing.T) {
	r := uniqueRecord(t)
	o, e := Listen(r, ownPeer(t))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ac := acceptAsync(ctx, o)
	cancel()
	a := getAccepted(t, ac)
	if a.err == nil {
		closeLease(t, a.lease)
		t.Fatal("canceled accept succeeded")
	}
	o.Close()
	// Once cleanup finished, a new explicit owner can reserve the name. The old
	// Owner itself cannot restart. This is not implicit generation refresh.
	again, e := Listen(r, ownPeer(t))
	if e != nil {
		t.Fatal("reservation leaked", e)
	}
	again.Close()
}

// This helper executes only this synthetic test binary, never a native Codex
// executable or accounts. Parent owns its suspended-start retained Windows Job.
func TestWindowsBootstrapHelper(t *testing.T) {
	generation := os.Getenv("MAGPIE_BOOTSTRAP_TEST_GENERATION")
	if generation == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l, e := Connect(ctx, generation, ownPeer(t))
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(os.Stdout, "bootstrap-ready")
	<-l.Context().Done()
	closeLease(t, l)
}
func TestWindowsOwnedChildDeathCancelsLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := uniqueRecord(t)
	o, e := Listen(r, ownPeer(t))
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	ac := acceptAsync(ctx, o)
	read, write, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer read.Close()
	defer write.Close()
	cmd := exec.Command(ownPeer(t).Path, "-test.run=^TestWindowsBootstrapHelper$", "-test.timeout=20s")
	cmd.Env = append([]string{}, os.Environ()...)
	// Only a public random pipe locator is added. No credentials or native state.
	for i := len(cmd.Env) - 1; i >= 0; i-- {
		if strings.HasPrefix(cmd.Env[i], "MAGPIE_BOOTSTRAP_TEST_GENERATION=") {
			cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
		}
	}
	cmd.Env = append(cmd.Env, "MAGPIE_BOOTSTRAP_TEST_GENERATION="+r.Generation)
	cmd.Stdout = write
	cmd.Stderr = write
	tree, e := proc.StartTreeDrained(cmd)
	if tree != nil {
		defer func() {
			_ = tree.Stop()
			cleanup, c := context.WithTimeout(context.Background(), 4*time.Second)
			defer c()
			if e := tree.Wait(cleanup); e != nil {
				t.Error("owned test Job drain", e)
			}
		}()
	}
	if e != nil {
		t.Fatal(e)
	}
	write.Close()
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(read)
		if scanner.Scan() {
			line <- scanner.Text()
		} else {
			line <- ""
		}
	}()
	select {
	case text := <-line:
		if text != "bootstrap-ready" {
			t.Fatal("child handshake failed", text)
		}
	case <-ctx.Done():
		t.Fatal("child readiness timeout")
	}
	a := getAccepted(t, ac)
	if a.err != nil {
		t.Fatal(a.err)
	}
	defer closeLease(t, a.lease)
	if e := tree.Stop(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-a.lease.Context().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("retained peer death did not cancel lease")
	}
}

func TestWindowsMalformedFramesAndHandshakeCancellation(t *testing.T) {
	for _, mode := range []string{"oversized", "malformed", "wrong-generation", "wrong-challenge", "stall"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			r := uniqueRecord(t)
			o, e := Listen(r, ownPeer(t))
			if e != nil {
				t.Fatal(e)
			}
			// A controlled, malformed server owns this real pipe. Bypass Accept only
			// to exercise the wire rejection paths, never to establish production trust.
			o.mu.Lock()
			o.used = true
			h := o.handle
			o.mu.Unlock()
			done := make(chan error, 1)
			go func() {
				defer windows.CloseHandle(h)
				e := overlap(ctx, h, nil, func(ov *windows.Overlapped, _ *uint32) error {
					e := windows.ConnectNamedPipe(h, ov)
					if e == windows.ERROR_PIPE_CONNECTED {
						return nil
					}
					return e
				})
				if e != nil {
					done <- e
					return
				}
				var nonce [32]byte
				if e = readExact(ctx, h, nonce[:]); e != nil {
					done <- e
					return
				}
				if mode == "stall" {
					<-ctx.Done()
					done <- nil
					return
				}
				challenge := append(append([]byte{}, nonce[:]...), make([]byte, 32)...)
				if mode == "wrong-challenge" {
					challenge[0] ^= 1
				}
				if e = writeExact(ctx, h, challenge); e != nil {
					done <- e
					return
				}
				if mode == "wrong-challenge" {
					done <- nil
					return
				}
				if e = readExact(ctx, h, nonce[:]); e != nil {
					done <- e
					return
				}
				data := []byte("not JSON")
				if mode == "wrong-generation" {
					r.Generation = strings.Repeat("f", 32)
					data, _ = encode(r)
				}
				var header [4]byte
				n := uint32(len(data))
				if mode == "oversized" {
					n = payloadLimit + 1
				}
				binary.LittleEndian.PutUint32(header[:], n)
				if e = writeExact(ctx, h, header[:]); e == nil && mode != "oversized" {
					e = writeExact(ctx, h, data)
				}
				done <- e
			}()
			clientCtx := ctx
			var stop context.CancelFunc
			if mode == "stall" {
				clientCtx, stop = context.WithTimeout(ctx, 75*time.Millisecond)
				defer stop()
			}
			if l, e := Connect(clientCtx, r.Generation, ownPeer(t)); e == nil {
				closeLease(t, l)
				t.Fatal("malformed/stalled server accepted")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("malformed server cleanup incomplete")
			}
		})
	}
}

func TestWindowsLeaseCleanupErrorsAndDeadline(t *testing.T) {
	for _, which := range []string{"pipe", "process", "both", "none"} {
		t.Run(which, func(t *testing.T) {
			pipeErr, processErr := errors.New("injected pipe close failure"), errors.New("injected process close failure")
			ctx, cancel := context.WithCancel(context.Background())
			l := &Lease{ctx: ctx, cancel: cancel, done: make(chan struct{})}
			calls := 0
			detached := false
			l.finish(func() error {
				calls++
				if which == "pipe" || which == "both" {
					return pipeErr
				}
				return nil
			}, func() error {
				calls++
				if which == "process" || which == "both" {
					return processErr
				}
				return nil
			}, func() { detached = true })
			err := l.Close(context.Background())
			if calls != 2 || !detached {
				t.Fatal("cleanup did not attempt both handles and detach owner")
			}
			if errors.Is(err, pipeErr) != (which == "pipe" || which == "both") || errors.Is(err, processErr) != (which == "process" || which == "both") {
				t.Fatal("lost cleanup error", err)
			}
			if which == "none" && err != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := &Lease{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	deadline, stop := context.WithCancel(context.Background())
	stop()
	if !errors.Is(l.Close(deadline), context.Canceled) {
		t.Fatal("incomplete cleanup ignored caller deadline")
	}
}

func TestWindowsStartupProbeDoesNotConsumeLaunch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r := uniqueRecord(t)
	launch, err := Listen(r, ownPeer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Close()
	// A launch reservation cannot be used as an inspection channel.
	if l, err := ConnectStartupProbe(ctx, r.Generation, ownPeer(t)); err == nil {
		closeLease(t, l)
		t.Fatal("startup probe connected to launch reservation")
	}
	probe, err := ListenStartupProbe(r, ownPeer(t))
	if err != nil {
		t.Fatal("startup reservation collided with launch", err)
	}
	defer probe.Close()
	if duplicate, err := ListenStartupProbe(r, ownPeer(t)); err == nil {
		duplicate.Close()
		t.Fatal("duplicate startup reservation accepted")
	}
	for _, phase := range []struct {
		name    string
		owner   *Owner
		connect func(context.Context, string, Peer) (*Lease, error)
	}{{"startup", probe, ConnectStartupProbe}, {"launch", launch, Connect}} {
		t.Run(phase.name, func(t *testing.T) {
			ac := acceptAsync(ctx, phase.owner)
			client, err := phase.connect(ctx, r.Generation, ownPeer(t))
			if err != nil {
				t.Fatal(err)
			}
			defer closeLease(t, client)
			a := getAccepted(t, ac)
			if a.err != nil {
				t.Fatal(a.err)
			}
			defer closeLease(t, a.lease)
			if client.Record() != r {
				t.Fatal("operation changed the launch record")
			}
			if _, err := phase.owner.Accept(ctx); err == nil {
				t.Fatal("operation accepted a second connection")
			}
			closeLease(t, a.lease)
			if again, err := phase.connect(ctx, r.Generation, ownPeer(t)); err == nil {
				closeLease(t, again)
				t.Fatal("operation reconnected after consumption")
			}
		})
	}
}

func TestWindowsStartupProbeRejectsPeerPolicy(t *testing.T) {
	for _, side := range []string{"owner", "client"} {
		t.Run(side, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := uniqueRecord(t)
			ownerPeer, clientPeer := ownPeer(t), ownPeer(t)
			deny := func(context.Context, Process) error { return ErrUnavailable }
			if side == "owner" {
				clientPeer.Check = deny
			} else {
				ownerPeer.Check = deny
			}
			o, err := ListenStartupProbe(r, ownerPeer)
			if err != nil {
				t.Fatal(err)
			}
			defer o.Close()
			ac := acceptAsync(ctx, o)
			if l, err := ConnectStartupProbe(ctx, r.Generation, clientPeer); err == nil {
				closeLease(t, l)
				t.Fatal("probe disclosed record despite rejected peer")
			}
			a := getAccepted(t, ac)
			if a.err == nil {
				closeLease(t, a.lease)
				t.Fatal("probe owner accepted rejected peer")
			}
		})
	}
}
