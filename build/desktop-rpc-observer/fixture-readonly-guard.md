# Explicit read-only disposable fixture guard

`MAGPIE_CI_READONLY_GUARD` is absent by default. The only accepted value is a
JSON object with exactly `profile: "unelevated-read-only-4239"` and `workspace`
set to the harness-created absolute Windows workspace. This declaration is not
proof that setup succeeded. The separate native preflight/setup must establish
that both generated native homes select the reviewed built-in `:read-only`
profile and official unelevated mode. The observer removes its guard variable
from the bridge child environment. It does not change native settings or run
setup, and it is not an OS security boundary.

Guarded execution is **not** the usual byte-transparent observer: a complete
JSON line (maximum 64 KiB including newline) is checked before forwarding its
unchanged bytes. A rejection closes the experiment's strict process tree,
records a fixed `fixture_guard_rejected` label, and returns failure. It never
rewrites permissions or generates a successful reply. Ordinary mode retains
its existing pump and trace budget.

Before launching the bridge, this mode rejects any presence of
`CODEX_WINDOWS_REGISTERED_CORE`. It permits only the observed `app-server`,
`--analytics-default-enabled`, exact `features.code_mode_host=true`, and the
reviewed bundled-plugin enable literal. The one-shot startup probe additionally
requires `--print-startup-config` paired with the bounded Desktop client info.
That explicitly recognized probe uses the existing pass-through pump and native
startup policy validator; its non-RPC output is not claimed as a conversation
permission check. Unknown flags, sign-out, `prefer_mxc`, and sandbox overrides
are rejected rather than changed.

Only thread start/resume and turn start/steer have a conversation admission
path. Non-null configuration, other named permission profiles, extra capability
roots, environments, dynamic tools, tool outputs, collaboration settings,
instructions, alternate cwd or unknown fields are rejected. Only the two
self-authored model aliases and plain `synthetic-alpha-*`/`synthetic-beta-*`
input are accepted. No images/files or ordinary user data are accepted.
Explicit permission selection may only choose `:read-only`; a legacy read-only
sandbox request is still checked against the actual response provenance.
Runtime workspace roots may be empty or the exact fixture workspace. They are
context roots, not writable grants. No other root is allowed.

A thread is not usable for a turn until its actual start/resume reply has:
- matching cwd and permitted runtime workspace context;
- approval policy `never`;
- exactly `sandbox: {type: "readOnly", networkAccess: false}`;
- exactly `activePermissionProfile: {id: ":read-only", extends: null}`;
- one known public model alias, matching an explicit requested alias;
- bridge logical provider `magpie_bridge` in the top-level and thread objects.

The fixed upstream `c40729ea5cf8e947481b130c59807273eda2f8e9` read-only built-in
and native sandbox projection are the basis of this evidence. It is not proof
for arbitrary profiles, old servers omitting provenance, or externally modified
homes. Native provider IDs are checked by the production bridge before their
logical projection; this observer does not pretend to see them afterward.
Resume must return the requested thread ID and is revalidated, not trusted from
an earlier connection. Thread settings notifications are also checked.
Other thread mutation paths (fork, compaction, settings updates, review etc.)
are not admitted. Read/history/list, unsubscribe and interrupt remain subject
to the pinned production router. Other global mutable RPCs remain unsupported
by that exact production bridge; the separately opted-in bridge-owned model/
effort file write is not a native permissions write. This is a narrow fixture
gate, not a reusable authorization proxy for future bridge APIs.

All backend-originated requests are rejected, including tool/approval requests.
Only user-message, agent-message and reasoning item events are admitted. This
is not retroactive prevention of a tool that has already begun: the loopback
fixture independently emits only its fixed text response and no tool calls.
No real credentials, provider authentication or external inference is covered.

`fixture_readonly_guard_started` identifies the active stream mode. When a public
plugin catalog is configured, its real digest event precedes this record; guard
construction and admission still occur before child startup and any forwarding.
The parent continues to reject records before the required provenance. A
`fixture_readonly_turn_completed` event has only
`fixtureReadOnly: {schema: 1, owner: "alpha"|"beta", completed, threadTurn}`.
It is emitted after both an admitted turn/start response supplies its turn ID
and a matching real completed notification has status `completed` and no error.
Early notifications are bounded and held, never counted alone; duplicate or
failed completions fail closed. IDs and text stay private in bounded memory.
The parent must intersect this evidence with actual fixture request/raw-model
validation and UI observations. A missing event or exhausted trace is not
success. No restart or history migration proof is implied.

Only active guard mode increases each exclusive trace slot to 128 KiB; the
existing 16-slot cap remains (2 MiB aggregate). Default/probe mode stays 64 KiB.
Pending requests, known threads, active turns, held completion notifications and
completion tombstones are each capped at 128. This is bounded memory, not a
new wall-clock deadline. The existing enclosing job/process deadlines remain
necessary.

Unit tests use only synthetic JSON/paths and in-memory pipes. They cover
permission overrides, real-reply admission, wrong requested owner, resume,
response/notification ordering, duplicate completions, unknown execution,
server requests, byte preservation, truncated/oversized/duplicate-key frames,
argument restrictions and environment stripping. Running them does not perform
Windows setup. Local Go compilation/gofmt/race has not been available; CI must
verify this candidate before any runtime acceptance claim.
