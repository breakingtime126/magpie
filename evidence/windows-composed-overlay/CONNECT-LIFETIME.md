# Staged Connect lifetime ownership

This evidence overlay extends the already tested native host/runner components with a window-scoped nativehost.Manager. It is not wired into the product GUI; the original Connect callback remains unavailable until the runtime and authorized supplier catalog contract is complete.

The Manager keeps host ownership separate from nativeconnect.Controller. A request cancellation stops preparation but does not become the lifetime of a selected launch generation. Owners are registered before their Plan can enter the selection transaction. Repeated Connect reuses only an unconsumed, valid generation with a matching configured journal. Stale or consumed generations cannot be reused.

A nil catalog callback returns the missing-catalog error without opening selection storage or creating directories. A provided callback must verify runtime provenance and freshness, supplier metadata and the relevant authorization. Config.Validate is not that proof. The constructor requires an existing separate Magpie-owned root and the actual gateway.Server; a URL cannot grant ownership.

On transaction failure, cleanup is allowed only after inspection proves no journal or recovery uncertainty. Otherwise ownership is retained for explicit recovery. Disconnect restores the exact journal-matching selection but does not revoke a generation, since a native launch can race selection restoration. Window Close revokes every owner and uses a bounded join/drain. A cleanup timeout retains ownership for another Close attempt; it is never reported as a successful drain. Historical errors returned by Host.Close are conservatively treated the same way.

Ten manager tests include an actual nativeconnect.Controller with inert authored launcher/profile files and an in-memory selection store, plus synthetic failure, cancellation, concurrent/repeated Connect, retention and retry states. No registry, official native binary, account, model data, or supplier request is used by these tests. The existing composed adapter and separate Relisten fixtures remain required in the same Windows run.

Local checks at publication: gofmt clean, Windows compilation and vet recorded separately in the commit body. Windows execution is required; compiling is not a Windows pass. These results do not establish original-GUI acceptance, installed-runtime compatibility, supplier routing, real chat or restored Desktop starts.
