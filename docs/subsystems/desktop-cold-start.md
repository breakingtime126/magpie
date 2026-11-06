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

## Public bundled-plugin key identification

The actual `eed7071a22cc9fef22fdf1dc4def78d085f59cc0` Windows observation
identified the extra assignment as a `plugins.` key containing an at-sign and
the complete right-hand side `true`. The lifecycle trace was complete, but the
normal invocation still exited 2 before a successful initialize response.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42190963869).
This rules out treating the earlier forced-login static candidate as the actual
parameter. It does not yet identify a plugin or the particular boolean field.

A further metadata-only lookup freezes the exact public identities named in
the same reviewed official source:
[bundled hook selection](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/plugin/src/bundled_hooks.rs#L28-L88)
and [plugin loader](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs#L260-L274).
The identities are browser, chrome, chrome-dev, chrome-internal, computer-use,
unified-computer-use, sites and visualize, each at the openai-bundled marketplace.
Only their exact direct `enabled` and `auto_provision` property paths are
eligible. This is a 16-name investigation dictionary, not a claim to enumerate
every plugin shipped in Desktop. These source occurrences do not authenticate a
plugin or verify its signature. A different plugin, marketplace, nested field
or spelling remains redacted.

The lookup runs before the conservative bare-key reader because native plugin
identities contain an at-sign. It performs only whole-key ASCII whitespace
trimming and exact equality against frozen names, and emits only a matched
constant plus literal true/false/other. It never unquotes, extracts an arbitrary
identity, interprets a value, or changes the forwarded assignment. PowerShell's
fixed-label validator uses the same 16 additional names.

Recognition is not authorization to enable a plugin. Native `enabled` changes
installed-plugin activation and may expose skills, MCP servers, apps or hooks.
Native `auto_provision` is provisioning metadata, not authorization. Creating a
new entry can still make an installed plugin eligible for activation through
native defaults. It must not be forwarded as an inert or harmless setting. Neither field is passed through
by the production adapter in this diagnostic change. The actual exact match,
official behavior, both backends and remaining initialization gates must be
reviewed before any production compatibility change.


### Source-bound public catalog diagnostic

At `a54fd10a36483aaaf6498b3ce47682ae9f3415e9`, all three experimental
workflows passed, including
[the policy trace](https://github.com/breakingtime126/magpie/actions/runs/42193686360).
The verified 26.1103.3517.0 Desktop and its bundled
`codex-cli 0.167.0-alpha.3.2` still completed only the one-shot startup.
Normal JSONL startup rejected the extra assignment, and none of the 16 frozen
public plugin keys matched. A green diagnostic job did not establish a working
bridge.

The source-bound diagnostic obtains candidates only from the same verified
installed public package. The Node reader scans bounded packed JavaScript
under `.vite/build/` and four fixed marketplace-manifest layouts under
`app/resources/plugins/<marketdir>/`. It does not execute package code, read
private homes or environment values, search accounts, or fetch plugin catalogs.
The existing three-entry launcher inspection and its validation remain in place.

The added `publicPlugins` report is either a fixed blocked reason or a public
table with exact ASCII keys, source paths, SHA-256 values, derivation labels,
and explicit scan coverage. It scans at most 2,048 JavaScript files, at most
16 MiB each, and 16 manifests of at most 256 KiB each, with a combined source
limit of 128 MiB. At most 64 supporting sources and 128 distinct keys fit in
the existing 64 KiB report bound. The Node inspection has a fixed two-minute
process deadline. Lexically unsupported JavaScript contributes
no keys. Such a scan is marked `partial`, and the coverage records its gap.
A complete scan still does not prove that its static candidates are runtime
arguments.

Before publishing any candidate text, PowerShell validates the complete report,
source/derivation relationships, exact key grammar, strict ordinal sorting,
bounds and key digest. It then opens the original downloaded MSIX read-only,
rechecks its already signature-verified fixed package hash, and streams SHA-256
over the unique exact `app/resources/app.asar` ZIP entry. That hash must match
the installed archive hash reported by Node. The complete archive binding
covers its internal JavaScript sources without executing or extracting them.
Each external manifest source must independently match its exact original ZIP
entry hash. Case aliases and duplicate ZIP entries, redirected package path
components, nonregular entry attributes, missing sources, oversized entries,
hash failures and a two-minute streaming deadline all fail closed.
No package is extracted, repacked or changed for this verification.

The handoff is the process-local `MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES` variable,
canonical compact JSON with ordered fields `schema`, `packageSha256`,
`archiveSha256`, `keysSha256`, and `keys`. The digest covers the ASCII-sorted
keys joined by exactly LF, without a trailing LF. Each key is at most 128
ASCII bytes and has only the direct `plugins.<identity>@<marketplace>.enabled`
or `.auto_provision` form. The payload is at most 16 KiB UTF-8; the complete
minimal Windows child environment is at most 30,000 characters including
entry separators and terminators. Existing presence checks reject an inherited
handoff. An empty, unsupported or invalid catalog stops before Desktop launch,
with an explicit unknown-identity result, rather than treating failure as no
matching plugin.

The observer preserves the original fixed lookup first. Only otherwise unknown
keys can match the validated dynamic table, by exact equality after the existing
whole-key ASCII trimming. A matched event contains only a one-based
`publicPluginRef` and `bool_true`, `bool_false` or `other`, never the key or a
runtime value. An independent `public_plugin_catalog` event identifies the table
by its key digest. PowerShell requires exactly one matching digest per observed
PID after observer start and before any argv or child event, even if no reference
matches. It checks the exact event shape and integer
bounds, and resolves references only against its independently verified public
table. Repeated argv references are allowed; duplicate catalog events, mixed
fixed-key/unknown fields, changed digests and out-of-range references are not.
Published match names come from the public table, never from argv. The observer
strips this variable before launching the unchanged production bridge.

This is positive-match evidence only. Partial coverage, unsupported syntax or
no match must not be described as proof that a plugin is absent. A match
identifies public spelling, not plugin authenticity, signature verification,
installed-plugin safety, successful initialization, or authority to enable or
provision anything. Neither `enabled` nor `auto_provision` is forwarded by this
change. Authentication, organization policy, production argument acceptance
and both private backend homes remain unchanged.

Offline PowerShell fixtures load only allowlisted validator function definitions
from the probe's AST. They use synthetic ZIPs and check field casing, unknown
fields, duplicate keys and source paths, coverage/provenance gaps, source and
handoff digests, source indices, per-PID catalog ordering, strict references,
ZIP aliases, file/descendant collisions and link attributes, and environment
bounds. A synthetic ASAR also exercises the actual Node report through the
PowerShell JSON and catalog validators on both platforms. The policy workflow
runs these tests, both Node suites and the observer/bridge suites on Linux and
Windows at the exact pushed head, while preserving prior behavioral red
fixtures. The new native-executable observer regression runs on Windows and must also
fail behaviorally against the pre-catalog `a54fd10a` code; its explicit Linux
skip does not count as red evidence. These tests never install Desktop or contact an
account; the separate fresh Windows observation retains the signed-package,
registration, GUI-session and no-account gates.


### Modern JavaScript parsing and construction diagnostics

The source-bound observation at
`e1ceddf7542163cfaacdc975a72387e51b2aeb82` completed with all three
experimental workflows green. Its
[policy trace](https://github.com/breakingtime126/magpie/actions/runs/42200500315)
still did not identify the normal-launch plugin override or obtain a successful
initialize response. One-shot startup exited zero; normal startup exited 2.
The original signed package, bundled backend and production restrictions were
unchanged.

The public table had 16 keys from one bundled marketplace manifest. Of 29
selected JavaScript files, 20 parsed and nine were skipped; all three fixed
startup chunks reported `unsupported_lexical_syntax`. The scan covered
12,431,638 source bytes. This establishes a parser coverage gap, not evidence
that the unidentified runtime key is absent from the package.


The public scanner now uses the pinned Acorn 8.15.0 full JavaScript parser,
vendored with its MIT license under
[build/vendor/acorn-8.15.0](../../build/vendor/acorn-8.15.0/README.md).
The offline tests verify the parser and license bytes against their reviewed
upstream Git blob and SHA-256 identities. No npm install, runtime download,
package import, dynamic code evaluation or execution of Desktop source is
introduced. The separate three-entry legacy launcher analysis remains labeled
by its existing lexical status; it is not silently reclassified as an AST
result.

The public analysis must finish parsing a whole source before retaining that
file's literal candidates or construction diagnostics. Syntax failure leaves
no partial candidates or constructions from the file. Parser-time token, node,
depth and deadline guards supplement the existing file and source-byte limits.
The inspector process also has a 512 MiB V8 old-space limit and the existing
120-second wall timeout; that heap setting is not a claim to bound total
process memory. Parser exceptions and stderr are captured and never published
as source excerpts or raw error messages. Resource exhaustion blocks the
diagnostic rather than being interpreted as an empty catalog.

The additive `publicPlugins.analysis` object identifies the parser as
`acorn_8_15_0`. Its two ordered method rows describe `getConfigOverrides`
and `startupCommand`. `definitions` counts recognized function or method
definitions; `unresolved` counts maximal plugin-prefixed string, template or
concatenation expressions in those definitions whose paths cannot be classified.
Unresolved expressions can outnumber definitions. Both totals are independently
bounded at four million.

At most 128 coalesced construction rows carry only a verified ASAR source index,
one of those two method labels, `literal`/`template`/`concat` form, a fixed
path-shape label, `bool_true`/`bool_false`/`dynamic`/`other` value class,
and a positive count. Their total count is independently bounded at four
million. Sources can support these diagnostics without contributing a candidate
key. These are static syntax observations, not evidence that a method executes
or that any expression becomes a launch argument.

The eight shared shape labels are `direct_enabled`, `direct_auto_provision`,
`mcp_server_enabled`, `app_enabled`, `app_destructive_enabled`,
`app_open_world_enabled`, `app_default_tools_enabled` and `app_tool_enabled`.
They name only reviewed path forms. No plugin, marketplace, MCP server, app or
tool identity, source expression, arbitrary property name or constructed value
is emitted by a shape diagnostic. The catalog continues to accept only its
original two direct public-key forms; nested shapes do not become candidates
or extend production argument support.

For comparison, the observer can attach the optional `configPluginShape`
label to an existing unknown config argument. It remains unknown. Fixed known
keys and catalog-reference events never carry this label. PowerShell validates
the exact unknown-event shape and its relationship to the existing plugin root,
at-sign character classification and literal-value class. The key and original
argument bytes are never printed or changed. A shape match cannot establish
identity, plugin trust, runtime activation, authentication or successful startup.

PowerShell validates all new fields, scalar types, bounds, method order,
source kinds and indices, and duplicate construction tuples before the existing
original-MSIX provenance gate permits publication. Its tests cover valid forms,
aggregate boundaries, impossible method/count relationships, mixed runtime
event fields, malformed types and deliberate canary strings. A modern-syntax
synthetic ASAR exercises the actual Node report through PowerShell on Linux and
Windows. New behavioral red fixtures run against the exact `e1ceddf7` code
for both modern JavaScript parsing and unknown plugin path shapes; their expected
assertion messages must appear, so compile/setup failures do not count. Earlier
red fixtures and the guarded Windows native-exit handling remain intact.

The policy workflow watches the vendored parser, AST helper and standalone test
files explicitly. Changes to these diagnostics run their Linux/Windows tests
there without adding duplicate package installation to the separate baseline
cold-start workflow. All prior signature, archive/source binding, account-free
session, minimal-environment, per-PID catalog and production-policy boundaries
remain in force.
