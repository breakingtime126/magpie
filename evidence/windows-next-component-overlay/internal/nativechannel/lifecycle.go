package nativechannel

// Done closes after serving has stopped and admitted handlers have returned.
// Owners can observe unexpected session completion without discovering another
// endpoint. Close still supplies the actual shutdown error and bounded wait.
func (s *Session) Done() <-chan struct{} { return s.done }
