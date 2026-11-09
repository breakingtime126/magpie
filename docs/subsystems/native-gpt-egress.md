# Native Codex build: old built-in GPT accounts are inactive

This boundary applies only to the `nativecodex` build. It is a prerequisite for
letting official Codex own ChatGPT authentication and inference, not proof that
the same-window bridge is complete. No new account store or login UI is added.
Ordinary builds retain the prior behavior.

The identity test is exactly the built-in account agents `codex` and
`chatgpt-api`, never a model name such as GPT. Native auth reads, automatic
remembering, login/import/switch/forget/enable, saved token acquisition and the
old account signing/model-fetch paths reject or omit these accounts. The
background `leftAlone` predicate also suppresses their usual work. Low-level
Codex signing, refresh, quota/reset and ChatGPT API token/revocation calls reject
before sending, including callers that already hold a synthetic or cached token.
The Codex hold-reading exception receives no active login through this boundary.

Old saved records are not deleted, migrated or imported from native auth. When
another provider saves logins.json, writeLogins reads the actual previous file
and preserves its protected JSON objects, including unknown fields and repeated
records. A proposed addition, deletion or change to a protected typed record fails
the save; it is not silently restored while reporting success. An unreadable or malformed old file fails the write. JSON formatting and
placement relative to other providers may change; protected records retain
relative order. A concurrent protected-record change observed before the write also refuses
the save instead of resurrecting old state. Existing locking/concurrent-writer
guarantees are unchanged,
not an OS security or cross-process transaction boundary. In-memory account
caches may still contain old records for normal whole-file handling; this is not
a promise that the process never reads its own saved account file.

API-key AIHub, other providers and third-party plugins are unchanged. This is
NOT a universal OpenAI hostname/network firewall: a user-defined API-key provider
or plugin can still target OpenAI. It does not alter proxy/CA settings, force
native login/logout, or grant credential isolation between same-user processes.

## Verification

`go test -tags nogui ./internal/provider -run '^TestNativeGPT'`
checks ordinary build behavior and the pure serialization boundary.
`go test -tags nogui,nativecodex ./internal/provider -run '^TestNativeGPT'`
checks synthetic file/memory canaries, refusal before transport/sign callbacks,
legacy login/import refusal and preserved saved records. These focused tests
are not substitutes for ordinary full-suite and nativecodex GUI integration CI.
Existing ordinary account tests intentionally exercise the now-disabled builtins
and are not expected to describe nativecodex account behavior.

No native binary, real account, external service or user device was exercised
while preparing this candidate. Local Go execution was unavailable.

If an old duplicate or legacy account is normalized by existing parsing before
a different account is saved, the preservation check may reject that save. It
never resolves the ambiguity by deleting or normalizing protected records.

## Legacy gateway passthrough

The nativecodex build also rejects the old `CodexPath` handler before body read,
account lookup or WebSocket negotiation, plus the lower `codexUpstreamOn`
passthrough entry used by compaction/fallback routes. This includes a caller's
own Authorization header, which bypasses Provider.Sign in the original gateway.
The old GET `/v1/codex/models` catalog handler is rejected before accessing the
old catalog/account path. Ordinary `/v1/models` and provider-routed AIHub
Responses are not disabled.

Inbound `/v1` WebSocket turns use responses_ws.go's `rs.o.via.ServeHTTP` to
re-enter the HTTP routing path. An own-Codex fallback then reaches codexBackend;
there is no separate direct-own dial in that inbound handler. codex_socket.go
is an outbound RoundTripper and is not globally disabled, because arbitrary
user-defined providers remain outside this builtin boundary. These are source
observations, not a live WebSocket integration result.

Focused gateway tests use nil server state, unread synthetic bodies and a
transport that must never be called. Run `go test -tags nogui,nativecodex
./internal/gateway -run '^TestNativeGPT'` and the ordinary build counterpart.
