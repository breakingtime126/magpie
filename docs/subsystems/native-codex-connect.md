# Original Codex Connect transaction

This is a partial integration, not a working same-window release. The original
Agents Connect/Disconnect endpoint now has a nativecodex/local-Codex-only seam.
Legacy ConnectHow, Sync, history and provider guards remain intact. Other agents
and ordinary builds keep their old paths. The separate account/test sheet is
no longer constructed or advertised by the nativecodex desktop host.

## Current product stop

The host has no supplier catalog preparer yet. Connect returns a missing verified
AIHub catalog error before creating a lock, profile, journal or changing either
launch variable. No backend JSON form replaces the missing preparation.

A trusted in-process Prepare callback must discover the installed supported
official executable/version and existing native home, check its provenance and
pin, validate authorized supplier metadata, and prove that this Magpie owns the
intended local listener. It then supplies a shared-native profile and the bundled
adapter. Config.Validate is reused but is **not** runtime/provenance/catalog
semantic verification. The test callback uses inert fixtures only.

The Windows packager includes a same-source stdio adapter as an internal component.
Do not enable a real Prepare callback until official-runtime discovery, listener
ownership and the metadata source are wired. This patch does not run a catalog builder or access native
model caches/bundles. A model/list projection is not a full catalog.

The separately reviewed local-placeholder gateway mode uses the original public
caller label magpie-codex, injected only into the secondary child. It is not a
secret or endpoint authentication. No key is written to the profile, journal or
user environment. Listener ownership and existing gateway policy still matter.

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
Run go test with nogui and nogui,nativecodex for internal/nativeconnect and the
focused GUI seam tests. Local Go/gofmt are unavailable; CI remains required.
