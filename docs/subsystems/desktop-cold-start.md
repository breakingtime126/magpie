# Official Desktop cold-start CI

The experimental `Experimental Desktop cold start` workflow is a separate,
credential-free baseline on a fresh GitHub-hosted Windows 2025 VM. It never
installs or runs anything on a user's computer, changes `main`, publishes a
release, or imports account state.

It downloads the current x64 Store-signed package and offline license from the
[official Windows deployment page](https://learn.chatgpt.com/docs/enterprise/windows-deployment),
verifies trust with the Windows SDK signature tool, records SHA-256 and manifest
identity/version, and checks the existing runner's interactive desktop. The URL
is floating. The first run verified 26.1103.3517.0, SHA-256
`ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643`.
Subsequent runs stop if those bytes change. This is not a reproduction of
historical Desktop 26.1029.7803.0. The verified manifest has three applications;
the Desktop entry is selected by its exact `app/ChatGPT.exe` executable, and its
AUMID still comes from actual registration plus manifest application ID.

Only the documented licensed provisioning command is used. No unsigned-package
switch, license bypass, manifest change, certificate installation, security or
network policy change, new session, RDP, account login, or repackaged executable
is allowed. If provisioning does not register the application in the current
runner session, the probe stops and reports that limitation.

Activation uses the registered AUMID and Windows' application activation manager.
Process package identity, visible window geometry and a bounded screenshot are
separate evidence. Each launch is observed for 60 seconds without UI interaction. A screenshot can
be occluded and must be reviewed; a green
job means only that those baseline observations were obtained. It is not a
successful bridge connection, usable model picker, account login, model response,
or production acceptance.

The first phase deliberately does not set `CODEX_CLI_PATH`. Normal MSIX activation
has no per-launch environment-block parameter, so setting an environment variable
in PowerShell does not prove that Desktop receives it. The second job uses another fresh VM and the same SHA-pinned official package.
It launches only the newly registered `chatgpt-desktop.exe` alias declared under
that manifest's Desktop application. An identically named alias existing before
installation is a blocker. Directory-entry enumeration checks the documented
root alias location as well as the family-qualified location; the initial `Test-Path` check failed to discover the latter, while subsequent
directory enumeration found both. A separate fresh runner returned no entries
immediately after provisioning, so enumeration now waits approximately 60 seconds for
registration to materialize. It does not trigger activation, rewrite registration,
or retry access errors. Continued absence is a reported launcher blocker. The selected entry must have the documented
CreateProcess alias reparse tag `0x8000001B`. The probe never parses undocumented
APPEXECLINK payload strings or fabricates an alias. Actual runtime package identity
remains a separate mandatory check. Non-shell process
creation supplies a minimal, non-secret, process-local environment containing
the exact commit-built bridge path. It never writes user/system environment.

The pinned package contains both `app\\Codex.exe` and
`app\\resources\\codex.exe`. Only the latter resource backend is considered, and
its `--version` must identify it as `codex-cli` in a separate empty home with no
inherited authentication environment. This is an identity/version check, not a
model or authentication request.

The baseline/direct bridge configuration has two new empty homes and loopback discard endpoints,
no API key, and no startup, requirements, attestation or OAuth opt-ins. The separate
instrumented startup follow-up below selects only its explicitly reviewed profile. The exact production bridge is built unchanged with a
unique commit-derived filename and validated using `--check` before monitoring.
The probe requires a matching child process-start event under a package-verified
Desktop parent, and records file hashes before/after. Short-lived events may not
permit querying the running image path; this evidence is not proof of a successful
protocol or model request. Static adapter error markers may be detected in fresh
app log files; no raw log lines, auth files or arbitrary environment are emitted. Do not substitute naked extracted EXEs, global environment changes or
debug injection to make this test appear to pass.

A screenshot of at most 1 MiB is also emitted as base64 in workflow logs for
remote visual review where supported. Workflow-log retention is independent of
the seven-day artifact retention; no successful image retrieval is assumed.

Artifacts contain only public package/signature metadata, sanitized test-process
facts and the fresh unauthenticated window image. They do not contain the MSIX,
offline license, app data, credentials, raw process command lines, or environment
values. Test-created activation is closed when possible; the disposable VM is
the final process and filesystem lifetime boundary.

## Policy-read diagnostic

The direct launch at commit `56c7e8b6a55be2374f8fdced2a47962e095af078`
reached the original login screen in the baseline job. The override job logged
`source=override`, started the uniquely named bridge child, and then displayed
“Organization settings could not be loaded.” The Desktop log classified the
failure as `phase=policy-read reason=rpc`. This does not identify an exact method,
error origin, successful handshake, or a network denial.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42169742564).

The separate `Experimental Desktop policy trace` workflow adds an explicitly
instrumented `observe` mode. Its Windows-only test observer launches the unchanged
production bridge and forwards arguments and stdio bytes without issuing extra
requests, retries, invented results, changed capabilities or policy overrides.
It is not installed or included in the production package.

Only fixed method/flag/config-key names, generated correlation numbers, error
codes and fixed classifications, four initialization booleans, and process/EOF
metadata can enter the trace. Raw IDs or their hashes, parameters, results,
messages, argument values, paths, arbitrary stderr, credentials and proof tokens
are never trace fields. Unknown values are classified without their text.
PowerShell validates the fixed metadata schema and publishes a validated snapshot,
not a recopy of a still-growing file.

One fresh directory and prefix allow at most 16 exclusive 64 KiB trace files.
Duplicate request IDs remain uncorrelatable, and exhausting correlation capacity
disables further matching. Missing child exit or output EOF, exhausted trace slots or truncated
summaries mean incomplete evidence, not proof that no failure occurred.
The helper forwards ordinary Windows exit codes and output EOF. Its blocked
stdin reader is released by immediate process exit after the child ends; it is
not a reusable in-process proxy and does not claim Unix signal transparency.

Race/redaction/forwarding tests and a real Windows helper-process test must pass
before the instrumented Desktop launch. The latter holds inherited stdin open
while the native fixture exits, checking byte-exact output, EOF and exit status.
No old 0.166 compatibility mode is enabled simply because the packaged backend
reports 0.167.0-alpha.3.2. The first correlated failure must be established before
considering a separately reviewed compatibility change.

## Exact-version startup follow-up

The instrumented run at `b4775be7ea17a2f34bee7aaef329748dd98610cf`
identified the first blocker as the one-shot startup command, not a JSONL RPC.
Desktop passed `--print-startup-config --startup-client-info`; the default-off
bridge emitted `startup_probe_unavailable` and exited 1. No initialize or
configRequirements/read request occurred in that complete invocation.
[Metadata and screenshot](https://github.com/breakingtime126/magpie/actions/runs/42173927946).

The follow-up explicitly enables only the new exact-version unrestricted startup
profile for the verified bundled 0.167.0-alpha.3.2 binary. Its SHA-256 is
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`.
This does not enable any old authentication or JSONL requirements profile.
Both independently configured official commands must return the same complete,
actually unrestricted success result. A non-null requirement, different version,
unsupported field or mismatching backend result still stops startup. This is not
support for arbitrary organization policies or proof that different underlying
policies are equivalent.

The official one-shot command may refresh policy/auth state or write caches.
This CI uses newly created empty homes, no account credentials and no login or
inference requests. It does not claim zero attempted network activity, and it
does not change firewall, network security or machine policy to force a pass.
Linux race tests and Windows focused tests gate the actual Desktop attempt.
Later initialization, authentication and model-use gates remain unverified until
separately observed. A green observation job must not be described as a usable
Desktop integration.

A genuine one-shot success object has neither a JSONL `result` nor `error` key.
The transport observer deliberately classifies that shape as `unrecognized`
without recording its body. That label alone is not a startup failure. Interpret
it with the unchanged bytes, production validator, child exit code and complete
output EOF, and examine a later JSONL invocation as separate evidence.

## Static override identification

At `f5e0a7e9f90876d583341b3fa7500797c28e809e`, the official one-shot
startup path exited zero. Desktop then launched normal JSONL mode with an extra
`-c` key outside the four initial metadata labels. The adapter rejected the
arguments and exited 2 before reading configuration or starting JSONL backends.
An initialize request seen by the observer is not proof that the adapter read it.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42176293112).

A subsequent metadata-only refinement freezes 233 exact public config names
from the reviewed official schema at `c40729ea5cf8e947481b130c59807273eda2f8e9`,
blob `f5149c6efdf7ab9f1550aa0639dd5f2401205f25`. The finite feature names plus
selected boolean/enum keys are not a wildcard. Known literal booleans and allowed
per-key enum spellings become fixed classification labels. Objects, paths,
credentials and other values remain redacted; unknown or dynamic keys remain
unknown. Nothing is fetched from a schema during observation. This does not add
production argument support or authorize any setting change. The exact actual
override must be observed and its semantics reviewed before support is added.


The 233-name observation still classified the actual normal-launch override as
unknown at commit `05e3bd90a50bca2c2a3164148a748c17aa992cad`. It did not
identify the key or prove JSONL initialization reached the adapter.

The next instrumented CI pass reads only three fixed source entries in the
already signature-, hash- and registration-verified official package's
`app/resources/app.asar`. Entry names come from that exact package's recorded
failure stack. The stdlib-only Node reader neither loads nor executes package
code, extracts files, modifies the package, nor reads user data. The documented
Electron ASAR layout, sizes, offsets, links and redirects are checked; bounded
synthetic archive and lexer tests run on Linux and Windows first.

The output consists solely of static candidate key names, fixed classifications,
counts and explicit unsupported states. Raw source, values, command lines and
dynamic key portions are not output. PowerShell independently validates the
metadata schema before publication. The lexical reader is intentionally not a
full JavaScript parser or control-flow proof. Ambiguous syntax discards that
entry's candidates. Computed or escaped keys, returned object forms and other
unsupported definitions can remain unresolved. A candidate does not prove the
actual runtime override and must never automatically expand production argument
acceptance. The adapter and policy behavior remain unchanged.


The static inspection at `49a6aed41c71f8ac74d450d2a98200604ad0017d`
completed and found a direct `-c` candidate named `forced_login_method`.
The pinned official schema permits only `chatgpt` or `api`. A subsequent
observer-only change recognizes this one additional fixed key and those two enum
labels to determine whether it is the real extra override. Arbitrary values stay
redacted, and neither production acceptance nor login policy changes. Static
source also contains unrelated command flags, so candidates are never treated as
proven app-server launch parameters. Both Linux and Windows synthetic inspector
suites passed 16/16 without skips at the observed commit.


The `9c69ae37750e25545657f5260e0e903fad7c7810` runtime still reported an
unknown override after adding the forced-login candidate. This neither confirms
that candidate nor excludes whitespace/quoted key spellings. No production
forced-login support was added from this hypothesis.

The observer revision at `31f269b0a6acb9bd5bb7e8d676ca89b797e96e03`
froze all 830 explicitly named property paths in the same pinned schema,
following only explicit properties and local schema references/combinators.
It never admits dynamic map entries or wildcard paths. The original 234 value
classifications retain their types; every additional name has an opaque value.
The actual additional override still appeared as `other_key`/`other` and the
normal app-server invocation exited 2. That classification was not evidence that
the official CLI would reject the argument.

### Native literal-key diagnostic correction

Reviewing the pinned official
[CLI parser](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/utils/cli/src/config_override.rs)
and [override application](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/overrides.rs)
showed that the CLI splits at the first equals sign, trims the whole key, then
splits its path at literal dots. It does not unquote key segments or trim each
segment. The earlier observer's TOML-like normalization therefore could falsely
label a quoted spelling as a known setting. No production argument acceptance
was based on that diagnostic normalization.

The corrected observer recognizes only whole-key ASCII whitespace trimming and
fixed bare dotted names. Quoted keys, whitespace within a segment and other
unreviewed syntax remain unknown. One schema property has a literal dot within
its segment; its frozen quoted boundary remains unrecognized. No flattened
alias is invented. Non-ASCII whitespace may be accepted by the native parser
but remains conservatively unknown to this diagnostic.

Unknown config events include only fixed shape/family labels plus three bounded
classifications: an exact public root followed by a dot or `other`; a fixed,
ordered set of character classes found only before the first equals sign; and
whether the complete right-hand side is literally `true`, `false` or other.
No original key, dynamic suffix, raw value, length, position, count or hash is
published. The lexical root does not assert that the native parser accepts the
setting or that its value has any particular semantics. PowerShell validates
the fixed fields, labels, order and event relationship before publication.

All argument bytes remain unchanged. The production adapter still rejects
unsupported arguments, and authentication, permissions and organization policy
are not altered by this diagnostic. A recognized root or candidate remains an
investigation clue, not permission to apply a setting to either backend.
