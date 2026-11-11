// Package nativeprepdiag observes preparation without carrying operation data.
// Its context value changes neither cancellation nor launch authority.
package nativeprepdiag

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type Stage uint8

const (
	Connect Stage = iota + 1
	ManagerAdmission
	SelectionInspection
	SupplierMetadata
	SupplierFreshness
	GatewayOwnership
	RuntimePreparation
	RuntimeHomes
	RuntimeDiscovery
	RuntimeExecutable
	ProbeDirectory
	RuntimeVersion
	RuntimeRediscovery
	RuntimeRevalidation
	ProbeCleanup
	SupplierRevalidation
	GenerationDirectory
	SupplierCatalog
	BridgeConfiguration
	HostPreparation
	HostInputs
	LauncherResolution
	LauncherVerification
	ProfilePreparation
	HostAdmission
	ChannelStart
	ChannelPublication
	BootstrapListeners
	PreparedPlan
	ControllerValidation
	SelectionLock
	SelectionRead
	SelectionProfile
	SelectionPublication
	PreparationCleanup
	QueryEnvironment
	QueryStart
	QueryWait
	QueryDeadline
	QueryCancelled
	QueryExitTwo
	QueryExitThree
	QueryExitNonzero
	QueryWaitFailure
	QueryOutputBound
	QueryJSON
	QueryIdentity
)

var names = [...]string{
	Connect:              "connect",
	ManagerAdmission:     "manager-admission",
	SelectionInspection:  "selection-inspection",
	SupplierMetadata:     "supplier-metadata",
	SupplierFreshness:    "supplier-freshness",
	GatewayOwnership:     "gateway-ownership",
	RuntimePreparation:   "runtime-preparation",
	RuntimeHomes:         "runtime-homes",
	RuntimeDiscovery:     "runtime-discovery",
	RuntimeExecutable:    "runtime-executable",
	ProbeDirectory:       "probe-directory",
	RuntimeVersion:       "runtime-version",
	RuntimeRediscovery:   "runtime-rediscovery",
	RuntimeRevalidation:  "runtime-revalidation",
	ProbeCleanup:         "probe-cleanup",
	SupplierRevalidation: "supplier-revalidation",
	GenerationDirectory:  "generation-directory",
	SupplierCatalog:      "supplier-catalog",
	BridgeConfiguration:  "bridge-configuration",
	HostPreparation:      "host-preparation",
	HostInputs:           "host-inputs",
	LauncherResolution:   "launcher-resolution",
	LauncherVerification: "launcher-verification",
	ProfilePreparation:   "profile-preparation",
	HostAdmission:        "host-admission",
	ChannelStart:         "channel-start",
	ChannelPublication:   "channel-publication",
	BootstrapListeners:   "bootstrap-listeners",
	PreparedPlan:         "prepared-plan",
	ControllerValidation: "controller-validation",
	SelectionLock:        "selection-lock",
	SelectionRead:        "selection-read",
	SelectionProfile:     "selection-profile",
	SelectionPublication: "selection-publication",
	PreparationCleanup:   "preparation-cleanup",
	QueryEnvironment:     "query-environment",
	QueryStart:           "query-start",
	QueryWait:            "query-wait",
	QueryDeadline:        "query-deadline",
	QueryCancelled:       "query-cancelled",
	QueryExitTwo:         "query-exit-two",
	QueryExitThree:       "query-exit-three",
	QueryExitNonzero:     "query-exit-nonzero",
	QueryWaitFailure:     "query-wait-failure",
	QueryOutputBound:     "query-output-bound",
	QueryJSON:            "query-json",
	QueryIdentity:        "query-identity",
}

const MaxEvents = 128

var attempts atomic.Uint64

type key struct{}
type observer struct {
	mu       sync.Mutex
	id       uint64
	sequence int
	records  []string
	closed   bool
}

// Begin buffers one request's fixed events. finish flushes at most MaxEvents
// records once, after the operation returns and releases its locks. The sink
// must return promptly: logging can delay response delivery, not preparation.
// Panic is contained; missing, dropped or incomplete logs are not evidence.
func Begin(ctx context.Context, sink func(string)) (context.Context, func(bool)) {
	o := &observer{id: attempts.Add(1)}
	ctx = context.WithValue(ctx, key{}, o)
	o.emit(Connect, "entered")
	return ctx, func(success bool) {
		o.mu.Lock()
		if o.closed {
			o.mu.Unlock()
			return
		}
		outcome := "failed"
		if success {
			outcome = "completed"
		}
		o.appendLocked(Connect, outcome)
		o.closed = true
		records := o.records
		o.records = nil
		o.mu.Unlock()
		if sink != nil {
			for _, line := range records {
				func() { defer func() { _ = recover() }(); sink(line) }()
			}
		}
	}
}

// Inherit transfers only the observer to a lifetime context, never cancellation.
func Inherit(lifetime, request context.Context) context.Context {
	if request == nil || lifetime == nil {
		return lifetime
	}
	if o, ok := request.Value(key{}).(*observer); ok {
		return context.WithValue(lifetime, key{}, o)
	}
	return lifetime
}
func (o *observer) emit(stage Stage, outcome string) {
	if o == nil || int(stage) >= len(names) || stage == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.appendLocked(stage, outcome)
	}
}
func (o *observer) appendLocked(stage Stage, outcome string) {
	if o.sequence >= MaxEvents {
		return
	}
	o.sequence++
	o.records = append(o.records, fmt.Sprintf("native-connect-preparation-v1 attempt=%d sequence=%d stage=%s outcome=%s", o.id, o.sequence, names[stage], outcome))
}

// Track observes sequential phases in one function. Reaching Next completes
// the previous phase; returning without Complete fails the current phase.
// Nested functions use independent trackers with the same attempt.
type Tracker struct {
	observer *observer
	stage    Stage
}

func Track(ctx context.Context, stage Stage) *Tracker {
	t := &Tracker{}
	if ctx != nil {
		t.observer, _ = ctx.Value(key{}).(*observer)
	}
	t.Next(stage)
	return t
}
func (t *Tracker) Next(stage Stage) {
	t.Complete()
	t.stage = stage
	t.observer.emit(stage, "entered")
}
func (t *Tracker) Complete() {
	if t.stage != 0 {
		t.observer.emit(t.stage, "completed")
		t.stage = 0
	}
}
func (t *Tracker) End() {
	if t.stage != 0 {
		t.observer.emit(t.stage, "failed")
		t.stage = 0
	}
}
