# Exact 0.167 native routing acceptance

This CI-only program exercises the compiled production adapter against the
unmodified Windows `codex-cli 0.167.0-alpha.3.2` executable bundled with the
verified Desktop package. It is separate from the retained Linux 0.166 tests.

## Inputs and invocation

Build `go build -trimpath -tags nogui -o control-plane-native.exe ./build/control-plane-native`.

The caller supplies the adapter, official executable, and the exact official
`codex-rs/models-manager/models.json` from commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`.
The harness verifies native SHA-256
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`,
the native version, and catalog Git blob
`489ebe3f2483348641a16160ce8491dd077eabca` before use.
It selects the unchanged gpt-5.5 model record for native catalog loading; it
does not inject bridge model descriptors or invent native model metadata.

Dot-source `invoke.ps1`, then call:

```powershell
Invoke-ControlPlaneNative -HelperExecutable $helper -BridgeExecutable $bridge `
  -CodexExecutable $codex -CatalogPath $catalog `
  -ScratchParent $freshEmptyScratch -HelperHome $freshEmptyHelperHome
```

The two directory inputs must be empty, distinct, and non-overlapping. They
belong to the disposable CI job. Never use a user computer or credential store.
The wrapper does not download anything or start Desktop. The integrating job
owns preparation and deletion of its inputs. It must verify the original
signed package independently.

The wrapper clears the helper environment, suppresses stderr, bounds captured
stdout, and runs the `-supervise` watchdog. The watchdog uses
`internal/proc.StartTreeStrict` for its child and enforces 265 seconds.
The acceptance process enforces 240 seconds and also owns its adapter through
`StartTreeStrict`. Windows Job Object kill-on-close ownership propagates
cleanup when the wrapper terminates its owned watchdog at 270 seconds.
No executable-name or global process kill is used. The summary's
`adapter_clean_exit` means only the adapter exited with status zero after
frontend EOF. `Tree.Wait` sweeps its owned tree, and cleanup may forcibly end
owned descendants; this is not a claim of graceful native-process shutdown.

## Acceptance and boundaries

The logical sequence is initialize (with the actual separate control home),
config/read before catalog resolution, actual native model/list, config/read
with its resolved catalog default, two projectless threads selected by their
alpha/beta aliases, two interleaved assistant-text turns per thread, rejection
of opposite-backend model overrides, thread/read with logical providers, and
a final unchanged routing-default read. Every assistant completion is audited
at the reader boundary through adapter EOF. Exactly four distinct completions
must match the known threads, actual turn IDs and route-specific synthetic
answers. Extra, duplicate, unexplained or wrong-route completions fail even
when no waiter would have consumed them. The assistant count is derived from
that complete audit, not from successful waiter calls.

Native HOME, CODEX_HOME, control_home, and cwd are freshly created and separate.
The control home has no fabricated config.toml. The fixture writes no auth.json,
copies no credentials, uses file credential storage and sets
requires_openai_auth=false for both distinct native providers.
Both model endpoints are numeric loopback HTTP SSE fixtures. Optional Apps,
plugins, cloud skills, updates, and analytics are disabled using ordinary
configuration. No model tool output, command, platform proof, account or
entitlement is synthesized. Every server-initiated RPC is a failure.

The SSE fixture reuses the working event sequence from
`../test-official-runtime.py`: response.created,
response.output_item.done (assistant output_text), and response.completed.
Each endpoint accepts exactly its two expected synthetic prompts, rejects
authentication/cookie/proof headers, unexpected methods/routes, oversized
bodies and cross-provider input, and retains only counts.

The program prints only the fixed summary schema, booleans, bounded counts,
known status/failure labels and the verified binary hash. It never prints
native diagnostics, configs, payloads, IDs, prompts, tokens or private paths.
Native errors and semantic differences fail acceptance; they are not retried
with weaker permissions or replaced with synthetic success.

This proves a narrow native protocol/assistant-text slice only. It does not
establish real account access, provider entitlement, company-policy support,
OS-enforced zero egress, or completed Desktop GUI compatibility. The parent
job keeps this report separate from its Desktop capture.

## Verification

`go test -race ./build/control-plane-native` covers local synthetic fixtures,
environment and input guards, exact pins, strict child lifecycle, unreviewed
RPC/tool rejection, bounded protocol and summary privacy.
The real acceptance is never silently skipped by these tests; it is the
separate explicit Windows CI invocation above.

Source was prepared without an available executor. gofmt, unit/race/vet/build,
the full suite, and exact native runtime acceptance were not run during
authoring. Record the exact integrated head and real CI outcomes before
claiming success.
