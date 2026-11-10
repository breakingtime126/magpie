package nativechannel

// Done closes after serving has stopped and admitted handlers have returned.
// Owners can observe unexpected session completion without discovering another
// endpoint. Close still supplies the actual shutdown error and bounded wait.
func (s *Session) Done() <-chan struct{} { return s.done }

// Revoked closes as soon as this session stops admitting requests and cancels
// their contexts. Unlike Done it does not wait for uncooperative handlers. An
// owner must revoke its lease immediately, then separately wait with Close.
func (s *Session) Revoked() <-chan struct{} { return s.revoked }
