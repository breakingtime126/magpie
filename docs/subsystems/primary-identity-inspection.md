# Primary identity inspection (experimental 0.167.0-alpha.3.2 contract)

`magpie-codex-bridge --inspect-primary-identity-v0.167.0-alpha.3.2` is a distinct,
explicit native-operation opt-in. It does **not** change `--check`, normal bridge
launch or authentication defaults. The existing `MAGPIE_APP_SERVER_BRIDGE_CONFIG`
selects a minimal paired profile whose primary provider is `openai`, with a
reviewed primary `startup_probe_sha256`. Other compatibility modes, a shared
working directory, primary inherited environment and static primary models are
rejected. Both profiles' paths undergo ordinary Config.Validate checks, but only
the primary executable is hashed and launched; no secondary process is started.

The caller must trust the actual native executable and selected configuration.
A matching hash/provider name is not official provenance, remote destination
binding or an OS security boundary. Version scope is the operator-reviewed
executable pin and the explicit version-named command, not a cryptographic or
userAgent-based version attestation. This mode remains unverified against real
accounts and official-runtime initialization notifications.

The native process receives the existing explicit backend environment and cwd
of its primary home. It receives exactly initialize (a distinct Magpie client,
no extra capabilities), initialized, and getAuthStatus with includeToken:false
and refreshToken:false. No Router, catalog, account/read, login/logout, proof,
OAuth, model request or tool turn is initiated. Native initialization and status
can nevertheless refresh credentials, contact services and update native state;
false refresh is **not** a zero-network or zero-write guarantee. A failed or
cancelled operation does not roll back those native side effects. Never attach
this operation silently to Save and check, nor run it automatically or poll it.

Output is only schema=1, scope=native-reported-identity-only, authMethod (nullable)
and requiresOpenaiAuth. Null means the native server did not report an identity,
not proof that no account is configured: native token/refresh failure paths can
also return null. A non-null type does not establish token validity, quota,
chat readiness or Codex Desktop integration. Any non-null returned authToken,
source error, unknown fields, unsolicited request/notification, oversized JSON,
wrong ID, missing initialization field, abnormal EOF/exit or timeout fails closed.
Raw native stdout/stderr and errors are never logged or emitted. Temporary JSON
buffers are ordinary Go memory, not securely wiped storage.

The exchange uses a 15-second context, a 64-KiB JSON line limit, fixed IDs and
only one primary. This is not a hard wall-clock limit for the whole operation:
configuration/file operations and cleanup have their own blocking behavior.
EOF is offered before cleanup using the existing strict owned-tree implementation.
Cleanup waits 500 milliseconds, then requests Stop if needed and waits for the
parent; these final waits have no independent hard deadline. Taking that forced
stop path, or a parent exit error returned by Wait, prevents success. Not all
underlying kill/sweep failures are exposed by the proc API, so success is not
confirmation that every descendant was reaped or every cleanup error detected.

Wait also sweeps on a successful parent exit: on Windows, closing the
KILL_ON_JOB_CLOSE job requests termination of its descendants; on Unix, group
SIGKILL is sent and retried asynchronously, without waiting for every descendant.
Processes that escape that Unix process group (for example via setsid) are not
covered by a complete-tree guarantee. These are inherited proc boundaries, not
new OS isolation or verified descendant-exit guarantees.
There is no automatic retry or alternate backend. CLI errors are fixed
unavailable/timeout/cancelled classes; configuration failures retain the existing
redacted adapter diagnostics. CLI output is not a logged-in assertion.

Synthetic tests use only copied test executables, public inert canaries and
private temporary homes. They cover the exact primary-only sequence, negative
response shapes, optional-mode/pin admission, noninheritance, clean EOF,
cancellation/deadline, and unchanged --check behavior. No real native, account,
keyring, login, model service or Windows security setting is used. Local tooling
had no usable Go/gofmt runtime; static review/application checks are not execution
or real-authentication acceptance. GUI consent and presentation are separate work.

Source of semantics: fixed official c40729ea5cf8e947481b130c59807273eda2f8e9
[initialize/status types](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v1.rs)
and [getAuthStatus handler](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/account_processor.rs#L1097-L1168).
