package appserverbridge

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func sharedThreadReady167(t *testing.T) *Router {
	t.Helper()
	r := sharedNativeRouterFixture167(t)
	checks := sharedNativeInitialize167(t, r)
	var list []Delivery
	for _, d := range checks {
		list = append(list, reply(t, r, d, string(raw(sharedNativeConfigResponse167(r, d.Backend))))...)
	}
	if len(list) != 2 {
		t.Fatal("startup did not reach real catalog")
	}
	for _, d := range list {
		reply(t, r, d, string(raw(object{"data": raw([]json.RawMessage{fixtureModel("synthetic-model", true)}), "nextCursor": raw(nil)})))
	}
	if r.phase != "ready" || len(r.active) != 0 {
		t.Fatal("startup did not complete")
	}
	return r
}

func sharedThreadRequest167(method, backend, cwd string) object {
	p := object{"cwd": raw(cwd), "model": raw(alias(backend, "synthetic-model")), "sandbox": raw("workspace-write")}
	if method == "thread/resume" {
		p["threadId"] = raw("existing")
	}
	return object{"id": raw("thread-check"), "method": raw(method), "params": raw(p)}
}

func sharedThreadConfigResult167(t *testing.T, r *Router, backend, cwd string) object {
	t.Helper()
		result := sharedNativeConfigResponse167(r, backend)
	config, _ := child(result, "config")
	config["projects"] = raw(object{cwd: raw(object{"trust_level": raw("trusted")})})
	config["unusedPrivateMetadata"] = raw("PRIVATE_CANARY")
	result["config"] = raw(config)
	return result
}

func sharedThreadAssertPolicy167(t *testing.T, r *Router, out []Delivery, backend string) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != backend || len(r.pending) != 1 || len(r.active) != 1 || r.activeBytes == 0 {
		t.Fatal("thread was not retained under a target-only policy check")
	}
	m, _ := parseObject(out[0].Message)
	if text(m, "method") != "configRequirements/read" || !requirementsNull(m["params"]) {
		t.Fatal("thread forwarded before its policy precondition")
	}
}

func sharedThreadConfigCheck167(t *testing.T, r *Router, policy Delivery, cwd string) Delivery {
	t.Helper()
	out := reply(t, r, policy, controlPlaneNullRequirements)
	if len(out) != 1 || out[0].Backend != policy.Backend || len(r.pending) != 1 || len(r.active) != 1 {
		t.Fatal("valid policy did not retain group for same-target config")
	}
	m, _ := parseObject(out[0].Message)
	p, _ := child(m, "params")
	if text(m, "method") != "config/read" || len(p) != 2 || !sameJSON(p["includeLayers"], raw(false)) || text(p, "cwd") != cwd {
		t.Fatal("project config check changed original cwd or included native layers")
	}
	return out[0]
}

func TestSharedNativeThread167ChecksTargetBeforeStartAndEveryResume(t *testing.T) {
	for _, method := range []string{"thread/start", "thread/resume"} {
		for _, backend := range []string{"openai", "aihub"} {
			for _, environment := range []string{"absent", "null", "spaced-null", "empty"} {
				r := sharedThreadReady167(t)
				cwd := t.TempDir()
				r.threads["existing"] = backend
				request := sharedThreadRequest167(method, backend, cwd)
				params, _ := child(request, "params")
				if environment != "absent" {
					value := raw(nil)
					if environment == "empty" {
						value = raw([]any{})
					}
					params["runtimeWorkspaceRoots"] = value
					if method == "thread/start" {
						params["environments"] = value
						params["serviceName"] = raw(nil)
					}
					request["params"] = raw(params)
				}
				encoded := string(raw(request))
				if environment == "spaced-null" {
					encoded = strings.ReplaceAll(encoded, ":null", ": \n null ")
				}
				out := client(t, r, encoded)
				sharedThreadAssertPolicy167(t, r, out, backend)
				check := sharedThreadConfigCheck167(t, r, out[0], cwd)
				out = reply(t, r, check, string(raw(sharedThreadConfigResult167(t, r, backend, cwd))))
				if len(out) != 1 || out[0].Backend != backend || len(r.active) != 1 || len(r.pending) != 1 {
					t.Fatal("trusted config did not release exactly the held native request")
				}
				m, _ := parseObject(out[0].Message)
				actual, _ := child(m, "params")
				params["model"] = raw("synthetic-model")
				params["modelProvider"] = raw(r.byID[backend].Provider)
				if text(m, "method") != method || !sameJSON(raw(actual), raw(params)) || strings.Contains(string(out[0].Message), "PRIVATE_CANARY") {
					t.Fatal("release changed original pinned request or leaked native config")
				}
				threadID := "created"
				if method == "thread/resume" {
					threadID = "existing"
				}
				out = reply(t, r, out[0], string(raw(object{"modelProvider": raw(r.byID[backend].Provider), "thread": raw(object{"id": raw(threadID), "modelProvider": raw(r.byID[backend].Provider)})})))
				if len(out) != 1 || out[0].Backend != "" || len(r.active) != 0 || len(r.pending) != 0 || r.activeBytes != 0 || r.threads[threadID] != backend {
					t.Fatal("thread completion lost owner or request resources")
				}
				response, _ := parseObject(out[0].Message)
				if !sameJSON(response["id"], request["id"]) || exists(response, "error") {
					t.Fatal("thread completion lost original ID")
				}
				// A known owner is not admission for a later resume.
				r.threads["existing"] = backend
				out = client(t, r, string(raw(sharedThreadRequest167("thread/resume", backend, cwd))))
				sharedThreadAssertPolicy167(t, r, out, backend)
			}
		}
	}
}

func TestSharedNativeThread167RejectsUnsupportedContextBeforeChecks(t *testing.T) {
	for _, kind := range []string{"missing-cwd", "relative-cwd", "missing-dir", "control", "native", "environment", "service", "fork"} {
		r := sharedThreadReady167(t)
		r.threads["existing"] = "openai"
		request := sharedThreadRequest167("thread/start", "openai", t.TempDir())
		p, _ := child(request, "params")
		switch kind {
		case "missing-cwd":
			delete(p, "cwd")
		case "relative-cwd":
			p["cwd"] = raw("relative")
		case "missing-dir":
			p["cwd"] = raw(filepath.Join(t.TempDir(), "missing"))
		case "control":
			p["cwd"] = raw(r.controlHome167)
		case "native":
			p["cwd"] = raw(r.backends[0].CodexHome)
		case "environment":
			p["environments"] = raw([]object{{"type": raw("remote"), "id": raw("PRIVATE_CANARY")}})
		case "service":
			p["serviceName"] = raw("PRIVATE_CANARY")
		case "fork":
			request["method"] = raw("thread/fork")
			p["threadId"] = raw("existing")
		}
		request["params"] = raw(p)
		out := client(t, r, string(raw(request)))
		expectError(t, out)
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || strings.Contains(string(out[0].Message), "PRIVATE_CANARY") {
			t.Fatal("unsupported context reached native or retained payload")
		}
	}
}

func TestSharedNativeThread167FailedPolicyOrRouteNeverForwards(t *testing.T) {
	for _, kind := range []string{"policy-error", "nonnull-policy", "missing-policy-field", "route", "config-error"} {
		r := sharedThreadReady167(t)
		cwd := t.TempDir()
		out := client(t, r, string(raw(sharedThreadRequest167("thread/start", "aihub", cwd))))
		sharedThreadAssertPolicy167(t, r, out, "aihub")
		d := out[0]
		var response object
		if strings.Contains(kind, "policy") {
			m, _ := parseObject(d.Message)
			response = object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE_CANARY")})}
			if kind != "policy-error" {
				delete(response, "error")
				result := object{"requirements": raw(nil)}
				if kind == "nonnull-policy" {
					result["requirements"] = raw(object{})
					result["supportsIndependentSpeedModes"] = raw(true)
				}
				response["result"] = raw(result)
			}
		} else {
			d = sharedThreadConfigCheck167(t, r, d, cwd)
			m, _ := parseObject(d.Message)
			result := sharedThreadConfigResult167(t, r, "aihub", cwd)
			config, _ := child(result, "config")
			switch kind {
			case "route":
				config["model_provider"] = raw("PRIVATE_CANARY")
			}
			result["config"] = raw(config)
			response = object{"id": m["id"], "result": raw(result)}
			if kind == "config-error" {
				delete(response, "result")
				response["error"] = raw(object{"code": raw(-32000), "message": raw("PRIVATE_CANARY")})
			}
		}
		out, err := r.Server(d.Backend, raw(response))
		if err == nil || len(out) != 0 || r.primaryRouteReady167() || !r.auth.primaryProofFailed {
			t.Fatal("observed route uncertainty did not terminate relay admission")
		}
		if len(r.active) != 0 || len(r.pending) != 0 || r.activeBytes != 0 || len(r.threads) != 0 || strings.Contains(err.Error(), "PRIVATE_CANARY") {
			t.Fatal("failed admission retained payload, bound thread, or leaked source")
		}
		// The consumed failed stage has no outstanding sibling to resume it.
		// A duplicate late response must never emit the held thread request.
		late, _ := r.Server(d.Backend, raw(response))
		for _, delivery := range late {
			if delivery.Backend != "" {
				t.Fatal("late response resumed rejected thread")
			}
		}
	}
}

func TestSharedNativeThread167DoesNotRequireOrRewriteNativeTrust(t *testing.T) {
	for _, state := range []string{"absent", "untrusted", "trusted"} {
		r := sharedThreadReady167(t)
		cwd := t.TempDir()
		request := sharedThreadRequest167("thread/start", "aihub", cwd)
		out := client(t, r, string(raw(request)))
		check := sharedThreadConfigCheck167(t, r, out[0], cwd)
		result := sharedNativeConfigResponse167(r, "aihub")
		config, _ := child(result, "config")
		if state != "absent" {
			config["projects"] = raw(object{cwd: raw(object{"trust_level": raw(state)})})
		}
		result["config"] = raw(config)
		out = reply(t, r, check, string(raw(result)))
		if len(out) != 1 || out[0].Backend != "aihub" {
			t.Fatal("native trust was made a bridge route precondition")
		}
		message, _ := parseObject(out[0].Message)
		params, _ := child(message, "params")
		original, _ := child(request, "params")
		original["model"] = raw("synthetic-model")
		original["modelProvider"] = raw(sharedSecondaryProvider167)
		if !sameJSON(raw(params), raw(original)) {
			t.Fatal("bridge rewrote native trust or caller sandbox")
		}
	}
}

func TestSharedNativeThread167PreservesLocalEnvironmentSnapshot(t *testing.T) {
	r := sharedThreadReady167(t)
	cwd := t.TempDir()
	request := sharedThreadRequest167("thread/start", "aihub", cwd)
	params, _ := child(request, "params")
	roots := raw([]string{cwd})
	params["runtimeWorkspaceRoots"] = roots
	params["environments"] = raw([]object{{"environmentId": raw("local"), "cwd": raw(cwd), "runtimeWorkspaceRoots": roots}})
	request["params"] = raw(params)
	out := client(t, r, string(raw(request)))
	check := sharedThreadConfigCheck167(t, r, out[0], cwd)
	out = reply(t, r, check, string(raw(sharedThreadConfigResult167(t, r, "aihub", cwd))))
	if len(out) != 1 || out[0].Backend != "aihub" {
		t.Fatal("native local snapshot was rejected by route admission")
	}
	forwarded, _ := parseObject(out[0].Message)
	actual, _ := child(forwarded, "params")
	params["model"] = raw("synthetic-model")
	params["modelProvider"] = raw(sharedSecondaryProvider167)
	if !sameJSON(raw(actual), raw(params)) {
		t.Fatal("route admission changed original environment, roots, or permissions")
	}
}
