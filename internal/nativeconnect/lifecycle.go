package nativeconnect

import (
	"context"
	"errors"
	"time"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

const cleanupTimeout = 3 * time.Second

// Generation owns resources prepared for one future launch. Plan must reject
// expired or consumed generations. Close must verify cleanup, and allow retry.
type Generation interface {
	Plan() (Plan, error)
	Close(context.Context) error
}

// PrepareGeneration validates runtime provenance, supplier metadata and gateway
// ownership before returning. request cancels preparation; lifetime owns any
// resources that must survive the request. Return partial ownership even on error.
// Implementations must honor cancellation and must not call back into Manager.
type PrepareGeneration func(request, lifetime context.Context) (Generation, error)

// Manager owns preparation independently of HTTP requests and selection recovery.
// A nil factory deliberately keeps Connect disabled without creating files.
type Manager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	gate       chan struct{}
	controller *Controller
	factory    PrepareGeneration
	owners     []Generation
	selected   Generation
}

func NewManager(lifetime context.Context, root string, store Store, factory PrepareGeneration) *Manager {
	if lifetime == nil {
		lifetime = context.Background()
		ctx, cancel := context.WithCancel(lifetime)
		cancel()
		lifetime = ctx
	}
	ctx, cancel := context.WithCancel(lifetime)
	m := &Manager{ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), factory: factory}
	m.controller = New(root, store, m.prepare)
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
func (m *Manager) prepare(ctx context.Context) (Plan, error) {
	if m.factory == nil {
		return Plan{}, ErrMissingCatalog
	}
	h, err := m.factory(ctx, m.ctx)
	if h != nil {
		m.owners = append(m.owners, h)
		m.selected = h
	}
	if err != nil {
		return Plan{}, err
	}
	if h == nil || m.ctx.Err() != nil || ctx.Err() != nil {
		return Plan{}, ErrUnavailable
	}
	trace := nativeprepdiag.Track(ctx, nativeprepdiag.PreparedPlan)
	defer trace.End()
	plan, planErr := h.Plan()
	if planErr == nil {
		trace.Complete()
	}
	return plan, planErr
}
func (m *Manager) Connect(ctx context.Context) (Status, error) {
	trace := nativeprepdiag.Track(ctx, nativeprepdiag.ManagerAdmission)
	defer trace.End()
	if err := m.acquire(ctx); err != nil {
		return Status{}, err
	}
	defer m.release()
	if m.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	if m.factory == nil {
		return Status{}, ErrMissingCatalog
	}
	trace.Next(nativeprepdiag.SelectionInspection)
	current, err := m.controller.Inspect()
	if err != nil {
		return current, err
	}
	if current.JournalPresent {
		if !current.SelectionConfigured || m.selected == nil {
			return current, ErrConflict
		}
		if err := m.controller.selectionReady(); err != nil {
			current.SelectionConfigured = false
			current.RecoveryRequired = true
			return current, err
		}
		if _, err := m.selected.Plan(); err != nil {
			return current, err
		}
		trace.Complete()
		return current, nil
	}
	trace.Complete()
	m.selected = nil
	first := len(m.owners)
	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	status, err := m.controller.Connect(operation)
	if err == nil {
		return status, nil
	}
	known, inspectErr := m.controller.Inspect()
	// Uncertainty must retain ownership: a launch may already be consuming it.
	if status.RecoveryRequired || inspectErr != nil || known.JournalPresent {
		return status, err
	}
	m.selected = nil
	// Cleanup belongs to the host; request cancellation cannot prevent it.
	cleanup, done := context.WithTimeout(context.Background(), cleanupTimeout)
	defer done()
	trace.Next(nativeprepdiag.PreparationCleanup)
	cleanupErr := m.drain(cleanup, first)
	if cleanupErr == nil {
		trace.Complete()
	}
	return status, errors.Join(err, cleanupErr)
}
func (m *Manager) Disconnect(ctx context.Context) (Status, error) {
	if err := m.acquire(ctx); err != nil {
		return Status{}, err
	}
	defer m.release()
	if m.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	status, err := m.controller.Disconnect()
	if err == nil {
		m.selected = nil
	}
	// Restoration never proves an existing launch stopped using its generation.
	return status, err
}
func (m *Manager) Status(ctx context.Context) (Status, error) {
	if err := m.acquire(ctx); err != nil {
		return Status{}, err
	}
	defer m.release()
	return m.controller.Inspect()
}

// Close cancels before joining preparation. It never restores launch selection.
// On timeout/error retain the Manager and retry Close with a fresh context.
func (m *Manager) Close(ctx context.Context) error {
	m.cancel()
	if ctx == nil {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, cleanupTimeout)
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
