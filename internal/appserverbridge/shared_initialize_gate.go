package appserverbridge

import "errors"

// This gate changes process startup ordering only for the explicit shared-home
// mode. Router remains the protocol authority; no synthetic initialize request
// or response is created. Ordinary isolated backends retain parallel startup.
type sharedInitializeGate struct {
	primary, secondary           string
	queued                       []Delivery
	bytes                        int
	initQueued, released, failed bool
}

func newSharedInitializeGate(cfg Config) *sharedInitializeGate {
	if cfg.SharedNativeRoute == nil {
		return nil
	}
	return &sharedInitializeGate{primary: cfg.Backends[0].ID, secondary: cfg.Backends[1].ID}
}

func (s *sharedInitializeGate) hold(d Delivery) (bool, error) {
	if s == nil || s.released || d.Backend != s.secondary {
		return false, nil
	}
	if s.failed {
		return true, nil
	}
	m, err := parseObject(d.Message)
	method := text(m, "method")
	if err != nil || (method != "initialize" && method != "initialized") ||
		(method == "initialize" && s.initQueued) || len(s.queued) >= 128 ||
		len(d.Message) > (32<<20)-s.bytes {
		return false, errors.New("shared native initialization queue unavailable")
	}
	s.queued = append(s.queued, Delivery{Backend: d.Backend, Message: append([]byte(nil), d.Message...)})
	s.bytes += len(d.Message)
	s.initQueued = s.initQueued || method == "initialize"
	return true, nil
}

// Capture the actual pending group before Server consumes its response ID.
// Do not use diagnostic observation: ordinary builds have no observer.
func (s *sharedInitializeGate) pendingPrimary(r *Router, backend string, line []byte) *request {
	if s == nil || s.released || s.failed || backend != s.primary || r.phase != "initializing" {
		return nil
	}
	m, err := parseObject(line)
	if err != nil {
		return nil
	}
	key, err := idKey(m["id"])
	if err != nil {
		return nil
	}
	p, ok := r.pending[key]
	if !ok || p.backend != backend || p.group.method != "initialize" {
		return nil
	}
	return p.group
}

// Called only AFTER Router.Server returned no protocol error. An accepted
// primary initialization stores its validated result; an error or malformed
// result never provides that evidence. Preserve early initialized ordering.
func (s *sharedInitializeGate) validated(g *request) ([]Delivery, bool) {
	if s == nil || s.released || s.failed || g == nil {
		return nil, false
	}
	if g.failed {
		s.queued = nil
		s.bytes = 0
		s.failed = true
		return nil, false
	}
	if !s.initQueued || g.method != "initialize" || len(g.results[s.primary]) != 1 || g.remaining != 1 {
		return nil, false
	}
	s.released = true
	queued := s.queued
	s.queued = nil
	s.bytes = 0
	return queued, true
}
