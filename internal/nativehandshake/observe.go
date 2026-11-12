// Package nativehandshake records fixed diagnostic milestones, never protocol
// data. Ordinary builds have no filesystem observer or diagnostic authority.
package nativehandshake

import "context"

type Stage uint8

const (
	HostListening Stage = iota + 1
	HostProbeAdmitted
	HostProbeAcknowledged
	HostJSONLAdmitted
	AdapterEntered
	StartupProbeRequested
	JSONLRequested
	ProfileAuthenticated
	EndpointAllocated
	ProbeResultValidated
	ProbeOutputWritten
	ProbeFailed
	JSONLFailed
	PrimaryStarted
	SecondaryStarted
	InitializeReceived
	PrimaryInitializeWritten
	SecondaryInitializeWritten
	PrimaryInitializeReply
	SecondaryInitializeReply
	PrimaryInitializeError
	SecondaryInitializeError
	InitializationRejected
	InitializationReady
	InitializeOutputWritten
	ProtocolFailed
	StreamWriteFailed
	ProbeEnvironmentAdmitted
	ProbeEnvironmentRejected
	PrimaryExited
	BridgeContextCancelled
	ClientStreamEnded
	PrimaryStreamEnded
	SecondaryStreamEnded
	ProtocolSourceClient
	ProtocolSourcePrimary
	ProtocolSourceSecondary
	ProtocolClassEnvelope
	ProtocolClassCorrelation
	ProtocolClassUnscopedRequest
	ProtocolClassThreadOwnership
	ProtocolClassInitialization
	ProtocolClassClientNotification
	ProtocolClassOther
	ProtocolShapeNotification
	ProtocolShapeRequest
	ProtocolShapeResponse
	ProtocolShapeInvalid
	ProtocolClassAdmission
	ProtocolClassCatalog
	ProtocolPendingInitialize
	ProtocolPendingPolicy
	ProtocolPendingConfig
	ProtocolPendingCatalog
	ProtocolPendingNone
	ProtocolPendingOther
	CatalogReasonPin
	CatalogReasonBackendCount
	CatalogReasonEmptyList
	CatalogReasonJSON
	CatalogReasonModelSchema
	CatalogReasonEffortNull
	CatalogReasonEffortUnknown
	CatalogReasonDuplicateModel
	CatalogReasonDuplicateEffort
	CatalogReasonDefaultUnsupported
	CatalogReasonNoVisibleDefault
	CatalogReasonSnapshot
	CatalogReasonSettingsStore
	CatalogReasonRouter
	CatalogReasonDefaultMismatch
	CatalogReasonPolicySchema
	CatalogReasonPolicySize
	CatalogReasonPolicyMismatch
	CatalogReasonPolicyNonNull
)

var names = [...]string{
	HostListening: "host-listening", HostProbeAdmitted: "host-probe-admitted",
	HostProbeAcknowledged: "host-probe-acknowledged", HostJSONLAdmitted: "host-jsonl-admitted",
	AdapterEntered: "adapter-entered", StartupProbeRequested: "startup-probe-requested",
	JSONLRequested: "jsonl-requested", ProfileAuthenticated: "profile-authenticated",
	EndpointAllocated: "endpoint-allocated", ProbeResultValidated: "probe-result-validated",
	ProbeOutputWritten: "probe-output-written", ProbeFailed: "probe-failed", JSONLFailed: "jsonl-failed",
	PrimaryStarted: "primary-started", SecondaryStarted: "secondary-started",
	InitializeReceived: "initialize-received", PrimaryInitializeWritten: "primary-initialize-written",
	SecondaryInitializeWritten: "secondary-initialize-written", PrimaryInitializeReply: "primary-initialize-reply",
	SecondaryInitializeReply: "secondary-initialize-reply", PrimaryInitializeError: "primary-initialize-error",
	SecondaryInitializeError: "secondary-initialize-error", InitializationRejected: "initialization-rejected",
	InitializationReady: "initialization-ready", InitializeOutputWritten: "initialize-output-written",
	ProtocolFailed: "protocol-failed", StreamWriteFailed: "stream-write-failed",
	ProbeEnvironmentAdmitted: "probe-environment-admitted", ProbeEnvironmentRejected: "probe-environment-rejected",
	PrimaryExited: "primary-exited", BridgeContextCancelled: "bridge-context-cancelled",
	ClientStreamEnded: "client-stream-ended", PrimaryStreamEnded: "primary-stream-ended",
	SecondaryStreamEnded:            "secondary-stream-ended",
	ProtocolSourceClient:            "protocol-source-client",
	ProtocolSourcePrimary:           "protocol-source-primary",
	ProtocolSourceSecondary:         "protocol-source-secondary",
	ProtocolClassEnvelope:           "protocol-class-envelope",
	ProtocolClassCorrelation:        "protocol-class-correlation",
	ProtocolClassUnscopedRequest:    "protocol-class-unscoped-request",
	ProtocolClassThreadOwnership:    "protocol-class-thread-ownership",
	ProtocolClassInitialization:     "protocol-class-initialization",
	ProtocolClassClientNotification: "protocol-class-client-notification",
	ProtocolClassOther:              "protocol-class-other",
	ProtocolShapeNotification:       "protocol-shape-notification",
	ProtocolShapeRequest:            "protocol-shape-request",
	ProtocolShapeResponse:           "protocol-shape-response",
	ProtocolShapeInvalid:            "protocol-shape-invalid",
	ProtocolClassAdmission:          "protocol-class-admission",
	ProtocolClassCatalog:            "protocol-class-catalog",
	ProtocolPendingInitialize:       "protocol-pending-initialize",
	ProtocolPendingPolicy:           "protocol-pending-policy",
	ProtocolPendingConfig:           "protocol-pending-config",
	ProtocolPendingCatalog:          "protocol-pending-catalog",
	ProtocolPendingNone:             "protocol-pending-none",
	ProtocolPendingOther:            "protocol-pending-other",
	CatalogReasonPin: "catalog-reason-pin",
	CatalogReasonBackendCount: "catalog-reason-backend-count",
	CatalogReasonEmptyList: "catalog-reason-empty-list",
	CatalogReasonJSON: "catalog-reason-json",
	CatalogReasonModelSchema: "catalog-reason-model-schema",
	CatalogReasonEffortNull: "catalog-reason-effort-null",
	CatalogReasonEffortUnknown: "catalog-reason-effort-unknown",
	CatalogReasonDuplicateModel: "catalog-reason-duplicate-model",
	CatalogReasonDuplicateEffort: "catalog-reason-duplicate-effort",
	CatalogReasonDefaultUnsupported: "catalog-reason-default-unsupported",
	CatalogReasonNoVisibleDefault: "catalog-reason-no-visible-default",
	CatalogReasonSnapshot: "catalog-reason-snapshot",
	CatalogReasonSettingsStore: "catalog-reason-settings-store",
	CatalogReasonRouter: "catalog-reason-router",
	CatalogReasonDefaultMismatch: "catalog-reason-default-mismatch",
	CatalogReasonPolicySchema: "catalog-reason-policy-schema",
	CatalogReasonPolicySize: "catalog-reason-policy-size",
	CatalogReasonPolicyMismatch: "catalog-reason-policy-mismatch",
	CatalogReasonPolicyNonNull: "catalog-reason-policy-nonnull",
}

func (s Stage) String() string {
	if s == 0 || int(s) >= len(names) {
		return ""
	}
	return names[s]
}

type key struct{}
type sink func(Stage)

// WithObserver supplies an in-memory test observer. It does not change context
// cancellation, grant access or enable a process/file observer in a normal build.
// The callback must return promptly and be concurrency-safe. Panics are ignored.
func WithObserver(ctx context.Context, observe func(Stage)) context.Context {
	return context.WithValue(ctx, key{}, sink(observe))
}

func Enabled(ctx context.Context) bool {
	if ctx != nil {
		if s, ok := ctx.Value(key{}).(sink); ok {
			return s != nil
		}
	}
	return processEnabled()
}

func Emit(ctx context.Context, stage Stage) {
	if stage.String() == "" {
		return
	}
	defer func() { _ = recover() }()
	if ctx != nil {
		if s, ok := ctx.Value(key{}).(sink); ok {
			if s != nil {
				s(stage)
			}
			return
		}
	}
	processEmit(stage)
}
