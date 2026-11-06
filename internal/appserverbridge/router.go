package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type modelRoute struct{ backend, model string }
type pending struct {
	group   *request
	backend string
}
type request struct {
	id                  json.RawMessage
	key, method, thread string
	params              object
	remaining           int
	results             map[string][]json.RawMessage
	seen                map[string]map[string]bool
	failed              bool
	size, bytes         int
}
type serverCall struct {
	backend, key string
	id           json.RawMessage
	answered     bool
}
type page struct {
	method        string
	data          []json.RawMessage
	offset, limit int
}

// Router owns the state of one client connection. Call it from one event
// loop, never concurrently. Losing the connection loses its ownership map;
// a new connection must discover saved threads with thread/list again.
type Router struct {
	configRequirementsRead bool

	auth authCompatibility

	backends           []Backend
	byID               map[string]Backend
	phase              string
	seq                uint64
	activeBytes        int
	pending            map[string]pending
	active             map[string]bool
	models             map[string]modelRoute
	defaultModel       string
	threads            map[string]string
	turns              map[string]bool
	calls              map[string]serverCall
	reverseCalls       map[string]string
	pages              map[string]page
	staleCalls         map[string]bool
	staleOrder         []string
	staleResponses     map[string]string
	staleResponseOrder []string
}

// NewRouter does not start processes, read credentials, or touch the network.
func NewRouter(backends []Backend) (*Router, error) {
	if len(backends) != 2 {
		return nil, errors.New("the bridge requires exactly two backends")
	}
	r := &Router{backends: backends, byID: map[string]Backend{}, phase: "cold", pending: map[string]pending{}, active: map[string]bool{}, models: map[string]modelRoute{}, threads: map[string]string{}, turns: map[string]bool{}, calls: map[string]serverCall{}, reverseCalls: map[string]string{}, pages: map[string]page{}, staleCalls: map[string]bool{}, staleResponses: map[string]string{}}
	for _, b := range backends {
		if b.ID == "" || b.Provider == "" || strings.Contains(b.ID, "/") || r.byID[b.ID].ID != "" {
			return nil, errors.New("backend identifiers and providers must be explicit and distinct")
		}
		r.byID[b.ID] = b
	}
	return r, nil
}
func (r *Router) next(prefix string) string {
	r.seq++
	return fmt.Sprintf("magpie:%s:%d", prefix, r.seq)
}
func alias(backend, model string) string { return "magpie/" + backend + "/" + model }
func (r *Router) send(g *request, backend string, params object) Delivery {
	id := r.next("request")
	r.pending["s:"+id] = pending{g, backend}
	return deliver(backend, object{"id": raw(id), "method": raw(g.method), "params": raw(params)})
}
func (r *Router) end(g *request) {
	if r.active[g.key] {
		delete(r.active, g.key)
		r.activeBytes -= g.size
	}
}
func (r *Router) finish(g *request, result object) []Delivery {
	r.end(g)
	return []Delivery{deliver("", object{"id": g.id, "result": raw(result)})}
}
func (r *Router) reject(g *request, code int, message string) []Delivery {
	out := []Delivery{fail(g.id, code, message)}
	r.abandon(g)
	return out
}

// A failed fanout must not retain user payload while a sibling stalls.
// Keep only bounded transport-ID tombstones for late responses.
func (r *Router) abandon(g *request) {
	g.failed = true
	r.end(g)
	for key, p := range r.pending {
		if p.group == g {
			delete(r.pending, key)
			r.staleResponses[key] = p.backend
			r.staleResponseOrder = append(r.staleResponseOrder, key)
		}
	}
	for len(r.staleResponseOrder) > 256 {
		delete(r.staleResponses, r.staleResponseOrder[0])
		r.staleResponseOrder = r.staleResponseOrder[1:]
	}
	g.params = nil
	g.results = nil
	g.seen = nil
}

// Client accepts one client JSONL message. Unsupported operations fail
// locally, rather than guessing a backend or broadcasting user input.
func (r *Router) Client(line []byte) ([]Delivery, error) {
	m, err := parseObject(line)
	if err != nil {
		return []Delivery{fail(nil, -32700, "invalid JSONL message")}, nil
	}
	method := text(m, "method")
	if method == "" {
		return r.clientResponse(m)
	}
	if !exists(m, "id") {
		if method != "initialized" || r.phase != "initialized" {
			return nil, errors.New("unexpected client notification")
		}
		r.phase = "ready"
		out := []Delivery{}
		for _, b := range r.backends {
			out = append(out, deliver(b.ID, m))
		}
		return out, nil
	}
	key, err := idKey(m["id"])
	if err != nil {
		return []Delivery{fail(m["id"], -32600, err.Error())}, nil
	}
	if r.active[key] {
		return nil, errors.New("duplicate active client request id")
	}
	p, err := child(m, "params")
	if err != nil {
		return []Delivery{fail(m["id"], -32602, "params must be an object")}, nil
	}
	if err := validateParams(method, p); err != nil {
		return []Delivery{fail(m["id"], -32602, err.Error())}, nil
	}
	g := &request{size: len(line), id: m["id"], key: key, method: method, params: p, results: map[string][]json.RawMessage{}, seen: map[string]map[string]bool{}}
	if len(r.active) >= 128 || r.activeBytes+len(line) > 32<<20 {
		return []Delivery{fail(g.id, -32000, "bridge request limit reached")}, nil
	}
	if method == "initialize" {
		caps, e := child(p, "capabilities")
		if e != nil {
			return []Delivery{fail(g.id, -32602, "invalid capabilities")}, nil
		}
		if r.phase != "cold" {
			return []Delivery{fail(g.id, -32600, "already initialized")}, nil
		}
		if (r.auth.attestationUnavailable || r.auth.gatewayRead || r.auth.rejectHumanApproval) && validateRequirementsJSON(line) != nil {
			return []Delivery{fail(g.id, -32602, "invalid initialization message")}, nil
		}
		if err := r.auth.negotiate(caps); err != nil {
			return []Delivery{fail(g.id, -32602, "unsupported authentication capability")}, nil
		}
		r.phase = "initializing"
		r.active[key] = true
		r.activeBytes += g.size
		g.remaining = len(r.backends)
		out := []Delivery{}
		for _, b := range r.backends {
			out = append(out, r.send(g, b.ID, p))
		}
		return out, nil
	}
	if r.phase != "ready" {
		return []Delivery{fail(g.id, -32600, "initialize and initialized are required")}, nil
	}
	switch method {
	case "account/gatewayOAuth/read":
		if !r.auth.gatewayRead || !r.auth.gatewayNegotiated {
			return []Delivery{fail(g.id, -32601, "primary gateway OAuth inspection is disabled")}, nil
		}
		if validateRequirementsJSON(line) != nil {
			return []Delivery{fail(g.id, -32602, "invalid gateway OAuth request")}, nil
		}
		r.active[key] = true
		r.activeBytes += g.size
		return []Delivery{r.send(g, r.backends[0].ID, nil)}, nil
	case "configRequirements/read":
		if !r.configRequirementsRead {
			return []Delivery{fail(g.id, -32601, "config requirements inspection is disabled")}, nil
		}
		r.active[key] = true
		r.activeBytes += g.size
		g.remaining = len(r.backends)
		out := []Delivery{}
		for _, b := range r.backends {
			// The pinned official request is Option<()>: absent/null, not {}.
			// Only an empty client object is adapted to the same no-args call.
			out = append(out, r.send(g, b.ID, nil))
		}
		return out, nil
	case "environment/info", "environment/status":
		// Environment IDs belong to each child's registry, not the model
		// catalog or most recently used thread. Only the reserved local ID
		// is safe to inspect on both children; remote info can reconnect.
		if text(p, "environmentId") != "local" {
			return []Delivery{fail(g.id, -32602, "unscoped environment queries support only the shared local environment")}, nil
		}
		r.active[key] = true
		r.activeBytes += g.size
		g.remaining = len(r.backends)
		out := []Delivery{}
		for _, b := range r.backends {
			out = append(out, r.send(g, b.ID, p))
		}
		return out, nil
	case "model/list", "thread/list", "thread/loaded/list":
		if method == "thread/list" {
			key := text(p, "sortKey")
			if key != "" && key != "created_at" && key != "updated_at" && key != "recency_at" {
				return []Delivery{fail(g.id, -32602, "unsupported thread sort key")}, nil
			}
			direction := text(p, "sortDirection")
			if direction != "" && direction != "asc" && direction != "desc" {
				return []Delivery{fail(g.id, -32602, "unsupported thread sort direction")}, nil
			}
		}
		for _, pending := range r.pending {
			if !pending.group.failed && pending.group.method == method {
				return []Delivery{fail(g.id, -32000, "a list refresh is already in progress")}, nil
			}
		}
		if c := text(p, "cursor"); c != "" {
			return r.page(g, c)
		}
		if exists(p, "cursor") && text(p, "cursor") == "" {
			return []Delivery{fail(g.id, -32602, "invalid bridge cursor")}, nil
		}
		if _, err := pageLimit(p); err != nil {
			return []Delivery{fail(g.id, -32602, err.Error())}, nil
		}
		// Exclusions are backend-specific IDs. Partition them rather than
		// disclose one backend's identifiers to another process.
		if exists(p, "excludedThreadIds") {
			if _, err := stringList(p["excludedThreadIds"]); err != nil {
				return []Delivery{fail(g.id, -32602, err.Error())}, nil
			}
		}
		r.active[key] = true
		r.activeBytes += g.size
		g.remaining = len(r.backends)
		out := []Delivery{}
		for _, b := range r.backends {
			if method == "model/list" && b.Models != nil {
				for _, row := range b.Models {
					g.bytes += len(row)
				}
				if g.bytes > 16<<20 {
					return r.reject(g, -32000, "combined list exceeds bridge size limit"), nil
				}
				g.results[b.ID] = b.Models
				g.remaining--
				continue
			}
			params := clone(p)
			delete(params, "cursor")
			params["limit"] = raw(100)
			if exists(p, "excludedThreadIds") {
				ids, _ := stringList(p["excludedThreadIds"])
				own := []string{}
				for _, id := range ids {
					if r.threads[id] == b.ID {
						own = append(own, id)
					}
				}
				params["excludedThreadIds"] = raw(own)
			}
			out = append(out, r.send(g, b.ID, params))
		}
		if g.remaining == 0 {
			return r.listResult(g)
		}
		return out, nil
	case "thread/start":
		model, e := fieldString(p, "model")
		if e != nil {
			return []Delivery{fail(g.id, -32602, e.Error())}, nil
		}
		if model == "" {
			model = r.defaultModel
		}
		route, ok := r.models[model]
		if !ok {
			return []Delivery{fail(g.id, -32602, "select a model from model/list before starting a thread")}, nil
		}
		if err := r.pin(p, route.backend); err != nil {
			return []Delivery{fail(g.id, -32602, err.Error())}, nil
		}
		p["model"] = raw(route.model)
		p["modelProvider"] = raw(r.byID[route.backend].Provider)
		r.active[key] = true
		r.activeBytes += g.size
		return []Delivery{r.send(g, route.backend, p)}, nil
	case "thread/resume", "thread/fork", "thread/read", "thread/turns/list", "thread/items/list", "thread/archive", "thread/unarchive", "thread/name/set", "thread/compact/start", "thread/rollback", "thread/revert", "thread/unsubscribe", "turn/start", "turn/interrupt", "turn/steer":
		id, e := fieldString(p, "threadId")
		if e != nil || id == "" {
			return []Delivery{fail(g.id, -32602, "threadId is required")}, nil
		}
		backend := r.threads[id]
		if r.byID[backend].ID == "" {
			return []Delivery{fail(g.id, -32602, "unknown or ambiguous thread; list its owning backend's threads first")}, nil
		}
		g.thread = id
		if err := r.pin(p, backend); err != nil {
			return []Delivery{fail(g.id, -32602, err.Error())}, nil
		}
		for _, field := range []string{"turnId", "expectedTurnId", "parentTurnId", "rootTurnId", "lastTurnId"} {
			tid, e := fieldString(p, field)
			if e != nil {
				return []Delivery{fail(g.id, -32602, e.Error())}, nil
			}
			if tid != "" && !r.turns[backend+"\x00"+id+"\x00"+tid] {
				return []Delivery{fail(g.id, -32602, "unknown turn or turn belongs to another thread")}, nil
			}
		}
		if model, e := fieldString(p, "model"); e != nil {
			return []Delivery{fail(g.id, -32602, e.Error())}, nil
		} else if model != "" {
			route, ok := r.models[model]
			if !ok || route.backend != backend {
				return []Delivery{fail(g.id, -32602, "a thread cannot change provider; start a new thread")}, nil
			}
			p["model"] = raw(route.model)
		}
		if method == "thread/resume" || method == "thread/fork" {
			p["modelProvider"] = raw(r.byID[backend].Provider)
		}
		r.active[key] = true
		r.activeBytes += g.size
		return []Delivery{r.send(g, backend, p)}, nil
	case "account/read", "account/rateLimits/read":
		// The first backend owns the client's account view. Account mutation
		// and authentication flows are deliberately not part of this bridge.
		r.active[key] = true
		r.activeBytes += g.size
		return []Delivery{r.send(g, r.backends[0].ID, p)}, nil
	default:
		return []Delivery{fail(g.id, -32601, "method is not supported by this experimental bridge")}, nil
	}
}

func (r *Router) pin(p object, backend string) error {
	for _, field := range []string{"config", "history", "path", "collaborationMode", "parentTurnId", "rootTurnId"} {
		if exists(p, field) {
			return fmt.Errorf("%s overrides are not supported", field)
		}
	}
	provider, e := fieldString(p, "modelProvider")
	if e != nil {
		return e
	}
	if provider != "" && provider != r.byID[backend].Provider {
		return errors.New("modelProvider cannot override the selected backend")
	}
	return nil
}
func (r *Router) clientResponse(m object) ([]Delivery, error) {
	key, e := idKey(m["id"])
	if e != nil {
		return nil, errors.New("invalid client response id")
	}
	if r.staleCalls[key] {
		return nil, nil
	}
	c, ok := r.calls[key]
	if !ok || c.answered {
		return nil, errors.New("unknown or already answered backend request")
	}
	if exists(m, "result") == exists(m, "error") {
		return nil, errors.New("backend request response must contain result or error")
	}
	c.answered = true
	r.calls[key] = c
	m["id"] = c.id
	return []Delivery{deliver(c.backend, m)}, nil
}

// Server accepts a message from the process named backend. A protocol
// violation is fatal: the runner stops both children and never falls back.
func (r *Router) Server(backend string, line []byte) ([]Delivery, error) {
	if r.byID[backend].ID == "" {
		return nil, errors.New("unknown backend")
	}
	m, e := parseObject(line)
	if e != nil {
		return nil, e
	}
	if _, present := m["method"]; present {
		// Inspect raw keys before dispatch: a duplicate method must not hide
		// an authentication request behind a different final method value.
		if validateServerEnvelopeKeys(line) != nil || text(m, "method") == "" {
			return nil, errAuthCompatibility
		}
	}
	if method := text(m, "method"); method != "" {
		// A response must not masquerade as a request/notification and bypass
		// method-specific payload validation or sanitized error handling.
		if _, present := m["result"]; present {
			return nil, errors.New("backend message mixes request and response fields")
		}
		if _, present := m["error"]; present {
			return nil, errors.New("backend message mixes request and response fields")
		}
		if method == "attestation/generate" {
			return r.attestationMessage(backend, line)
		}
		if method == "account/gatewayOAuth/changed" {
			return r.gatewayNotification(backend, line)
		}
		return r.serverMessage(backend, method, m)
	}
	key, e := idKey(m["id"])
	if e != nil {
		return nil, e
	}
	if cancelled, ok := r.staleResponses[key]; ok {
		if cancelled != backend {
			return nil, errors.New("wrong-backend late response")
		}
		delete(r.staleResponses, key)
		return nil, nil
	}
	pending, ok := r.pending[key]
	if !ok || pending.backend != backend {
		return nil, errors.New("unknown or wrong-backend response id")
	}
	delete(r.pending, key)
	g := pending.group
	if g.failed {
		return nil, nil
	}
	if g.method == "account/gatewayOAuth/read" {
		result, err := validateGatewayRead(line, r.backends[0], r.auth.gatewayProviderName)
		if err != nil {
			return r.reject(g, -32000, "primary gateway OAuth readiness unavailable"), nil
		}
		return r.finish(g, result), nil
	}
	if g.method == "configRequirements/read" {
		// Validate the original envelope before a permissive map decoder can
		// hide duplicate keys. Backend error bodies may contain credentials.
		if validateRequirementsJSON(line) != nil || len(m) != 2 || !exists(m, "result") || exists(m, "error") {
			return r.reject(g, -32000, "shared config requirements unavailable"), nil
		}
		result, err := validateConfigRequirementsResponse(m["result"])
		if err != nil {
			return r.reject(g, -32000, "shared config requirements unavailable"), nil
		}
		encoded := raw(result)
		g.bytes += len(encoded)
		if g.bytes > 64<<10 {
			return r.reject(g, -32000, "shared config requirements exceed bridge size limit"), nil
		}
		g.results[backend] = []json.RawMessage{encoded}
		g.remaining--
		if g.remaining != 0 {
			return nil, nil
		}
		first := g.results[r.backends[0].ID][0]
		second := g.results[r.backends[1].ID][0]
		if !sameJSON(first, second) {
			return r.reject(g, -32000, "config requirements differ between backends"), nil
		}
		accepted, _ := parseObject(first)
		return r.finish(g, accepted), nil
	}
	if exists(m, "error") && exists(m, "result") {
		return nil, errors.New("backend response has both result and error")
	}
	if exists(m, "error") {
		m["id"] = g.id
		out := []Delivery{deliver("", m)}
		r.abandon(g)
		return out, nil
	}
	result, e := child(m, "result")
	if e != nil || !exists(m, "result") {
		return nil, errors.New("backend response has no result object")
	}
	switch g.method {
	case "environment/info", "environment/status":
		encoded := raw(result)
		g.bytes += len(encoded)
		if g.bytes > 64<<10 {
			return r.reject(g, -32000, "combined environment response exceeds bridge size limit"), nil
		}
		if err := validateEnvironmentResult(g.method, result); err != nil {
			return nil, err
		}
		g.results[backend] = []json.RawMessage{encoded}
		g.remaining--
		if g.remaining != 0 {
			return nil, nil
		}
		first := g.results[r.backends[0].ID][0]
		second := g.results[r.backends[1].ID][0]
		if !sameJSON(first, second) {
			return r.reject(g, -32000, "local environment differs between backends; no shared environment result is available"), nil
		}
		original, _ := parseObject(first)
		return r.finish(g, original), nil
	case "initialize":
		g.results[backend] = []json.RawMessage{raw(result)}
		g.remaining--
		if g.remaining != 0 {
			return nil, nil
		}
		r.phase = "initialized"
		first, _ := parseObject(g.results[r.backends[0].ID][0])
		return r.finish(g, first), nil
	case "model/list", "thread/list", "thread/loaded/list":
		var data []json.RawMessage
		if json.Unmarshal(result["data"], &data) != nil {
			return nil, errors.New("invalid backend list data")
		}
		for _, row := range data {
			g.bytes += len(row)
		}
		if g.bytes > 16<<20 {
			return r.reject(g, -32000, "combined list exceeds bridge size limit"), nil
		}
		g.results[backend] = append(g.results[backend], data...)
		if len(g.results[backend]) > 10000 {
			return nil, errors.New("backend catalog exceeds bridge limit")
		}
		cursor, e := fieldString(result, "nextCursor")
		if e != nil {
			return nil, e
		}
		if cursor != "" {
			if g.seen[backend] == nil {
				g.seen[backend] = map[string]bool{}
			}
			if g.seen[backend][cursor] || len(g.seen[backend]) >= 100 {
				return nil, errors.New("backend pagination did not finish")
			}
			g.seen[backend][cursor] = true
			p := clone(g.params)
			p["cursor"] = raw(cursor)
			p["limit"] = raw(100)
			if exists(p, "excludedThreadIds") {
				ids, _ := stringList(p["excludedThreadIds"])
				own := []string{}
				for _, id := range ids {
					if r.threads[id] == backend {
						own = append(own, id)
					}
				}
				p["excludedThreadIds"] = raw(own)
			}
			return []Delivery{r.send(g, backend, p)}, nil
		}
		g.remaining--
		if g.remaining != 0 {
			return nil, nil
		}
		return r.listResult(g)
	default:
		if exists(result, "thread") {
			thread, e := child(result, "thread")
			if e != nil {
				return nil, e
			}
			if g.thread != "" && g.method != "thread/fork" && text(thread, "id") != g.thread {
				return nil, errors.New("backend returned a different thread")
			}
			if e := r.bindThread(backend, thread); e != nil {
				return nil, e
			}
			r.rewriteThread(backend, thread)
			result["thread"] = raw(thread)
		}
		if exists(result, "turn") {
			turn, e := child(result, "turn")
			if e != nil {
				return nil, e
			}
			if e := r.bindTurn(backend, g.thread, text(turn, "id")); e != nil {
				return nil, e
			}
		}
		if g.method == "thread/turns/list" {
			var turns []object
			if json.Unmarshal(result["data"], &turns) != nil {
				return nil, errors.New("invalid turns list")
			}
			for _, t := range turns {
				if e := r.bindTurn(backend, g.thread, text(t, "id")); e != nil {
					return nil, e
				}
			}
		}
		if exists(result, "modelProvider") && text(result, "modelProvider") != r.byID[backend].Provider {
			return nil, errors.New("backend changed provider")
		}
		r.rewriteModel(backend, result, "model")
		return r.finish(g, result), nil
	}
}

func sameJSON(a, b []byte) bool {
	decode := func(encoded []byte) any {
		var value any
		d := json.NewDecoder(bytes.NewReader(encoded))
		d.UseNumber()
		_ = d.Decode(&value) // Both inputs already passed parseObject.
		return value
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func validateEnvironmentResult(method string, result object) error {
	if method == "environment/status" {
		switch text(result, "status") {
		case "ready", "pending", "disconnected", "unknown":
		default:
			return errors.New("invalid environment status")
		}
		if value, ok := result["error"]; ok {
			var s string
			if !exists(result, "error") || json.Unmarshal(value, &s) != nil {
				return errors.New("invalid environment status error")
			}
		}
		return nil
	}
	shell, err := child(result, "shell")
	if err != nil {
		return errors.New("invalid environment shell info")
	}
	for _, field := range []string{"name", "path"} {
		var s string
		if !exists(shell, field) || json.Unmarshal(shell[field], &s) != nil {
			return errors.New("invalid environment shell info")
		}
	}
	value, ok := result["cwd"]
	var cwd string
	if !ok || (exists(result, "cwd") && json.Unmarshal(value, &cwd) != nil) {
		return errors.New("invalid environment cwd info")
	}
	return nil
}

func (r *Router) bindThread(backend string, t object) error {
	id := text(t, "id")
	if id == "" {
		return errors.New("backend thread has no id")
	}
	if provider := text(t, "modelProvider"); provider != r.byID[backend].Provider {
		return errors.New("backend returned a thread from another provider")
	}
	if old := r.threads[id]; old != "" && old != backend {
		r.threads[id] = "!"
		return errors.New("thread id collision across backends")
	}
	for _, field := range []string{"parentThreadId", "forkedFromId"} {
		if v := text(t, field); v != "" {
			if old := r.threads[v]; old != "" && old != backend {
				return errors.New("thread refers to a foreign parent")
			}
			r.threads[v] = backend
		}
	}
	r.threads[id] = backend
	var turns []object
	if exists(t, "turns") {
		if json.Unmarshal(t["turns"], &turns) != nil {
			return errors.New("invalid thread turns")
		}
		for _, turn := range turns {
			if e := r.bindTurn(backend, id, text(turn, "id")); e != nil {
				return e
			}
		}
	}
	return nil
}
func (r *Router) bindTurn(backend, thread, id string) error {
	if r.threads[thread] != backend || id == "" {
		return errors.New("turn has unknown thread or id")
	}
	r.turns[backend+"\x00"+thread+"\x00"+id] = true
	return nil
}
func (r *Router) rewriteModel(backend string, o object, key string) {
	if s := text(o, key); s != "" {
		o[key] = raw(alias(backend, s))
	}
}
func (r *Router) rewriteThread(backend string, t object) { r.rewriteModel(backend, t, "model") }

func (r *Router) serverMessage(backend, method string, m object) ([]Delivery, error) {
	p, e := child(m, "params")
	if e != nil {
		return nil, e
	}
	if method == "thread/started" {
		t, e := child(p, "thread")
		if e != nil {
			return nil, e
		}
		id := text(t, "id")
		allowed := r.threads[id] == backend
		if parent := text(t, "parentThreadId"); parent != "" {
			allowed = r.threads[parent] == backend
		}
		for _, pending := range r.pending {
			if pending.backend == backend && !pending.group.failed && (pending.group.method == "thread/start" || pending.group.method == "thread/fork" || pending.group.method == "thread/resume") {
				allowed = true
			}
		}
		if !allowed {
			return nil, errors.New("unsolicited unknown thread")
		}
		if e := r.bindThread(backend, t); e != nil {
			return nil, e
		}
		r.rewriteThread(backend, t)
		p["thread"] = raw(t)
	} else {
		id := text(p, "threadId")
		if id != "" {
			if r.threads[id] != backend {
				return nil, errors.New("notification or request belongs to an unknown or foreign thread")
			}
		} else {
			if exists(m, "id") {
				return nil, errors.New("unscoped backend request is unsupported")
			}
			// Only the primary account view is visible. Never expose a second
			// account or allow an unscoped event to establish thread ownership.
			if backend == r.backends[0].ID && (method == "account/updated" || method == "account/rateLimits/updated") {
				return []Delivery{deliver("", m)}, nil
			}
			return nil, nil
		}
		if method == "turn/started" || method == "turn/completed" {
			turn, e := child(p, "turn")
			if e != nil {
				return nil, e
			}
			if e := r.bindTurn(backend, id, text(turn, "id")); e != nil {
				return nil, e
			}
		}
		if tid := text(p, "turnId"); tid != "" {
			if e := r.bindTurn(backend, id, tid); e != nil {
				return nil, e
			}
		}
		if method == "thread/settings/updated" {
			settings, e := child(p, "threadSettings")
			if e != nil {
				return nil, e
			}
			r.rewriteModel(backend, settings, "model")
			p["threadSettings"] = raw(settings)
		}
		if method == "model/rerouted" {
			r.rewriteModel(backend, p, "fromModel")
			r.rewriteModel(backend, p, "toModel")
		}
	}
	if method == "serverRequest/resolved" {
		k, e := idKey(p["requestId"])
		if e != nil {
			return nil, e
		}
		translated, ok := r.reverseCalls[backend+"\x00"+k]
		if !ok {
			return nil, errors.New("unknown resolved backend request")
		}
		p["requestId"] = raw(translated)
		delete(r.reverseCalls, backend+"\x00"+k)
		delete(r.calls, "s:"+translated)
		r.staleCalls["s:"+translated] = true
		r.staleOrder = append(r.staleOrder, "s:"+translated)
		if len(r.staleOrder) > 256 {
			delete(r.staleCalls, r.staleOrder[0])
			r.staleOrder = r.staleOrder[1:]
		}
	}
	m["params"] = raw(p)
	if exists(m, "id") {
		key, e := idKey(m["id"])
		if e != nil {
			return nil, e
		}
		scoped := backend + "\x00" + key
		if old := r.reverseCalls[scoped]; old != "" {
			if !r.calls["s:"+old].answered {
				return nil, errors.New("duplicate backend request id")
			}
			delete(r.calls, "s:"+old)
		}
		if len(r.calls) >= 4096 {
			return nil, errors.New("too many outstanding backend requests")
		}
		translated := r.next("server")
		r.calls["s:"+translated] = serverCall{backend, key, m["id"], false}
		r.reverseCalls[scoped] = translated
		m["id"] = raw(translated)
	}
	return []Delivery{deliver("", m)}, nil
}

func pageLimit(p object) (int, error) {
	if !exists(p, "limit") {
		return 100, nil
	}
	var n int
	if json.Unmarshal(p["limit"], &n) != nil || n < 1 || n > 1000 {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return n, nil
}
func (r *Router) page(g *request, cursor string) ([]Delivery, error) {
	p, ok := r.pages[cursor]
	if !ok || p.method != g.method {
		return []Delivery{fail(g.id, -32602, "unknown or expired bridge cursor")}, nil
	}
	delete(r.pages, cursor)
	return r.emitPage(g, p.data, p.offset, p.limit), nil
}
func (r *Router) emitPage(g *request, data []json.RawMessage, offset, limit int) []Delivery {
	end := min(offset+limit, len(data))
	var next any
	if end < len(data) {
		token := r.next("page")
		r.pages[token] = page{g.method, data, end, limit}
		next = token
	}
	for snapshotBytes(r.pages) > 32<<20 || len(r.pages) > 64 { // Pagination holds bounded snapshots, never credentials.
		oldest := ""
		num := uint64(^uint64(0))
		for k := range r.pages {
			n, _ := strconv.ParseUint(strings.TrimPrefix(k, "magpie:page:"), 10, 64)
			if n < num {
				oldest = k
				num = n
			}
		}
		delete(r.pages, oldest)
	}
	return r.finish(g, object{"data": raw(data[offset:end]), "nextCursor": raw(next)})
}
func (r *Router) listResult(g *request) ([]Delivery, error) {
	data := []json.RawMessage{}
	routes := map[string]modelRoute{}
	defaultModel := ""
	seen := map[string]bool{}
	for _, b := range r.backends {
		for _, encoded := range g.results[b.ID] {
			if g.method == "thread/loaded/list" {
				var id string
				if json.Unmarshal(encoded, &id) != nil || id == "" {
					return nil, errors.New("invalid loaded thread id")
				}
				if old := r.threads[id]; old != "" && old != b.ID {
					r.threads[id] = "!"
					return nil, errors.New("loaded thread id collision")
				}
				r.threads[id] = b.ID
				if !seen[id] {
					data = append(data, raw(id))
					seen[id] = true
				}
				continue
			}
			item, e := parseObject(encoded)
			if e != nil {
				return nil, e
			}
			if g.method == "model/list" {
				if e := validateModel(item); e != nil {
					return nil, e
				}
				model := text(item, "model")
				if model == "" || text(item, "id") == "" {
					return nil, errors.New("model descriptor requires id and model")
				}
				var hidden, include bool
				_ = json.Unmarshal(item["hidden"], &hidden)
				_ = json.Unmarshal(g.params["includeHidden"], &include)
				if hidden && !include {
					continue
				}
				a := alias(b.ID, model)
				if _, ok := routes[a]; ok {
					return nil, errors.New("duplicate backend model")
				}
				routes[a] = modelRoute{b.ID, model}
				item["id"] = raw(a)
				item["model"] = raw(a)
				if name := text(item, "displayName"); name != "" {
					item["displayName"] = raw(name + " [" + b.ID + "]")
				}
				r.rewriteModel(b.ID, item, "upgrade")
				if exists(item, "upgradeInfo") {
					u, e := child(item, "upgradeInfo")
					if e != nil {
						return nil, e
					}
					r.rewriteModel(b.ID, u, "model")
					item["upgradeInfo"] = raw(u)
				}
				var isDefault bool
				_ = json.Unmarshal(item["isDefault"], &isDefault)
				if isDefault && defaultModel == "" {
					defaultModel = a
				} else {
					item["isDefault"] = raw(false)
				}

			} else {
				if e := r.bindThread(b.ID, item); e != nil {
					return nil, e
				}
				r.rewriteThread(b.ID, item)
				id := text(item, "id")
				if exists(g.params, "excludedThreadIds") {
					ids, _ := stringList(g.params["excludedThreadIds"])
					excluded := false
					for _, excludedID := range ids {
						if excludedID == id {
							excluded = true
							break
						}
					}
					if excluded {
						continue
					}
				}
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			data = append(data, raw(item))
		}
	}
	if g.method == "model/list" {
		r.models = routes
		r.defaultModel = defaultModel
	} else if g.method == "thread/list" {
		sortKey := text(g.params, "sortKey")
		if sortKey == "" {
			sortKey = "created_at"
		}
		fields := map[string]string{"created_at": "createdAt", "updated_at": "updatedAt", "recency_at": "recencyAt"}
		field := fields[sortKey]
		if field == "" {
			return r.reject(g, -32602, "unsupported thread sort key"), nil
		}
		ascending := text(g.params, "sortDirection") == "asc"
		sort.SliceStable(data, func(i, j int) bool {
			a, _ := parseObject(data[i])
			b, _ := parseObject(data[j])
			var x, y int64
			_ = json.Unmarshal(a[field], &x)
			_ = json.Unmarshal(b[field], &y)
			if x == y {
				return text(a, "id") < text(b, "id")
			}
			if ascending {
				return x < y
			}
			return x > y
		})
	}
	limit, _ := pageLimit(g.params)
	return r.emitPage(g, data, 0, limit), nil
}

func snapshotBytes(pages map[string]page) int {
	total := 0
	for _, p := range pages {
		for _, row := range p.data {
			total += len(row)
		}
	}
	return total
}
