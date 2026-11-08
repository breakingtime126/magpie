# Explicit disposable unelevated setup slice

`desktop-fixtures -sandbox-setup-profile unelevated-read-only-4239` is an
additional, default-off **test-harness** selection. It requires
`-fixture-profile bridge-owned-catalog-4239 -reviewed-build desktop-4239` and
no external catalog. Ordinary fixtures and the production bridge do not gain a
setup RPC. An operator must specifically authorize this security-mode selection
for the disposable VM before running this profile. A commit, CI success or this
flag is not standing permission to configure arbitrary machines.

The existing empty scratch directory, exact native executable digest, native
version check, and explicit child environment remain prerequisites. Each new
native home receives the same fully authored loopback fixture configuration,
except `sandbox_mode="read-only"` is replaced with the official builtin
`default_permissions=":read-only"`. No credentials, hooks, external model
service, plugin installation or MCP configuration are introduced.

Before the first setup attempt, **both** native peers must initialize in their
known separate homes and provide:

* genuine `configRequirements/read` with `requirements:null`;
* genuine `config/read` for the launch workspace, including layers: exactly one
  user layer for that home's generated file, no profile selection, and its full
  semantic content equal to the authored TOML. Only empty system/sessionFlags
  layers are additionally accepted; disabled, managed, project, nonempty or
  unknown layers fail closed;
* genuine `thread/start {ephemeral:true}` without cwd, model, permission,
  sandbox or config overrides. Its effective result must identify the expected
  raw model/provider, workspace, `approvalPolicy:"never"`, builtin profile
  `{id:":read-only",extends:null}`, and
  `sandbox:{type:"readOnly",networkAccess:false}`.

`runtimeWorkspaceRoots` need not be empty: workspace identity is not write
permission. The source-bound proof concerns this exact builtin profile, not a
general inference from an arbitrary legacy sandbox projection. The pinned
builtin compiles to `PermissionProfile::read_only()`. The reviewed legacy
projection does not silently project an unrepresentable writable profile to
ReadOnly; its conservative fallback is WorkspaceWrite. Thread creation may
create ordinary native state and initialize subsystems. It is not a no-write
or zero-egress operation, and no inference is requested by these probes.

Immediately before **each** setting request, the harness rechecks the original
file digest, full user-layer content/version, and genuine null requirements.
It calls only `windowsSandbox/setupStart {mode:"unelevated"}`, omitting cwd.
The pinned handler reloads configuration and then makes workspace roots empty.
For the proven builtin read-only effective profile, the legacy preflight exits
without writable-root ACL work. It does not enter the elevated account,
service, firewall or Registered Core provisioning branches. Official setup
persists `[windows].sandbox="unelevated"` and clears legacy sandbox keys (the
fixture did not contain those keys). Normal native state/debug writes remain
possible. This is not elevated-mode validation.

Requests are sequential and are not retried. `started:true` is only acceptance;
the same connection must emit genuine
`windowsSandbox/setupCompleted {mode:"unelevated",success:true,error:null}`.
The updated TOML must semantically equal the original plus the one windows
sandbox selection. Failure or timeout after an attempt remains potentially
partial; there is no fabricated rollback or automatic fallback mode.

Because native readiness holds its startup configuration in this version,
both setup peers must exit cleanly, then new peers actually reload each home.
The new peers repeat policy/layer/effective-thread checks and must return
`windowsSandbox/readiness {status:"ready"}` before clean shutdown. Only then
is fixture ready emitted. This proves setup/reload of these two native test
homes, not Desktop restart, tool execution or complete process isolation.

The ready and final audit `sandbox_setup` object contains only fixed labels,
counts and booleans. Successful readiness requires two preflights, attempts,
started replies, completions and postverifications, four clean peer exits,
`stage:"complete"`, `partial_state_possible:false`, and `no_retry:true`.
Only this explicit profile has a 360-second **post-ready** fixture lifetime;
existing profiles retain 180 seconds. The harness leaves scratch retention and
Desktop process-family cleanup to the existing external owner.

## Evidence and limits

Public reference source is OpenAI Codex commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`, corresponding to the reviewed runtime
version declaration `0.167.0-alpha.3.2`; digest pinning does not constitute a
reproducible-build proof:

* `app-server/src/request_processors/windows_sandbox_processor.rs`: setup
  reload, omitted-cwd workspace handling, notifications, readiness startup view.
* `core/src/config/permissions.rs` and `protocol/src/models.rs`: builtin
  `:read-only` resolution and actual permission profile.
* `core/src/sandboxing/manager.rs`: legacy policy projection/fallback.
* `app-server/src/config_manager_service.rs` and
  `app-server/src/config_layer.rs`: merged config and raw layer reporting.
* `windows-sandbox-rs/src/lib.rs`: unelevated no-writable-root early exit.

Checks occur in a controlled VM before Desktop starts. They are not an OS
security boundary against another process with the same user's privileges,
path substitution, or a concurrent privileged configuration writer. The
production bridge still rejects setup requests. Later Desktop overrides must
be separately constrained to this same read-only test scope; this preparatory
slice alone does not prove future turns retain that scope.

Synthetic unit tests exercise ordering, strict admission, unknown/changed
layers, file changes, failed/uncertain requests, genuine completion versus
acceptance, post-restart readiness, clean shutdown and truthful audit fields.
No successful compile, gofmt, unit test or native setup run is implied by the
source addition; record their actual CI results separately.
