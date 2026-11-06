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
no API key, and no startup, requirements, attestation, OAuth, auth-status or
feature-list opt-ins. The separate
instrumented follow-ups below record their separate startup and launch-compatibility
opt-ins. Startup inspection alone still selects only its original profile. The exact
commit-built production bridge uses a unique commit-derived filename and is
validated using `--check` before monitoring.
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
instrumented `observe` mode. Its Windows-only test observer launches the exact
commit-built production bridge and forwards arguments and stdio bytes unchanged,
without issuing extra requests, retries, invented results, changed capabilities or
policy overrides. The bridge's separately selected compatibility modes are recorded
in the report; the observer itself does not grant or select them.
It is not installed or included in the production package.

Only fixed method/flag/config-key names, generated correlation numbers, error
codes and fixed classifications, four initialization booleans, the bounded
request booleans documented below, and process/EOF metadata can enter the trace. Raw IDs or their hashes, parameters, results,
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

The separate `-EnableReviewedStartupProbe` switch enables only the exact-version
unrestricted startup profile for the verified bundled 0.167.0-alpha.3.2 binary. Its SHA-256 is
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


## Explicit CI launch-compatibility follow-up

At `643cc5d060659c68c75e342e7e0fd71590b6993c`, the
[Windows policy trace](https://github.com/breakingtime126/magpie/actions/runs/42213706926)
identified the actual normal-launch override by its source-bound public catalog:
`plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true`.
Desktop was the signed, hash-verified 26.1103.3517.0 package and its backend was
0.167.0-alpha.3.2. The one-shot startup exited zero while normal startup still
exited 2 under the old argument parser. The positive spelling match does not prove
plugin installation, loadability, successful initialization or authentication.

The separate `-EnableReviewedLaunchCompatibility` switch in
[the cold-start probe](../../build/test-desktop-cold-start.ps1) is default-off.
It requires `-Mode observe` and the independent
`-EnableReviewedStartupProbe` switch. The existing exact repository, experimental
branch and GitHub-hosted-runner guards run before any probe filesystem or package
actions. Only
[the experimental policy-trace workflow](../../.github/workflows/experimental-desktop-policy-trace.yml)
selects the additional switch. Baseline/direct-launch configurations and
startup-only callers are not broadened. For the additional launch-compatibility
switch, the signed package must also have exact manifest version
`26.1103.3517.0` and SHA-256
`ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643`.
A changed floating download or mismatching version reports a package mismatch and
stops before provisioning. Existing signature and registered-identity checks remain
mandatory; startup-only and baseline gates are unchanged.

The CI profile keeps `startup_probe=compare-unrestricted-v0.167.0-alpha.3.2`,
the exact `codex-cli 0.167.0-alpha.3.2` version check and the
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`
executable pin on both backend entries. It separately adds:

- `desktop_plugin_override=codex-app-tools-v0.167.0-alpha.3.2`;
- `attestation=unavailable-v0.167.0-alpha.3.2`;
- `gateway_oauth=primary-read-only-v0.167.0-alpha.3.2`;
- `gateway_oauth_provider_name="CI alpha launch-only"`;
- `config_requirements_read=compare-null-v0.167.0-alpha.3.2`;
- `auth_status=primary-no-auth-v0.167.0-alpha.3.2`;
- `experimental_feature_list=compare-v0.167.0-alpha.3.2`.

These are configured modes, not assertions that their runtime exchanges succeeded.
The native forwarding contract and fail-closed behavior are documented in
[the app-server bridge reference](app-server-bridge.md).
The probe does not install, copy, provision or link a plugin/cache, set a shared
backend `working_directory`, load credentials or add inference, authentication or
diagnostic requests. Each native child retains its separate empty HOME and
CODEX_HOME, the existing loopback discard model endpoint, and its
`features.apps=false` / `features.plugins=false` configuration. A successful
argument parse is not evidence that a plugin was loaded or that a model is usable.
No firewall, network policy or other security setting is changed; no zero-egress
claim is made.

Gateway OAuth reads remain primary-native and read-only. A false or null result is
not logged in. Attestation remains explicitly unavailable; a failure there is not
evidence of a Windows token or access-token problem. JSONL requirements reads must
obtain actual successful `requirements:null` results from both backends on each
read. Missing, malformed, failed, non-null or incompatible results fail closed,
never become invented null requirements. The one-shot startup comparison remains
independent and unchanged. Reports list the explicit profile and these limits
without recording response bodies, credentials, proof tokens or raw arguments.
A green observation still means bounded process/window/metadata evidence only.

[Offline PowerShell profile tests](../../build/test-desktop-ci-profile.ps1)
AST-load only the allowlisted gate/configuration, sanitized-shape and evidence helpers. They cover missing
opt-ins, baseline/direct modes, repository/branch/runner denials, unchanged default
and startup-only JSON, all exact compatibility values, both executable pins,
independent backend homes, unchanged caller inputs, exact Desktop version/package
hash rejection, and backend version/hash/count rejection.
They run on Linux and Windows alongside all prior catalog and observer regressions.
The standalone `TestDesktopPluginLiteralReachesLaunchOptions` fixture is also
copied onto the exact pre-change `643cc5d0` adapter; its named behavioral assertion
must fail, so compilation, setup errors and skips are not accepted as red evidence.
No fixture launches Desktop, contacts an account or tests a real plugin.

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
16 MiB each, and at most 64 unique JSON files (including at most 16 marketplace manifests)
of at most 256 KiB each, with a combined source
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
ASCII bytes and has the direct `plugins.<identity>@<marketplace>.enabled`
or `.auto_provision` form, or the source-linked MCP declaration form described
below. The payload is at most 16 KiB UTF-8; the complete
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
as source excerpts or raw error messages. Source-reading, provenance, global resource and process failures block the
diagnostic rather than being interpreted as an empty catalog. The narrowly
classified file-local AST skips below discard all evidence from the affected
file and explicitly reduce coverage.

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
is emitted by a shape diagnostic. Shapes alone never become catalog candidates or extend production argument
support. The separately authenticated MCP declaration chain described below is
the only additional nested key form admitted to the diagnostic catalog.

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


### Bounded partial parser coverage

The `af097bf4336559cb468ffb72b7458458845f2a9b`
[Windows diagnostic](https://github.com/breakingtime126/magpie/actions/runs/42204661575)
passed its 143 PowerShell and 52 Node checks, then stopped before Desktop launch
with the generic `public_source_limit` reason. The signed package was unchanged.
That observation does not identify which parser or package-reader bound was hit,
and it does not establish a runtime key.

The following diagnostic keeps every limit, but distinguishes six explicitly
identified file-local AST guards: token, allocated-node, parse-depth, walk-depth,
walk-node and per-file deadline. Like syntax errors, these discard the entire
file's analysis atomically. No candidate, source record, method count or
construction row from that file survives. Bytes already read still count toward
the unchanged aggregate source-byte budget.

The successful public analysis adds `skipped` rows containing only a fixed
`reason` and positive integer `count`. Nonzero rows appear once each, in this
exact order: `syntax_error`, `public_ast_token_limit`,
`public_ast_node_limit`, `public_ast_parse_depth_limit`,
`public_ast_walk_depth_limit`, `public_ast_walk_node_limit` and
`public_ast_deadline`. There are at most seven rows; their counts sum exactly
to `coverage.jsSkipped` and cannot exceed 2,048. A complete report has no skip
rows. Any skipped file requires `partial` status.

Source-byte limits, read or UTF-8 failures, archive/hash/provenance failures,
global source/catalog/construction-row caps, unclassified `RangeError` and
whole-process failures still block the entire diagnostic. In particular,
`public_ast_bytes_limit` and `public_ast_range_error` are never skip reasons.
The bounds remain 16 MiB per source, four million tokens, one million allocated
or walked nodes, depth 512 for guarded parsing or AST walking, and one shared
15-second deadline per source, in addition to the process heap and wall limits
above. Nothing is retried with larger limits or a less restrictive parser.

Independently completed JavaScript and marketplace-manifest evidence may remain
in a partial catalog. It still needs the same complete schema, digest and
original signed-MSIX source verification before publication or injection into
Desktop. An empty catalog still stops; a valid nonempty partial catalog can
support positive runtime matches and the fixed unknown-path-shape diagnostic.
Incomplete coverage and an unmatched runtime argument remain unknown, never
proof that a plugin or key is absent.

PowerShell validates skip ordering, uniqueness, exact field types and counts,
coverage arithmetic and the complete/partial relationship. Its fixtures reject
global failures disguised as skips, extra source/candidate fields, duplicates,
unknown or differently cased reasons and inconsistent totals. Blocked reports
retain the exact three-field shape with fixed reason labels and no retained
catalog, source excerpts or exception messages.

An additional offline regression file exercises the existing AST/package APIs
against exact `af097bf4` code. Both the typed parse-depth reason and retention
of independent complete evidence must fail with their intended assertions on
the old implementation. The current Linux and Windows suites run that same file;
the policy workflow explicitly watches its path and preserves every earlier
behavioral red check.


### Source-linked MCP declaration identification

At `e1c8fb52c61406baba1a6d712c0fcea83aacaf0d`, the
[Windows policy trace](https://github.com/breakingtime126/magpie/actions/runs/42207254261)
completed with 28 JavaScript sources fully analyzed and one whole-file
`public_ast_node_limit` skip. Its 22 direct catalog keys did not match the
normal-startup argument. The unknown argument had the fixed
`mcp_server_enabled` shape and literal true classification. No runtime
plugin, marketplace or server name was logged. Normal startup still exited 2;
one-shot startup exited zero. This shape is evidence for a narrower diagnostic,
not successful initialization or permission to enable a server.

The additional catalog form is only
`plugins.<identity>@<marketplace>.mcp_servers.<server>.enabled`.
All three variable segments retain the same ASCII alphanumeric/underscore/hyphen
grammar, first character alphanumeric, at most 64 bytes each; the complete key
still cannot exceed 128 bytes. App/tool paths, arbitrary nested fields and MCP
`auto_provision` are not admitted. The production adapter remains unchanged.

Candidates are derived from a connected declaration chain in the public package,
not a cross-product of unrelated names. The marketplace entry's name and the
marketplace document's name define the plugin identity, following the pinned
[native marketplace resolver](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/marketplace.rs#L530-L588).
Neither the directory name nor `plugin.json.name` substitutes for that identity.
Only bounded local sources are followed within that marketplace root. Native
recognized plugin manifest locations and MCP declarations are inspected
independently, conservatively recording public declarations rather than
asserting which source wins native precedence.

A nested candidate adds zero-based `marketplaceSource` and `pluginSource`
indices to its existing `source`, key and derivation. The fixed derivations
are `legacy_mcp_inline`, `legacy_mcp_default`, `legacy_mcp_path` and
`agent_mcp_default`. Inline declarations use the plugin manifest itself as
their source. Otherwise the source is the linked MCP JSON file; the default is
`.mcp.json` for a legacy manifest or `mcp.json` for a supported
Agent Plugins 1.0 root manifest. Explicit local MCP file references are bounded
to the same plugin root. An unsupported root schema is not reinterpreted as a
legacy manifest. The server-map key supplies the MCP policy segment before
native namespacing or app routing. No transport command, URL, environment value,
header, arbitrary JSON value or private metadata is published.

The new source kinds are `plugin_manifest` and `mcp_manifest`. All remain
inside `app/resources/plugins/<marketdir>/`, with safe ASCII components no
longer than 64 bytes and full source paths no longer than 512 bytes. Local
plugin-source paths and local MCP references have at most eight components,
including the MCP filename. Absolute paths, dot traversal, aliases, links,
variables and URI references are not followed. Every new source counts toward
the same 64-source and combined 128 MiB budgets. Each JSON file is at most
256 KiB; at most 64 distinct JSON files are read, including no more than 16
marketplace manifests. The 128-key, 16 KiB environment, 64 KiB report, parser and
process limits do not change.

The separate `manifestAnalysis` object contains only integer
`pluginEntries` (at most 2,048), `pluginManifests`, `mcpFiles`
(each at most 64), and `skippedEntries` (at most the entry count).
An entry with an unsupported or missing local source, manifest, schema, MCP
reference, file or server identity contributes once to the skipped count.
Valid sibling declarations can remain. The existing complete/partial status
still describes JavaScript coverage; manifest incompleteness is separately
visible and never means that other server keys are absent. Malformed or
duplicate JSON, read/provenance failures and global bounds still block the
whole catalog rather than yielding an empty result.

Before publication, PowerShell validates exact candidate shapes, derivation and
source-kind relationships, indices, shared marketplace/plugin roots, exact
default filenames and local path bounds. Every supporting marketplace, plugin
and MCP manifest must independently hash to its unique original signed-MSIX ZIP
entry on the same read-only verification stream. A terminal MCP file alone
cannot establish provenance. The trusted bounded extractor establishes JSON
relationships; PowerShell binds all supporting bytes and validates the emitted
relation, without executing or reparsing plugin configuration. The existing
whole-ASAR binding, ZIP ambiguity rejection, no-account/GUI-session gates,
per-PID digest ordering and reference-only runtime events are preserved.

This catalog identifies package-declared positive candidates only. It does not
authenticate a plugin, select native precedence, validate a transport, prove
that a server is installed or loadable, establish policy approval, or authorize
forwarding. A match is still just a public spelling match. Enabling an MCP
server can activate a connection or process; no enablement, provisioning,
credential, permission or production argument change is made here.

Offline tests cover every derivation, linked source kind/index/root, exact key
and path bounds, all-source ZIP hashes, aliases and links, and rejection of
unreviewed nested forms and raw values. An actual Node report from synthetic
marketplace/plugin/MCP files passes through PowerShell and original-ZIP binding,
with command/environment canaries absent from output. New scanner and observer
behavioral red tests use exact `e1c8fb52` APIs and require their named
assertion failures before the current Linux/Windows suites run. The workflow
watches the new MCP scanner tests explicitly and preserves all earlier reds.

The RPC metadata allowlist recognizes the exact method spelling
`getAuthStatus` and the reviewed public startup labels listed in the
[native trace-envelope reference](app-server-bridge.md#native-0167-request-trace-envelope).
Differently cased or suffixed spellings remain unknown. These are diagnostic
labels, not proof that Desktop sent a candidate. The bounded request diagnostic
below does not add a production handler or change routing, sign-in or policy behavior.

### Bounded getAuthStatus request flags

The signed Desktop `26.1103.3517.0` and bundled backend
`0.167.0-alpha.3.2`
[observation](https://github.com/breakingtime126/magpie/actions/runs/42217188118/job/127891199826)
showed `getAuthStatus`, `account/read` and `configRequirements/read` after
the initialize response. The original method-only trace did not reveal the
authentication request's flags. This motivates a narrowly scoped diagnostic,
not a claim that initialization or authentication succeeds.

Only an unambiguous client-to-bridge `getAuthStatus` request with ordinary
request correlation is eligible for the
[test-only parser](../../build/desktop-rpc-observer/auth_request_metadata.go). Its optional `authParamsValid` boolean is
true only when `params` is an object containing no keys except the exact
`includeToken` and `refreshToken` fields, each absent, null or a native JSON
boolean. For valid params, two additional booleans,
`includeTokenRequested` and `refreshTokenRequested`, report the native
requested flags, with absent/null fields defaulting to false. Those defaults
describe request syntax, not actual authentication state.

Missing/null/non-object params, unknown fields, wrong types and duplicate
parameter keys emit `authParamsValid:false` without requested flags. They never emit partial
requested flags or convert an invalid value to false. Escaped key spellings
are decoded before duplicate comparison. Nested data, arbitrary keys,
credentials, IDs, paths, response bodies and raw errors are never published.

The optional bounded native `trace` carrier is recognized without telemetry
propagation. A present carrier adds only `tracePresent:true` and
`traceValid:boolean` on an unambiguous client auth or requirements request;
invalid trace suppresses all auth flags. No trace IDs, state, lengths, hashes
or raw values are recorded. Diagnostic request IDs use the existing bounded
native string-or-int64 rule. See the
[shared contract, bounds and evidence limits](app-server-bridge.md#native-0167-request-trace-envelope)
rather than interpreting shape validity as authentication or W3C validity.
Absent trace retains the previous metadata shape and all old wire-byte bounds.

Ambiguous/duplicate envelopes, unknown envelope fields, invalid JSON-RPC version
fields, invalid IDs, notifications, responses, other directions/methods and
duplicate-ID or correlation-limit records carry none of these three fields.
Malformed, oversized and unterminated lines retain their existing generic
classification. Missing diagnostics mean unknown, never false. Observation
preserves every forwarded byte and all existing line/trace/correlation bounds.

PowerShell validates the exact event shape, native boolean types, direction,
method, request class and correlation before publication. A false validity
field forbids both requested flags; a true one requires both. The focused
public-catalog PowerShell fixtures exercise accepted and rejected shapes,
casing, unknown fields, invalid types and duplicate encoded aliases. The
self-contained
[`auth_request_metadata_red_test.go`](../../build/desktop-rpc-observer/auth_request_metadata_red_test.go)
checks flag combinations, invalid and nested canaries, ambiguous envelopes,
both directions, unchanged correlation, exact wire bytes and existing caps.
Copied alone onto `bcc80e7c31fa65f01e181c3eee08759862affa8a`,
`TestGetAuthStatusRequestedFlagsObserved` is the behavioral red target.
This is a test definition, not a claimed executed red/green result.

The default bridge still rejects `getAuthStatus`; the separately selected
[disposable no-auth status CI mode](#disposable-no-auth-status-ci-gate) below is
a narrow exception, not real-account support. Even when
`refreshTokenRequested:false`, the official implementation can perform
implicit credential refresh. These diagnostics do not establish a pure,
offline or side-effect-free read and do not authorize authentication changes,
login, token disclosure or a production compatibility mode.

### Complete pinned RPC vocabulary and read-request shapes

The observer and PowerShell sanitizer now share
[`rpc_method_facts.json`](../../build/desktop-rpc-observer/rpc_method_facts.json),
a finite, source-derived dictionary from official Codex
`c40729ea5cf8e947481b130c59807273eda2f8e9`. It contains 183 unique client
method names: 106 in the checked-in stable JSON schema, 74 experimental
declarations and three deprecated v1 methods. The checked-in JSON schema alone
is incomplete: the exporter removes experimental declarations and
`getConversationSummary`, `gitDiffToRemote`, and `getAuthStatus`.
The full [ClientRequest declaration](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/common.rs#L502-L1581)
is authoritative for this diagnostic vocabulary.

Generation independently recomputed every recorded upstream Git blob identity
and SHA-256 and reconciled all three disjoint name sets. Only public facts and
source identities are retained, not the upstream source bodies. Go embeds the
same JSON that PowerShell reads from this repository. Both verify its fixed
SHA-256 before using it. The JSON is one line without a trailing newline, so
Windows checkout line-ending conversion does not change its identity. No
normalization occurs before hashing. An invalid dictionary blocks diagnostic setup or
publication; it never becomes an empty or partially trusted table. Offline
tests verify the frozen artifact and provenance; they neither fetch upstream
nor independently regenerate upstream schemas during a test.

The existing `initialized` and `attestation/generate` labels remain separate
from the client dictionary. Exact decoded JSON spellings map to the fixed
dictionary value. Unknown names still yield only `unknown`, never a raw name,
sample, length or digest. The declared `mock/experimentalMethod` is included
as public test-method vocabulary, not supported behavior. This complete set
covers only the pinned public source, not undisclosed proprietary Desktop
extensions. Name recognition does not route a method or enable a capability.

Two additional request-only diagnostics expose native boolean fields after
the same bounded, unambiguous envelope, native ID, correlation and optional
native trace checks. Both methods retain native envelopes without `jsonrpc`.
Valid trace shapes retain `tracePresent:true,traceValid:true`; invalid trace
shapes expose those two booleans with `traceValid:false` and suppress all
parameter metadata. No trace value is recorded.

- `config/read` emits `configParamsValid`. When true, it also emits
  `configIncludeLayers` and `configCwdProvided`. Params must be an object
  containing only `includeLayers` and `cwd`. Omitted `includeLayers`
  defaults to false; explicit null is invalid. `cwd` is absent, null or a
  well-formed string. Only a non-null string, including empty, means provided.
- `experimentalFeature/list` emits `featureListParamsValid`. When true,
  `featureListCursorProvided`, `featureListLimitProvided` and
  `featureListThreadIdProvided` report only non-null field presence.
  Params must be an object containing only `cursor`, `limit` and
  `threadId`. Cursor/thread ID are optional nullable strings; limit is an
  optional nullable native uint32, including zero. Negative values, fractions,
  exponent tokens, overflow, booleans and numeric strings are invalid.

The exact source shapes are
[ConfigReadParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L390-L398)
and [ExperimentalFeatureListParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/experimental_feature.rs#L10-L22).
String validation rejects invalid UTF-8 and unpaired escaped surrogates rather
than accepting Go's replacement decoding. Ordinary escapes, valid surrogate
pairs and schema-valid empty strings retain their native shape semantics.
No path is resolved or read, and no cursor, limit or thread value is logged.
Shape validity does not establish a usable path, cursor, thread or effective
configuration.

Missing/null/non-object params, unknown keys, wrong types and duplicate decoded
keys emit validity=false alone, without partial companion fields. Ambiguous
envelopes, duplicate escaped aliases, wrong directions, responses, notifications,
duplicate IDs and exhausted correlation produce no new parameter metadata.
Existing generic malformed/oversized/unterminated classifications, stream
bytes, trace caps and correlation quarantine remain unchanged.

PowerShell checks exact per-method event fields, booleans, native-trace
relationships and invalid-only shapes before publication. It rejects mixed
metadata families, values, casing aliases and duplicate fields. The observer
tests exercise the whole vocabulary and pinned facts, both parameter shapes,
type boundaries, Unicode, nested canaries, both directions, trace validity,
correlation, malformed input, and exact forwarding. The existing narrow startup
method regression now expects only the explicitly added feature-list diagnostic
on that exact request; all other payload and response guarantees remain intact.

The self-contained
[`read_request_metadata_red_test.go`](../../build/desktop-rpc-observer/read_request_metadata_red_test.go)
can be copied alone onto `4e75e65d04f36618fd05dcd858dd19ceb3bc0a4c`.
`TestCompleteRPCVocabularyMetadataRed` must fail with
`source-declared client method missing from complete vocabulary`;
`TestReadRequestShapesMetadataRed` must fail with
`read-request shape boolean missing or incorrect`.
These are test definitions, not an executed verification record. Formatting,
tests, vet, race checks and real Windows observation need exact-head CI evidence.

No production `config/read` or `experimentalFeature/list` route is introduced
by this diagnostic. No auth/config result body, private identifier, credential,
native trace carrier or raw error is logged. The vocabulary/shape diagnostic itself does not alter launch opt-ins or
establish successful Desktop initialization, equivalent backend configuration,
offline authentication or service acceptance. The separately reviewed CI mode
below has its own explicit admission and result gates.

### Disposable no-auth status CI gate

The signed Desktop observation at
[4e75e65d](https://github.com/breakingtime126/magpie/actions/runs/42226093994)
established valid native trace envelopes, successful fresh dual
`configRequirements/read` replies, and actual `getAuthStatus` requests with
`includeToken:true` and `refreshToken:false`. It also showed unsupported
`config/read` and `experimentalFeature/list`. A method-only observation is
not a real-account or model-usage test.

Only the already guarded `-EnableReviewedLaunchCompatibility` disposable
GitHub profile selects
`auth_status:"primary-no-auth-v0.167.0-alpha.3.2"`. Baseline, startup-only and
default configurations do not select it. Existing repository/branch/hosted-VM,
signed Desktop package/version/hash and both exact backend digest gates remain
unchanged. Both homes are freshly created, no credential environment is
inherited, and both native providers use `requires_openai_auth=false` with
loopback discard endpoints. No user account or keyring is imported.

The bridge forwards the actual permitted params to the primary only, including
`includeToken:true`; it does not rewrite the request to false. Explicit
refresh is rejected before dispatch. Success is accepted only when the actual
native result is exactly `authMethod:null, authToken:null,
requiresOpenaiAuth:false`. A token, any extra/malformed field or native error
is a failure, never converted into this triple. See the
[bridge contract](app-server-bridge.md#exact-0167-ci-only-no-auth-status) for admission, pinning and envelope
checks.

This matches the official handler's genuine no-auth-provider branch,
[before token extraction](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/account_processor.rs#L1097-L1123).
It means that this fixture's provider does not require OpenAI authentication;
it does not mean that a user is logged in. The bridge cannot certify that an
arbitrary backend is credential-free. A response validator prevents onward
token relay, not earlier receipt if someone misuses the mode with credentials.
Native initialization and config refresh can access auth/config sources before
this call. This CI mode is not authorization or guidance to use real accounts,
credential stores, external auth commands or production services.

The runtime gate consumes the full already-sanitized metadata snapshots, never
auth result bodies. It requires every observed status request to have a
correlated validated response, including at least one actual
`includeToken:true` request. Errors, outstanding replies, reused IDs or
correlation saturation fail the diagnostic. The report contains counts only.
The evidence depends on the production exact-result validator and describes
only the observed window, not complete process lifetime or future requests.
Offline fixtures cover the profile's default-off boundaries and these
success/failure correlations. Exact-head CI must establish execution results.

The remaining config read is deliberately unsupported: two fixed native
providers do not produce one identical effective config, and native config
serialization can contain provider secrets even without raw layers. No
primary-only config, invented shared defaults or weakened policy is substituted
to make the Desktop continue.

### Genuine feature-page comparison CI gate

The subsequent signed Desktop
[run at 2ab7bcd9](https://github.com/breakingtime126/magpie/actions/runs/42233630209)
observed nine correlated successful `getAuthStatus` exchanges, six preserving
`includeToken:true`. Its actual `experimentalFeature/list` request had valid
native trace and parameter metadata, a non-null uint32 limit, absent/null cursor,
and absent/null thread scope. That feature request was still unsupported at
that source revision. These are observed request shapes, not a successful
feature page or usable GUI.

Only the same explicitly selected `-EnableReviewedLaunchCompatibility` profile
now adds `experimental_feature_list:"compare-v0.167.0-alpha.3.2"`. Baseline,
startup-only and default configurations remain unchanged. The existing exact
signed Desktop `26.1103.3517.0` package gate, backend
`0.167.0-alpha.3.2` version and both native executable digest pins still apply.
There are no additional diagnostic RPCs: the observer forwards Desktop's actual
requests unchanged.

Each accepted unscoped request obtains a fresh native page from both children.
The bridge preserves cursor and limit, including subsequent cursor-present
pages, and succeeds only when both entire validated native pages match. It does
not merge a feature catalog, replace a native error with an empty page, invent
shared flags, return a primary-only page, or enable features. A non-null
`threadId` remains unsupported. See the
[bridge reference](app-server-bridge.md) for the production admission, page
validation and comparison contract.

`Get-DesktopFeatureListEvidence` consumes the full already-sanitized
`$allEvents` snapshot, not the 256-record report preview or raw RPC bodies.
It requires actual, valid unscoped requests and correlated successful replies,
including at least one limit-present request. It checks native boolean shape
metadata, valid optional trace metadata and per-process correlation. Errors,
missing flags, reused correlations, duplicate/saturated correlation records
from any method, unmatched replies and outstanding requests fail the gate.
Cursor-present requests remain admissible and must have their own replies.
Only request/reply counts and limit/cursor-presence counts are reported, never
page bodies, cursor values, numeric limit values, thread IDs or trace carriers.

Observer success alone cannot independently establish equal native pages. These
counts rely on the enabled production whole-page validator and cover only the
observed snapshot, not the complete process lifetime or future requests.
Offline fixtures exercise multiple processes, out-of-order replies, optional
trace, pagination presence, invalid/missing shapes, wrong methods/directions/IDs,
native errors, ambiguous correlation and failures after the preview cutoff.
Existing auth evidence semantics and shared sanitizers are unchanged. New
fixtures and actual feature success require exact-head CI evidence; the earlier
run above is not verification of the new mode.

The same two fresh empty homes, no inherited credential environment, no imported
account/keyring and loopback discard model endpoints remain in force. This is
genuine read-only feature-page compatibility in the disposable fixture, not a
real-account or inference test, and it makes no zero-egress claim.
`config/read`, `project/list` and `fs/readFile` remain unsupported. In
particular, different native provider configurations are not replaced with
invented shared configuration. GUI usability and complete Desktop compatibility
remain unverified.

### Optional full-file read-caller syntax metadata

Under the existing `-InspectStaticLauncher` opt-in, a separate captured Node
process runs
[`inspect-desktop-read-callers.mjs`](../../build/inspect-desktop-read-callers.mjs)
after the existing public catalog has passed its original signed-MSIX provenance
check. It receives only the verified installation root and that independently
verified ASAR digest. The reader requires the installed archive to match before
analyzing selected complete `.vite/build/*.js` files. It neither executes
package code nor reads any target of a `fs/readFile` expression.

This optional pass has separate limits: 16 MiB per source, 32 MiB combined
source bytes, 2,048 selected files, four million parser tokens, two million
allocated nodes and two million visited nodes, depth 512, 30 seconds of shared
analysis time, 128 calls across all sources and a 64 KiB report. A separate
Node heap cap is 1,024 MiB and the process timeout is 60 seconds. The original
catalog, parser and observer budgets are unchanged. Both streams are captured;
stderr and raw exceptions are discarded. Timeout, unexpected output, invalid
metadata or a failure to establish provenance is unavailable evidence, never
a finding that there are no callers. Optional failures do not bypass earlier
mandatory checks or change Desktop launch/RPC behavior.

PowerShell validates canonical JSON, exact schemas, fixed method/field/marker
vocabularies, integer bounds, aggregate call limits, distinct ordered source
references and offsets, proof flags fixed to false, and the trusted archive
digest. It publishes only a sanitized projection in
`static-read-callers-metadata.json` and the report: public source digests with
ordinal references, bounded counts/offsets, fixed classifications and public
response-field components. Source filenames, source text, actual paths,
identifiers, values and raw errors are not published.

The only candidate methods are `config/read`, `experimentalFeature/list`,
`project/list` and `fs/readFile`. A literal first argument is a positive
syntax fact, not an established RPC transport binding. Response-member facts
cover immediate awaits and restricted same-block const references only; wrapper
calls, aliases, callbacks and broader consumer flow remain unresolved. Mutations
can affect values, so even a recognized response-member expression does not prove
its runtime value. Path classifications and fixed filename markers establish
neither an actual resolved path/root nor authority to read one. The explicit
`runtimeBindingProven`, `responseValueProven` and `pathAuthorityProven` flags
remain false.

A wholly parsed source may contribute positive facts. Syntax or local
analysis limits omit that whole source and mark the package report partial;
global bounds, read/provenance failures and unknown failures discard the whole
report. Complete means complete parsing within the selected source scope, not
complete consumer coverage. Zero reported facts are never proof of absence.
Offline PowerShell fixtures cover schema, bounds, digest mismatch, incomplete
coverage, fixed false proof flags, output framing/status, redaction and aggregate
call limits. Execution of the new fixtures and the real optional package pass
still require exact-head CI. This diagnostic enables no auth/config route,
changes no defaults and introduces no additional Desktop launch or request.
