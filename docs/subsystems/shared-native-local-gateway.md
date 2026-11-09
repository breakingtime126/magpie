# Shared native local gateway caller label

This is an optional extension of the pinned shared-native route, not a new
account store or authentication mechanism. Set shared_native_route.gateway_auth
to magpie-loopback-placeholder-v1 and leave the secondary inherit_env empty.
The existing dedicated gateway_env_key still names the native provider's
environment variable. The adapter constructs it only in the secondary child's
environment with the public value magpie-codex. The primary receives neither
that value nor a gateway credential.

The existing gateway defines TokenFor("codex") as this public caller label
(gateway.go:52-59). Its ordinary loopback routes do not require a secret;
lanGuard still enforces its allowlist, forwarding, browser-origin, and managed
key rules. Nothing here changes or bypasses those rules. A user who assigned
this value to a managed key may still encounter that key's restrictions.

The adapter accepts only the existing exact http://127.0.0.1:port/v1 route.
It neither probes nor proves the listener's process identity. Connect must
separately establish that its intended Magpie gateway owns and serves the
configured address. Corporate proxy, CA and NO_PROXY settings are preserved,
not rewritten to make loopback requests succeed. A proxy or forwarding policy
may cause refusal; this mode does not weaken it.

No saved provider token, gateway access-key file, Codex authentication file,
discovery or migration helper is read. No key or caller label is written to
UserEnv or the profile. The profile records only the mode and variable name.
If that environment variable already exists in the supplied parent environment,
even empty or equal to the public label, construction fails rather than silently
overriding a caller's credential choice. Existing inherited-key mode is unchanged
when gateway_auth is omitted.

Tests use inert temporary fixtures and synthetic environment arrays. They cover
secondary-only injection, parent immutability, proxy/CA preservation, existing
value conflicts, unknown modes and unsupported destinations. No native binary,
account, HTTP request or gateway service was run in preparing this change.
Go tests and gofmt require CI; they were not run in this workspace.
