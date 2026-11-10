package nativehost

import (
	"context"
	"errors"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/nativeconnect"
)

// Catalog prepares provenance- and freshness-verified runtime/catalog inputs.
// Config.Validate alone does not satisfy this contract. A nil Catalog deliberately
// keeps Connect unavailable; no production catalog source is inferred here.
type Catalog func(context.Context) (appserverbridge.Config, error)

type generation interface {
	Plan() (nativeconnect.Plan, error)
	Close(context.Context) error
}

type transaction interface {
	Connect(context.Context) (nativeconnect.Status, error)
	Disconnect() (nativeconnect.Status, error)
	Inspect() (nativeconnect.Status, error)
}

type startGeneration func(context.Context, appserverbridge.Config) (generation, error)

// Manager separates window-owned resources from request-owned transactions.
// It must be closed when the window exits, and retained for retries if Close
// fails. Catalog must honor its context and must not call back into Manager.
// Disconnect restores selection only: even a not-yet-consumed owner is retained
// because consumption can race restoration. Close explicitly revokes all owners.
type Manager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	gate       chan struct{}
	catalog    Catalog
	start      startGeneration
	controller transaction
	owners     []generation
	selected   generation
}

// NewManager is side-effect free. window must be the window lifetime, never an
// HTTP request context. root has the same existing, separate directory contract
// as Start. No supplier metadata is discovered or trusted by this constructor.
func NewManager(window context.Context, server *gateway.Server, root string, store nativeconnect.Store, catalog Catalog) *Manager {
	return newManager(window, catalog, func(ctx context.Context, cfg appserverbridge.Config) (generation, error) {
		h, err := Start(ctx, server, cfg, root)
		if h == nil {
			return nil, err
		}
		return h, err
	}, func(prepare nativeconnect.Prepare) transaction {
		return nativeconnect.New(root, store, prepare)
	})
}

func newManager(window context.Context, catalog Catalog, start startGeneration, controller func(nativeconnect.Prepare) transaction) *Manager {
	if window == nil {
		window = context.Background()
		ctx, cancel := context.WithCancel(window)
		cancel()
		window = ctx
	}
	ctx, cancel := context.WithCancel(window)
	m := &Manager{ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), catalog: catalog, start: start}
	m.controller = controller(m.prepare)
	return m
}

func (m *Manager) acquire(ctx context.Context) error {
	if ctx == nil {
		return ErrUnavailable
	}
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.release()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) release() { <-m.gate }

// prepare is called only by the serialized Controller transaction. Registration
// precedes Plan, including when Start returns both an owner and an error.
func (m *Manager) prepare(ctx context.Context) (nativeconnect.Plan, error) {
	if m.catalog == nil {
		return nativeconnect.Plan{}, nativeconnect.ErrMissingCatalog
	}
	cfg, err := m.catalog(ctx)
	if err != nil {
		return nativeconnect.Plan{}, err
	}
	if ctx.Err() != nil || m.ctx.Err() != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	h, err := m.start(m.ctx, cfg)
	if h != nil {
		m.owners = append(m.owners, h)
		m.selected = h
	}
	if err != nil {
		return nativeconnect.Plan{}, err
	}
	if h == nil || m.ctx.Err() != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	return h.Plan()
}

func (m *Manager) Connect(ctx context.Context) (nativeconnect.Status, error) {
	if err := m.acquire(ctx); err != nil {
		return nativeconnect.Status{}, err
	}
	defer m.release()
	if m.ctx.Err() != nil {
		return nativeconnect.Status{}, ErrUnavailable
	}
	if m.catalog == nil {
		return nativeconnect.Status{}, nativeconnect.ErrMissingCatalog
	}
	current, err := m.controller.Inspect()
	if err != nil {
		return current, err
	}
	if current.JournalPresent {
		// A persisted selection alone cannot prove a live, reusable generation.
		if !current.SelectionConfigured || m.selected == nil {
			return current, nativeconnect.ErrConflict
		}
		if _, err := m.selected.Plan(); err != nil {
			return current, err
		}
		return current, nil
	}
	m.selected = nil
	first := len(m.owners)
	// Window shutdown also interrupts cooperative request preparation, while the
	// generation itself is still started only under m.ctx.
	operation, cancelOperation := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancelOperation)
	defer stop()
	defer cancelOperation()
	status, err := m.controller.Connect(operation)
	if err == nil {
		return status, nil
	}
	// Never infer absence from an error or from a partially changed selection.
	known, inspectErr := m.controller.Inspect()
	if status.JournalPresent || status.RecoveryRequired || inspectErr != nil || known.JournalPresent || known.RecoveryRequired {
		return status, err
	}
	m.selected = nil
	cleanup, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	return status, errors.Join(err, m.drain(cleanup, first))
}

// Status reports transaction evidence, not handshake/chat success or host health.
func (m *Manager) Status(ctx context.Context) (nativeconnect.Status, error) {
	if err := m.acquire(ctx); err != nil {
		return nativeconnect.Status{}, err
	}
	defer m.release()
	return m.controller.Inspect()
}

func (m *Manager) Disconnect(ctx context.Context) (nativeconnect.Status, error) {
	if err := m.acquire(ctx); err != nil {
		return nativeconnect.Status{}, err
	}
	defer m.release()
	status, err := m.controller.Disconnect()
	if err == nil {
		m.selected = nil
	}
	return status, err
}

// Close cancels the window lifetime before waiting for an in-flight transaction,
// then joins/drains owners with a bounded context. It never restores selection.
// Non-cooperative preparation can delay joining; timeout retains all ownership
// for a later Close. Host.Close errors never count as a verified drain.
func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrUnavailable
	}
	m.cancel()
	bounded, cancel := context.WithTimeout(ctx, cleanupLimit)
	defer cancel()
	if err := m.acquire(bounded); err != nil {
		return err
	}
	defer m.release()
	m.selected = nil
	return m.drain(bounded, 0)
}

func (m *Manager) drain(ctx context.Context, first int) error {
	kept := m.owners[:first]
	var errs []error
	for _, h := range m.owners[first:] {
		if err := h.Close(ctx); err != nil {
			kept = append(kept, h)
			errs = append(errs, err)
		}
	}
	clear(m.owners[len(kept):])
	m.owners = kept
	return errors.Join(errs...)
}
