# Original Codex Connect transaction

This is a partial integration, not a working same-window release. The original
Agents Connect/Disconnect endpoint now has a nativecodex/local-Codex-only seam.
Legacy ConnectHow, Sync, history and provider guards remain intact. Other agents
and ordinary builds keep their old paths. The separate account/test sheet is
no longer constructed or advertised by the nativecodex desktop host.

## Configured supplier preparation

The original desktop host constructs `nativesupplier.NewFactory` with its actual
gateway server and serialized lifecycle manager. It uses the user's existing,
enabled ordinary AIHub provider whose normalized HTTPS base matches
`https://aihub.friendtimes.net/v1`. It never creates a provider or supplies a key.
There is no separate UI or JSON setup form. Explicit saved model picks work even
without a fetched model list. Otherwise, use the existing provider editor's
Fetch models or model selection controls. The current narrow path supports a
direct ordinary provider, not account/plugin/group or cross-provider fallback
routes. Other providers and native GPT choices retain their original paths.
Every configured protocol endpoint must match AIHub unless the existing model
API setting explicitly pins that local model to an AIHub protocol. A fetched
list's API declarations do not constrain the gateway's refusal fallback. A
mixed-endpoint provider without that pin may currently show the general
missing-model guidance; selecting more models alone does not repair its route.

`provider.ConfiguredSupplierModels` reads only ordinary Magpie provider/settings
and selected saved supplier-list files. It returns an allowlisted metadata DTO
and opaque semantic revision, never keys, headers, arbitrary JSON or a native
model record. It does not enumerate native accounts, read native catalogs or
borrow inferred capabilities from another model family. The source accepts
unknown optional capabilities; it does not demand a complete vendor-native
catalog. Malformed/unreadable sources still fail before launch-selection writes.
Known GPT/Codex/O-series vendor IDs, including configured aliases, are not added
to the AIHub catalog; they remain native-primary choices.

`nativecatalog.Render` creates a small, independently authored native client
profile from the public pinned [ModelInfo/ModelsResponse schema](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/protocol/src/openai_models.rs)
and [file loader](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core/src/config/mod.rs).
The loader requires identity/display and client-policy fields plus an instruction
template, not complete supplier capacity data. Instructions are authored for
Magpie; no native prompt or catalog record is copied.

- Facts: qualified current route, label, and explicitly configured/declared
  context and reasoning choices. Historical tool success or context declarations
  are not treated as current live capability evidence.
  A wire rename uses the effective upstream target's saved declarations, not the
  old model's. An unlisted target stays unknown; local user overrides remain.
- Client defaults: text-only input, direct standard JSON function shell tools,
  no freeform patch/search/REPL/Responses Lite selection, no requested reasoning
  summary or verbosity. No guardian, sandbox or approval-policy override is added.
- Budgets: configured context takes precedence. Unknown context gets an 8192
  working budget and 6144 compaction threshold; tool output truncation is 2048
  tokens. These are Magpie operating choices, not claims of vendor capacity, and
  native configuration overrides still apply. An unknown smaller supplier limit
  can still reject a request. The native schema has no maximum-output field.

These catalog choices are not a complete outgoing-request filter. Native can
send explicit reasoning and parallel tool preferences, and configured MCP or
other tools are separate from the catalog. Magpie's existing protocol translation
remains responsible for the configured upstream. Real supplier compatibility
and tool execution are not established merely by generating this profile.

Preparation discovers and probes the supported official runtime, checks native
home separation and the adapter's compile-time SHA-256/source-revision pin,
verifies the actual owned gateway connection, freezes/hashes catalog bytes, and
creates a disposable generation directory. The host retains its authenticated
bootstrap channel, endpoint lease and child lifetime. Admission checks the
current semantic source revision and pins before launch transfer and afterward.
Each subsequent native HTTP request rechecks admission before entering the
captured gateway handler, returning a fixed error if the source changed.
A cancellable local-source watcher also revokes a running generation on a route
or model change; unrelated auth/theme changes do not deliberately invalidate an
unchanged metadata projection. No remote service is polled. Explicit local
configuration is valid until changed, without an artificial evidence-expiry
requirement. Revocation does not silently restore registry selection: Disconnect
still performs the journaled restoration before a fresh Connect.

The Windows packager embeds the same-source adapter digest and revision in the
original GUI. It does not install/update the official runtime; shared-native
discovery remains pinned to 0.167.0-alpha.3.2. The local-placeholder caller label
is public and injected only into the secondary child. No supplier key is written
to the profile, journal or user environment. Existing gateway and native policy
checks remain necessary. This is not protection against a hostile same-user
process or proof that an upstream accepts the requested capabilities. Endpoint
selection binds the configured source; it is not a new network confinement
boundary. Existing gateway HTTP redirects, middleware and routing policy still
apply, and source checks do not make concurrent same-user edits atomic.

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
and the focused GUI seam tests. Supplier tests cover both inert transaction fixtures and actual configured-source
to generated-catalog wiring. The separate opt-in official-runtime supplier text
test loads the production renderer output in a fresh home against loopback. It
is parser/text acceptance, not real-service, native tool-execution or Desktop
acceptance. Windows CI also checks packaging
pins and captures the original GUI in a synthetic home. A successful build is
not a successful real Connect or chat.

## Original Agents control and recovery status

The original owning desktop's boot data enables the native Codex control;
ordinary builds, web windows and foreign transport requests do not get it.
The existing `/api/state` (including `?last=1`) reads `Manager.Status` afresh
with a bounded timeout. This host-specific status is not persisted in the
shared last-state cache. Inspection failure is explicitly unknown, never an
empty/disconnected selection. Connect/Disconnect errors also return current
selection evidence without claiming backend or chat success.

The original Agents Codex row uses this evidence rather than legacy `wired`.
Its switch configures launch selection or opens the journal-restoration
confirmation. It does not use the legacy config preview, model picker or
connected toast. Journal, recovery and unknown states keep restoration
reachable even when the row was hidden or runtime discovery no longer returns
Codex. Disconnect dispatch does not require rediscovering an installed runtime
or preparing a supplier. A missing/damaged journal or external value conflict
still fails closed in the transaction; exposing recovery does not bypass it.
An HTTP failure refreshes status; if that read fails, recovery stays available
as unknown. Unrelated state responses do not erase the host status. In-flight
reads from before a selection change cannot replace its result.

`codex-connect-recovery.test.cjs` exercises the original page in both browser
engines and all five locales, including reload, Cancel, a lost response after a
partial write, disappearance of the runtime, conflict, and explicit recovery.
Its host API and journal state are synthetic, not real Windows or chat evidence.

## Authenticated one-shot startup inspection

The trusted envelope now has a separate, opt-in startup-inspection operation.
It is enabled only when the owner configuration selects the exact
`compare-unrestricted-v0.167.0-alpha.3.2` probe contract. The native supplier
factory now explicitly selects it for original Codex Connect, together with the
exact-version null-only requirements comparison, genuine matching feature-list
comparison, primary trusted-stdio auth status and reviewed bundled-plugin leaf
permission. These modes remain default-off for other configurations. This wiring
alone does not establish normal Desktop startup or same-window use acceptance.

The [`nativehost`](../../internal/nativehost/probe.go) reserves an independent
one-use bootstrap pipe for inspection, with the same immutable profile digest,
packaged-peer verification and source-admission callback as JSONL launch.
Inspection does not consume the normal launch lease. Direct JSONL without an
inspection remains possible. At most one inspection is accepted per generation;
repeated, concurrent and post-launch inspection is unsupported and fails closed.
A failed or uncertain inspection is not cached as successful configuration.

[`nativelaunch.ProbeStartup`](../../internal/nativelaunch/probe.go) authenticates
the exact profile bytes before filesystem-aware configuration validation.
It obtains a distinct retained endpoint and the generation's independently
trusted channel reference. [`ProbeStartupWithNativeEndpoint`](../../internal/appserverbridge/native_probe.go)
passes the reviewed route overrides to both genuine one-shot commands. It
checks both native executable pins before either command, compares the complete
strict native responses, and checks configuration, catalog pins and executable
pins again afterward. The public response remains the existing pinned native
version and unrestricted/null-requirements subset; no company policy, auth,
capability, native success or synthetic null requirements are fabricated.

On Windows both probes use owned processes with retained Job drainage. The
primary's [`production runner`](../../internal/appserverbridge/native_probe_process_windows.go)
keeps the same Job under cleanup ownership if drainage is uncertain; the failed
operation returns no successful output. The secondary inherits its own original
listener through `nativeendpoint.Start`. Successful drainage alone is not a
successful probe: root exit status must also be present and successful.
`WaitForExit` observes the existing owned wait rather than calling `Cmd.Wait`
again. Closing stdout is not completion. Unknown exit, nonzero exit, cancellation,
unproven descendants or failed cleanup suppress successful output.

Before releasing the validated result, the adapter uses the probe-only
[`FinishProbe`/`ValidateProbe` acknowledgement](../../internal/nativebootstrap/probe_windows.go).
The live owner rechecks source admission, packaged launcher and gateway lifetime;
reciprocal retained-peer checks cover completion. The probe lease remains alive
through the client's final check, independently of a subsequent JSONL launch.
Normal bootstrap's unexpected-byte rejection is unchanged. The plain experimental
configuration path and its portable comparison probe are unchanged.

Authored fixtures cover envelope authentication, operation separation, completion
failure, source/launcher revocation, direct JSONL, acknowledgement/launch races,
real synthetic child nonzero exits, closed stdout, and descendant drainage.
Portable fixture results and Windows cross-compilation are not Windows runtime
acceptance. Windows synthetic tests and a later authorized actual Desktop run
must be reported separately. No real native model record, cache, credential or
provider turn is required for these source-level checks.


## Factory-generated Desktop contracts and test boundaries

[`nativesupplier.newFactory`](../../internal/nativesupplier/factory.go) chooses
all five reviewed 0.167.0-alpha.3.2 contracts in the immutable generation profile.
It retains both executable pins and the shared-native primary route; it does not
select unavailable-attestation or no-auth diagnostic modes. Requirements with
non-null policy still fail, and feature pages must genuinely match. Primary
trusted-stdio status preserves requested token/refresh flags and validates the
native result. This is intentional credential relay to the trusted Desktop
frontend when later used with real accounts, not an authentication bypass or
proof of credential isolation. Source tests use only explicitly synthetic values.

Plugin permission does not inject an override. Absent, exact `enabled=true`,
and exact `enabled=false` remain distinct; only a requested reviewed leaf is
forwarded unchanged to both native children. Mixed/duplicate literals, arbitrary
plugin paths and plugin leaves in one-shot startup inspection remain rejected.
Native requirements, installed-plugin behavior and required-human-approval
refusal are not rewritten to obtain a successful startup.

[`desktop_contract_test.go`](../../internal/nativesupplier/desktop_contract_test.go)
uses the actual factory and configured supplier catalog builder to create its
immutable PreparedProfile, then decodes those exact bytes into the real process
handlers. It exercises authored subprocess startup and initialized request
sequences, policy/feature/auth refusal, explicit plugin values, native/catalog
pin changes and primary-only synthetic token/proof routing. The secondary helper
reads the actual generated catalog path and derives its model identifiers from
that authored file; its remaining protocol fields are synthetic, not evidence
that the official native loader accepted the catalog. Runtime discovery
and host transport ownership are explicit fixture boundaries in these portable
tests. Decoding this profile and calling the handlers is **not** execution of
the Windows authenticated bootstrap chain.

The separate bootstrap/host/endpoint tests exercise operation authentication,
probe completion acknowledgement and the subsequent JSONL lease. Their Windows
synthetic subprocess tests must execute in CI, not merely cross-compile. A whole
factory → packaged host → authenticated probe → JSONL → actual Desktop run is a
further integration boundary; source tests do not claim that it passed. No real
account, credential refresh, provider request or native model cache is needed to
run the authored fixtures.
