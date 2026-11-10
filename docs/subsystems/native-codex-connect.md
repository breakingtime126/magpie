# Original Codex Connect transaction

This is a partial integration, not a working same-window release. The original
Agents Connect/Disconnect endpoint now has a nativecodex/local-Codex-only seam.
Legacy ConnectHow, Sync, history and provider guards remain intact. Other agents
and ordinary builds keep their old paths. The separate account/test sheet is
no longer constructed or advertised by the nativecodex desktop host.

## Supplier preparation and current product stop

The original desktop host now constructs a production `nativesupplier.NewFactory`
with its actual gateway server and serialized lifecycle manager. The factory
connects runtime discovery, owned-listener verification, the shared-native host,
bootstrap channel and immutable prepared profile to the existing Connect action.
There is no separate product UI or JSON setup form.

The production supplier source still deliberately returns a missing-metadata
error. It has no independently validated, current full native catalog. This
happens before runtime probing, files, locks, journals or launch-variable writes.
A historical tool-call success, a declared context window, illustrative example,
model/list projection or self-signed JSON file is not that catalog. No catalog
builder or native model cache/bundle access is introduced by this integration.

An authorized source adapter must provide the exact supported native version's
catalog with independently checked schema and capability semantics, attributable
source and observation/expiry times, and a current gateway-qualified supplier
route generation. It must revoke a captured generation when the route or
capabilities change. The package-private source boundary cannot be supplied by
an HTTP request, environment variable or arbitrary external catalog path.

Given a validated synthetic source, preparation discovers and probes the exact
supported official runtime, checks native-home separation and the bundled
adapter's compile-time SHA-256/source-revision pin, verifies the actual owned
gateway connection, freezes and hashes supplier bytes, then creates a disposable
generation directory. The host retains an authenticated local bootstrap channel,
endpoint lease and child lifetime. Admission rechecks supplier validity and pins
before launch handoff and after transfer. Expiry or source revocation cancels the
generation independently of another Connect request. A hash proves byte identity,
not model capabilities or supplier policy.

The Windows packager builds the adapter first and embeds its digest and exact
source revision in the original GUI. It does not bundle, install or update the
official runtime. Shared-native discovery remains pinned to 0.167.0-alpha.3.2.

The local-placeholder caller label is injected only into the secondary child. It
is public, not a secret or endpoint authentication. No supplier key is written to
the profile, journal or user environment. Existing gateway and native policy
checks remain necessary. These checks are not protection against a hostile
same-user process or a substitute for an authorized provider metadata source.

## Persistence and recovery

Only after successful preparation and Config.Validate does the transaction create
a new immutable bridge profile below the separate Magpie connection directory.
It records both prior user-environment values in an exclusive, synced journal:
CODEX_CLI_PATH and MAGPIE_APP_SERVER_BRIDGE_CONFIG. Absence, empty string, REG_SZ
and REG_EXPAND_SZ are preserved. This supports process-interruption recovery,
not a verified machine-power-loss transaction. The desired values are only absolute paths.
An existing journal blocks another Connect, preserving the original values.

The Windows store compares the pair, rechecks each value before its write, flushes
the registry, and broadcasts WM_SETTINGCHANGE with a bounded call. It does not
change the current process environment or restart Explorer/Desktop. A partial
write, broadcast failure, cancellation or failed verification leaves the journal
and reports uncertainty. There is no automatic retry or implicit rollback.

Disconnect accepts each current value only if it equals the journal's exact
original or installed value (including type), allowing interrupted transactions
to recover. Any third value stops the whole restore before the first write.
After the original pair is verified, the journal is removed; the own profile is
retained when it still exists; a missing or damaged profile does not block restoring
the journal-matching selection. No native directory, authentication state or history is removed.
The OS file lock serializes cooperating Magpie controllers and fails closed if
not acquired. Windows registry values have no compare-and-swap operation:
an unrelated writer can race between checks and writes. This is not an OS
security boundary or a guarantee against a hostile same-user filesystem actor.

## Result semantics and verification

SelectionConfigured means only that both values read back as written.
BackendHandshakeVerified and ChatVerified remain false. The API does not report
old Connected success or invoke agent.Notice/Rewired. A running Codex window may
retain its earlier environment. Prior temporary-VM evidence established that
UserEnv + notification + normal AUMID activation can select the bridge image;
it did not prove successful handshake/chat or restored-start acceptance.

Synthetic tests cover missing-input zero-write, validated fixture transaction,
original value/type restoration, duplicate Connect, fresh-controller partial-write
recovery and external-change conflict. Windows registry mutation, real native
launch, credentials, network services and user devices are not exercised.
Run go test with nogui and nogui,nativecodex for internal/nativeconnect,
internal/nativesupplier, the native host/channel/endpoint/profile/launch packages
and the focused GUI seam tests. Supplier tests use an authored non-model JSON
fixture and inert executable paths; they do not prove native catalog parsing,
real-service capability or Desktop acceptance. Windows CI also checks packaging
pins and captures the original GUI in a synthetic home. A successful build is
not a successful real Connect or chat.
