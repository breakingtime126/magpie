# Disposable Desktop live welcome integration candidate

Behavior change: only the policy-trace workflow explicitly enables the new
EnableReviewedLiveWelcome switch. Other workflows and defaults are unchanged.
The existing hosted repository/branch, fresh install, signed package, native
version/hash, alias, session, startup compare-unrestricted, requirements:null,
no-auth, feature/config evidence and independent native turn/list gates remain.

The test-only PowerShell module supervises two held child processes. Fixture
readiness has a 25-second read bound and a 30-second external setup deadline;
a valid ready line starts an external 180-second timer. Output memory is bounded
(16 KiB for the fixture, 8 KiB for welcome, stderr discarded with the same bound).
The installed Windows PowerShell 5.1 executable runs the welcome helper in the
same session with a cleared explicit OS environment and 30-second external
wall-clock deadline. Timeout or malformed output is uncertain partial action,
never retried. Only held helper processes are killed by these supervisors.

Private bridge.json is read with a 16 KiB byte limit, duplicate keys rejected,
exact schema checked, then known child paths/providers/commands reconstructed.
The helper-created native configs are retained unchanged. The existing reviewed
bridge settings builder adds exact startup/launch profiles, working_directory,
and the synthetic primary provider display name. The real bridge --check and
all genuine native startup/policy checks still run; rejected settings fail.

The first screenshot precedes the one welcome attempt. The second capture uses
only the same HWND and activation process after rechecking held process identity
and package, including after a stopped UIA result. No later-page action occurs.
The fixed twelve-field helper result includes only enums, counts, booleans and
narrow target observations, never raw UI text or pattern probes. It is not
full onboarding, composer or model evidence. A stopped/unavailable/unchanged
welcome cannot produce an overall success, and cannot overwrite a prior
Desktop or independent native failure.

Fixture stdin stays open until activation cleanup. On verified activation exit,
EOF requests final fixture audit; otherwise the held helper is killed and the
live slice cannot succeed. Its final count-only audit is stopped/failed, never
conversation passed. Zero requests may be a valid idle fixture. Private scratch
is intentionally retained because activation exit does not prove descendants
have exited. No private scratch, config, request body/header, stderr or raw UIA
text is uploaded. Artifact paths are an explicit list of existing public static
metadata, report and before/after PNGs.

Verification: source inspection only here. PowerShell, Windows/UIA, helper
compilation/runtime fixtures and actual signed Desktop interaction must run at
the final exact CI head. Tests added for opt-in combinations, readiness pins,
duplicate metadata, count bounds, strict internal configuration, partial-action
and failure precedence, target label redaction, plus actual offline timeout and
stdout-bound child processes. Run build/desktop-live-welcome.tests.ps1 in pwsh
and build/desktop-welcome-step.tests.ps1 in Windows PowerShell 5.1. Existing
baseline and profile gates remain in the workflow.

The visible HWND must still belong to the held activation process. A UIA node
may belong to an Electron descendant only after a bounded, retained-handle
parent-chain proof to that activation, with exact executable, registered package,
session and creation identities at every hop. The helper rejects zero, unknown,
exited or foreign owners, caps the cache at 32 processes and ancestry at eight
links, and revalidates current target ownership before each action. It never
uses executable names or package membership alone as authority.

Windows PS5.1 offline tests also compile the exact constant native helper
definition and exercise one read-only bounded Toolhelp process snapshot, without
Desktop activation or UI methods. Passing that check is not UI ownership or
conversation acceptance. The separate zero-context real helper launch smoke
continues to check argv, environment and fixed result parsing before any UI.

Before its first action only, the helper may reread a strictly shell-only owned
UIA snapshot: exactly one approved window, approved window-chrome buttons and
empty noninteractive containers. There are at most four snapshots, with optional
rereads scheduled within a 3-second scheduling window. This does not bound an
in-flight snapshot; individual OS calls still rely on the unchanged 20-second
internal and 30-second external limits. Unknown/protected content and ownership/provider errors stop immediately;
no later-page read or action is retried. Exhaustion reports the fixed reason
accessibility_not_ready at welcome_read, with zero actions, all targets absent
and no completed action state. This is
missing accessibility readiness, not proof that the visible welcome content is
unknown or that onboarding succeeded. The parent keeps it a failed live slice.

The nullable `initialReadObservation` field explains a classified initial stop
once, from its last already-read owned ControlView snapshot. It records the
existing readiness snapshot count (one through four), exact and whitespace-only
normalized heading counts, and exact target-label counts split into interactive
and noninteractive nodes. It also records the first existing shell predicate
rejection: `node_count`, `unexpected_interactive`, `named_content`,
`container_type`, or `window_count`; `none` means the shell predicate accepted
the snapshot, not that welcome actions are allowed. It contains no raw names,
types, paths, application data or pattern probes. Before a classified snapshot,
after an accepted initial welcome, and for later reads the field remains null.
The strict parent schema requires the non-null summary to describe a stopped
`welcome_read` result with zero actions and no completed action state, checks
count consistency with existing target observations, and retains failure.

This adds no snapshot, UI action, retry, timeout or classification exception.
Whitespace-normalized evidence never satisfies the exact heading action gate.
The observation can distinguish a rejected shell shape from missing labels; it
does not diagnose why a provider omitted content or establish a next screen.
Stop this diagnostic lane after one capture unless its fixed facts identify a
specific harmless mismatch for separate review. An unexplained/inaccessible
tree remains blocked, with no coordinate input, fabricated onboarding state,
broadened allowlist, accessibility-enabling setting, or longer blind polling.
