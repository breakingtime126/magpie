package nativebootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

func rejectReparse(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrUnavailable
	}
	a, err := windows.GetFileAttributes(p)
	if err != nil || a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrUnavailable
	}
	return nil
}
func pipeName(generation string) (*uint16, error) {
	if !validGeneration(generation) {
		return nil, ErrUnavailable
	}
	return windows.UTF16PtrFromString(`\\.\pipe\magpie-native-bootstrap-` + generation)
}

// Owner reserves one name for one connection. Creation collision fails closed.
// Close before Accept releases the reservation. Once Accept starts it owns the
// handle until failure or Lease.Close. Close then requests cancellation; wait
// for Accept/Lease.Close to confirm cleanup. No second Accept or reconnect.
// The caller must arrange independent generation cancellation before Connect
// selection is installed, and cancel it on Disconnect or gateway replacement.
type Owner struct {
	mu     sync.Mutex
	handle windows.Handle
	record Record
	peer   Peer
	used   bool
	cancel context.CancelFunc
}

func Listen(record Record, peer Peer) (*Owner, error) {
	if _, err := encode(record); err != nil {
		return nil, err
	}
	if regularPackagePath(peer.Path) != nil {
		return nil, ErrUnavailable
	}
	name, err := pipeName(record.Generation)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, payloadLimit+4, payloadLimit+4, 0, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Owner{handle: h, record: record, peer: peer}, nil
}
func (o *Owner) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.used {
		if o.cancel != nil {
			o.cancel()
		}
		return nil
	}
	o.used = true
	return windows.CloseHandle(o.handle)
}
func (o *Owner) Accept(parent context.Context) (*Lease, error) { return o.accept(parent, false) }

func (o *Owner) accept(parent context.Context, await bool) (lease *Lease, err error) {
	if parent == nil {
		return nil, ErrUnavailable
	}
	o.mu.Lock()
	if o.used {
		o.mu.Unlock()
		return nil, ErrUnavailable
	}
	o.used = true
	lifetime, stop := context.WithCancel(parent)
	o.cancel = stop
	h, record, policy := o.handle, o.record, o.peer
	o.mu.Unlock()
	var p *peerProcess
	defer func() {
		if err != nil {
			stop()
			if p != nil {
				windows.CloseHandle(p.handle)
			}
			windows.CloseHandle(h)
		}
	}()
	ctx := lifetime
	if !await {
		limited, cancel := context.WithTimeout(lifetime, handshakeLimit)
		defer cancel()
		ctx = limited
	}
	if err = overlap(ctx, h, nil, func(ov *windows.Overlapped, n *uint32) error {
		e := windows.ConnectNamedPipe(h, ov)
		if errors.Is(e, windows.ERROR_PIPE_CONNECTED) {
			return nil
		}
		return e
	}); err != nil {
		return nil, err
	}
	if await {
		limited, cancel := context.WithTimeout(lifetime, handshakeLimit)
		defer cancel()
		ctx = limited
	}
	p, err = identify(ctx, h, false, policy)
	if err != nil {
		return nil, err
	}
	// No reference bytes are released before reciprocal live-peer verification.
	var theirs, ours [32]byte
	if _, err = rand.Read(ours[:]); err != nil {
		return nil, err
	}
	if err = readExact(ctx, h, theirs[:]); err != nil {
		return nil, err
	}
	answer := append(append([]byte{}, theirs[:]...), ours[:]...)
	if err = writeExact(ctx, h, answer); err != nil {
		return nil, err
	}
	var echo [32]byte
	if err = readExact(ctx, h, echo[:]); err != nil || !bytes.Equal(echo[:], ours[:]) {
		return nil, ErrUnavailable
	}
	if err = p.check(ctx, h, false, policy); err != nil {
		return nil, err
	}
	data, _ := encode(record)
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(data)))
	if err = writeExact(ctx, h, header[:]); err == nil {
		err = writeExact(ctx, h, data)
	}
	if err != nil {
		return nil, err
	}
	if err = p.check(ctx, h, false, policy); err != nil {
		return nil, err
	}
	return newLease(lifetime, h, p, record, stop), nil
}

// Connect attempts this exact generation once. Expected owner comes from an
// independently selected package path, not the locator. Missing/busy names,
// failed identity, expired generations and dead owners have no fallback.
func Connect(parent context.Context, generation string, policy Peer) (lease *Lease, err error) {
	name, err := pipeName(generation)
	if err != nil {
		return nil, err
	}
	if regularPackagePath(policy.Path) != nil {
		return nil, ErrUnavailable
	}
	// No impersonation of the client is needed or granted by this protocol.
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	var p *peerProcess
	defer func() {
		if err != nil {
			if p != nil {
				windows.CloseHandle(p.handle)
			}
			windows.CloseHandle(h)
		}
	}()
	ctx, cancel := context.WithTimeout(parent, handshakeLimit)
	defer cancel()
	p, err = identify(ctx, h, true, policy)
	if err != nil {
		return nil, err
	}
	var ours [32]byte
	if _, err = rand.Read(ours[:]); err != nil {
		return nil, err
	}
	if err = writeExact(ctx, h, ours[:]); err != nil {
		return nil, err
	}
	var answer [64]byte
	if err = readExact(ctx, h, answer[:]); err != nil || !bytes.Equal(answer[:32], ours[:]) {
		return nil, ErrUnavailable
	}
	if err = p.check(ctx, h, true, policy); err != nil {
		return nil, err
	}
	if err = writeExact(ctx, h, answer[32:]); err != nil {
		return nil, err
	}
	var header [4]byte
	if err = readExact(ctx, h, header[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(header[:])
	if n == 0 || n > payloadLimit {
		return nil, ErrUnavailable
	}
	data := make([]byte, n)
	if err = readExact(ctx, h, data); err != nil {
		return nil, err
	}
	record, err := decode(data, generation)
	if err != nil {
		return nil, err
	}
	if err = p.check(ctx, h, true, policy); err != nil {
		return nil, err
	}
	return newLease(parent, h, p, record, nil), nil
}

type peerProcess struct {
	handle   windows.Handle
	observed Process
}

func pipePID(h windows.Handle, server bool) (uint32, error) {
	var pid uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(h, &pid)
	} else {
		err = windows.GetNamedPipeClientProcessId(h, &pid)
	}
	if err != nil || pid == 0 {
		return 0, ErrUnavailable
	}
	return pid, nil
}
func identify(ctx context.Context, h windows.Handle, server bool, policy Peer) (*peerProcess, error) {
	pid, err := pipePID(h, server)
	if err != nil {
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return nil, ErrUnavailable
	}
	p := &peerProcess{handle: process, observed: Process{PID: pid, NativeHandle: uintptr(process)}}
	if err = p.check(ctx, h, server, policy); err != nil {
		windows.CloseHandle(process)
		return nil, err
	}
	return p, nil
}
func live(h windows.Handle) bool {
	s, e := windows.WaitForSingleObject(h, 0)
	return e == nil && s == uint32(windows.WAIT_TIMEOUT)
}
func (p *peerProcess) check(ctx context.Context, pipe windows.Handle, server bool, policy Peer) error {
	if ctx.Err() != nil || !live(p.handle) {
		return ErrUnavailable
	}
	pid, err := pipePID(pipe, server)
	if err != nil || pid != p.observed.PID {
		return ErrUnavailable
	}
	actual, err := windows.GetProcessId(p.handle)
	if err != nil || actual != pid {
		return ErrUnavailable
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buffer))
	if windows.QueryFullProcessImageName(p.handle, 0, &buffer[0], &n) != nil {
		return ErrUnavailable
	}
	path := windows.UTF16ToString(buffer[:n])
	if !strings.EqualFold(filepath.Clean(path), policy.Path) || regularPackagePath(path) != nil {
		return ErrUnavailable
	}
	// Exact package paths and live handles are the authority here, not a digest
	// of a mutable path. Replaced on-disk images remain outside this guarantee.
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(p.handle, &created, &exited, &kernel, &user) != nil {
		return ErrUnavailable
	}
	stamp := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	if p.observed.Created != 0 && p.observed.Created != stamp {
		return ErrUnavailable
	}
	if !sameUserSession(p.handle) {
		return ErrUnavailable
	}
	p.observed.Path = path
	p.observed.Created = stamp
	if policy.Check != nil {
		if err := policy.Check(ctx, p.observed); err != nil {
			return ErrUnavailable
		}
	}
	if ctx.Err() != nil || !live(p.handle) {
		return ErrUnavailable
	}
	return nil
}
func sameUserSession(peer windows.Handle) bool {
	var theirs, ours windows.Token
	if windows.OpenProcessToken(peer, windows.TOKEN_QUERY, &theirs) != nil {
		return false
	}
	defer theirs.Close()
	if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &ours) != nil {
		return false
	}
	defer ours.Close()
	a, e := theirs.GetTokenUser()
	if e != nil {
		return false
	}
	b, e := ours.GetTokenUser()
	if e != nil || !a.User.Sid.Equals(b.User.Sid) {
		return false
	}
	var x, y, n uint32
	if windows.GetTokenInformation(theirs, windows.TokenSessionId, (*byte)(unsafe.Pointer(&x)), 4, &n) != nil {
		return false
	}
	if windows.GetTokenInformation(ours, windows.TokenSessionId, (*byte)(unsafe.Pointer(&y)), 4, &n) != nil {
		return false
	}
	return x == y
}

// Lease owns the connected pipe and retained process handle. Context is
// canceled by owner exit, pipe break, unexpected post-handshake bytes, parent
// cancellation or Close. A runner must use it and independently drain its Job.
// Closing this lease is not evidence that the native child has drained.
type Lease struct {
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	record     Record
	cleanupErr error // written before done closes; readers synchronize through done
}

func (l *Lease) Context() context.Context { return l.ctx }
func (l *Lease) Record() Record           { return l.record }
func (l *Lease) Close(ctx context.Context) error {
	l.cancel()
	select {
	case <-l.done:
		return l.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func newLease(parent context.Context, h windows.Handle, p *peerProcess, r Record, detach context.CancelFunc) *Lease {
	ctx, cancel := context.WithCancel(parent)
	l := &Lease{ctx: ctx, cancel: cancel, done: make(chan struct{}), record: r}
	processDone := make(chan struct{})
	go func() {
		defer close(processDone)
		defer cancel()
		for ctx.Err() == nil {
			state, err := windows.WaitForSingleObject(p.handle, 25)
			if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
				return
			}
		}
	}()
	go func() {
		var unexpected [1]byte
		_ = readExact(ctx, h, unexpected[:])
		cancel()
		<-processDone
		l.finish(func() error { return windows.CloseHandle(h) }, func() error { return windows.CloseHandle(p.handle) }, detach)
	}()
	return l
}

// finish is called once, after all handle users have exited. Failed cleanup
// remains an error, never proof that both handles were released. done publishes
// that result without blocking Close beyond its caller's deadline.
func (l *Lease) finish(closePipe, closeProcess func() error, detach context.CancelFunc) {
	l.cleanupErr = errors.Join(closePipe(), closeProcess())
	if detach != nil {
		detach()
	}
	close(l.done)
}

// Each overlapped operation owns its event and drains cancellation before its
// buffers can be released. No synchronous, uncancelable pipe I/O is used.
func overlap(ctx context.Context, h windows.Handle, count *uint32, start func(*windows.Overlapped, *uint32) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	ov := windows.Overlapped{HEvent: event}
	var done uint32
	if count == nil {
		count = &done
	}
	err = start(&ov, count)
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return err
	}
	for {
		if ctx.Err() != nil {
			_ = windows.CancelIoEx(h, &ov)
			// Windows must complete the canceled request before ov/buffers disappear.
			_ = windows.GetOverlappedResult(h, &ov, count, true)
			return ctx.Err()
		}
		state, waitErr := windows.WaitForSingleObject(event, 25)
		if waitErr != nil || state != uint32(windows.WAIT_TIMEOUT) {
			if waitErr != nil {
				_ = windows.CancelIoEx(h, &ov)
				_ = windows.GetOverlappedResult(h, &ov, count, true)
				return waitErr
			}
			return windows.GetOverlappedResult(h, &ov, count, false)
		}
	}
}
func transfer(ctx context.Context, h windows.Handle, b []byte, write bool) error {
	for len(b) > 0 {
		var n uint32
		err := overlap(ctx, h, &n, func(ov *windows.Overlapped, count *uint32) error {
			var e error
			if write {
				e = windows.WriteFile(h, b, count, ov)
			} else {
				e = windows.ReadFile(h, b, count, ov)
			}
			return e
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		b = b[n:]
	}
	return nil
}
func readExact(ctx context.Context, h windows.Handle, b []byte) error {
	return transfer(ctx, h, b, false)
}
func writeExact(ctx context.Context, h windows.Handle, b []byte) error {
	return transfer(ctx, h, b, true)
}
