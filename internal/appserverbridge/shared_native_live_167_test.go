package appserverbridge

import "testing"

func sharedLiveHeldThread167(t *testing.T, r *Router, method, backend, thread, requestID string) Delivery {
	t.Helper()
	cwd := t.TempDir()
	m := sharedThreadRequest167(method, backend, cwd)
	m["id"] = raw(requestID)
	p, _ := child(m, "params")
	if method == "thread/resume" {
		p["threadId"] = raw(thread)
	}
	m["params"] = raw(p)
	out := client(t, r, string(raw(m)))
	if len(out) != 1 || out[0].Backend != backend {
		t.Fatal("missing target policy check")
	}
	out = reply(t, r, out[0], controlPlaneNullRequirements)
	if len(out) != 1 || out[0].Backend != backend {
		t.Fatal("missing target config check")
	}
	out = reply(t, r, out[0], string(raw(sharedThreadConfigResult167(t, r, backend, cwd))))
	if len(out) != 1 || out[0].Backend != backend {
		t.Fatal("admitted thread operation not released")
	}
	request, _ := parseObject(out[0].Message)
	if text(request, "method") != method {
		t.Fatal("unexpected released method")
	}
	return out[0]
}

func sharedLiveComplete167(t *testing.T, r *Router, d Delivery, thread string) []Delivery {
	t.Helper()
	return reply(t, r, d, string(raw(object{
		"modelProvider": raw(r.byID[d.Backend].Provider),
		"thread": raw(object{"id": raw(thread), "modelProvider": raw(r.byID[d.Backend].Provider)}),
	})))
}

func sharedLiveInput167(t *testing.T, r *Router, method, thread string, allowed bool) {
	t.Helper()
	params := object{"threadId": raw(thread)}
	if method == "turn/start" {
		params["input"] = raw([]object{{"type": raw("text"), "text": raw("synthetic input")}})
	}
	out := client(t, r, string(raw(object{"id": raw("live-" + method), "method": raw(method), "params": raw(params)})))
	if !allowed {
		expectError(t, out)
		return
	}
	if len(out) != 1 || out[0].Backend != r.threads[thread] {
		t.Fatal("live input did not route to exact admitted owner")
	}
	m, _ := parseObject(out[0].Message)
	if text(m, "method") != method {
		t.Fatal("live input unexpectedly polled configuration")
	}
	reply(t, r, out[0], `{}`)
}

func TestSharedNativeLive167OwnershipReadAndNotificationDoNotAdmit(t *testing.T) {
	r := sharedThreadReady167(t)
	// bindThread is the shared list/read ownership sink, not route admission.
	if err := r.bindThread("aihub", object{"id": raw("known"), "modelProvider": raw(sharedSecondaryProvider167)}); err != nil {
		t.Fatal(err)
	}
	read := client(t, r, `{"id":"read","method":"thread/read","params":{"threadId":"known"}}`)
	sharedLiveComplete167(t, r, read[0], "known")
	server(t, r, "aihub", string(raw(object{"method": raw("thread/started"), "params": raw(object{"thread": raw(object{"id": raw("notification-only"), "parentThreadId": raw("known"), "modelProvider": raw(sharedSecondaryProvider167)})})})))
	for _, id := range []string{"known", "notification-only"} {
		for _, method := range []string{"turn/start", "turn/steer", "thread/compact/start", "thread/rollback", "thread/name/set", "thread/unarchive"} {
			sharedLiveInput167(t, r, method, id, false)
		}
	}
	if len(r.sharedNativeLive167) != 0 || len(r.pending) != 0 || len(r.active) != 0 {
		t.Fatal("ownership-only event admitted a live route or retained input")
	}
}

func TestSharedNativeLive167RequiresSuccessfulStartOrResumeResult(t *testing.T) {
	for _, backend := range []string{"openai", "aihub"} {
		for _, method := range []string{"thread/start", "thread/resume"} {
			r := sharedThreadReady167(t)
			r.threads["known"] = backend
			d := sharedLiveHeldThread167(t, r, method, backend, "known", "admit")
			sharedLiveInput167(t, r, "turn/start", "known", false)
			out := sharedLiveComplete167(t, r, d, "known")
			if len(out) != 1 || r.sharedNativeLive167["known"] != backend {
				t.Fatal("successful admitted result did not authorize exact live owner")
			}
			sharedLiveInput167(t, r, "turn/start", "known", true)
			sharedLiveInput167(t, r, "thread/compact/start", "known", true)
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("live input leaked request accounting")
			}
		}
	}
}

func TestSharedNativeLive167ResumeFailureRevokesEarlierAdmission(t *testing.T) {
	for _, failure := range []string{"context", "policy", "native"} {
		r := sharedThreadReady167(t)
		d := sharedLiveHeldThread167(t, r, "thread/start", "aihub", "known", "start")
		sharedLiveComplete167(t, r, d, "known")
		if failure == "context" {
			out := client(t, r, `{"id":"resume","method":"thread/resume","params":{"threadId":"known"}}`)
			expectError(t, out)
		} else if failure == "policy" {
			m := sharedThreadRequest167("thread/resume", "aihub", t.TempDir())
			p, _ := child(m, "params")
			p["threadId"] = raw("known")
			m["params"] = raw(p)
			out := client(t, r, string(raw(m)))
			d := out[0]
			q, _ := parseObject(d.Message)
			out, err := r.Server(d.Backend, raw(object{"id": q["id"], "result": raw(object{"requirements": raw(object{}), "supportsIndependentSpeedModes": raw(true)})}))
			if err == nil || len(out) != 0 {
				t.Fatal("observed policy drift did not stop the connection")
			}
		} else {
			d = sharedLiveHeldThread167(t, r, "thread/resume", "aihub", "known", "resume")
			m, _ := parseObject(d.Message)
			out := server(t, r, d.Backend, string(raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("synthetic failure")})})))
			expectError(t, out)
		}
		sharedLiveInput167(t, r, "turn/start", "known", false)
		if len(r.sharedNativeLive167) != 0 || len(r.active) != 0 || len(r.pending) != 0 {
			t.Fatal("failed resume left earlier live authority")
		}
	}
}

func TestSharedNativeLive167LifecycleRevokesAndLateSuccessCannotRestore(t *testing.T) {
	for _, event := range []string{"thread/closed", "thread/archived", "thread/unsubscribe"} {
		r := sharedThreadReady167(t)
		r.threads["known"] = "aihub"
		d := sharedLiveHeldThread167(t, r, "thread/resume", "aihub", "known", "resume")
		if event == "thread/unsubscribe" {
			out := client(t, r, `{"id":"stop","method":"thread/unsubscribe","params":{"threadId":"known"}}`)
			if len(out) != 1 || out[0].Backend != "aihub" {
				t.Fatal("unadmitted thread could not be stopped")
			}
			reply(t, r, out[0], `{}`)
		} else {
			server(t, r, "aihub", string(raw(object{"method": raw(event), "params": raw(object{"threadId": raw("known")})})))
		}
		out := sharedLiveComplete167(t, r, d, "known")
		expectError(t, out)
		sharedLiveInput167(t, r, "turn/start", "known", false)
		if len(r.sharedNativeLive167) != 0 || len(r.active) != 0 || len(r.pending) != 0 {
			t.Fatal("late success revived closed admission")
		}
	}
}

func TestSharedNativeLive167ConcurrentResumeCannotRestoreOlderAdmission(t *testing.T) {
	r := sharedThreadReady167(t)
	r.threads["known"] = "aihub"
	old := sharedLiveHeldThread167(t, r, "thread/resume", "aihub", "known", "old")
	latest := sharedLiveHeldThread167(t, r, "thread/resume", "aihub", "known", "latest")
	sharedLiveComplete167(t, r, latest, "known")
	expectError(t, sharedLiveComplete167(t, r, old, "known"))
	sharedLiveInput167(t, r, "turn/start", "known", false)
	fresh := sharedLiveHeldThread167(t, r, "thread/resume", "aihub", "known", "fresh")
	sharedLiveComplete167(t, r, fresh, "known")
	sharedLiveInput167(t, r, "turn/start", "known", true)
}

func TestSharedNativeLive167DefaultModeUnchanged(t *testing.T) {
	r := testRouter(t, false)
	r.threads["known"] = "openai"
	sharedLiveInput167(t, r, "turn/start", "known", true)
	if len(r.sharedNativeLive167) != 0 || len(r.sharedNativeRevoked167) != 0 {
		t.Fatal("default mode allocated shared-route admission state")
	}
}

func TestSharedNativeLive167CloseOnlyRevokesItsExactOwner(t *testing.T) {
	r := sharedThreadReady167(t)
	d := sharedLiveHeldThread167(t, r, "thread/start", "aihub", "known", "start")
	sharedLiveComplete167(t, r, d, "known")
	closed := raw(object{"method": raw("thread/closed"), "params": raw(object{"threadId": raw("known")})})
	if _, err := r.Server("openai", closed); err == nil {
		t.Fatal("foreign close was accepted")
	}
	if r.sharedNativeLive167["known"] != "aihub" {
		t.Fatal("foreign close revoked another owner's admission")
	}
	server(t, r, "aihub", string(closed))
	sharedLiveInput167(t, r, "turn/start", "known", false)
	if len(r.sharedNativeLive167) != 0 {
		t.Fatal("owner close retained live admission")
	}
}
