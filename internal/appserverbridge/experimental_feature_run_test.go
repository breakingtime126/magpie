package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Native synthetic peers only: no account/config import or network services.
const experimentalFeatureFixturePage = `{ "data": [{"name":"network_proxy","stage":"beta","displayName":"Network proxy","description":"Fixture metadata","announcement":"","enabled":false,"defaultEnabled":false}], "nextCursor": "1" }`

func experimentalFeatureFixture() (bool, int) {
	home := os.Getenv("HOME")
	if _, err := os.Stat(filepath.Join(home, "fixture-experimental-feature")); os.IsNotExist(err) {
		return false, 0
	} else if err != nil {
		return true, 51
	}
	log, err := os.OpenFile(filepath.Join(home, "fixture-experimental-feature-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 52
	}
	defer log.Close()
	fmt.Fprintln(os.Stderr, "experimental-feature-private-stderr-canary")
	decoder := json.NewDecoder(os.Stdin)
	readNumber := 0
	holdNumber := 0
	if data, err := os.ReadFile(filepath.Join(home, "fixture-experimental-feature-hold-read")); err == nil {
		holdNumber, err = strconv.Atoi(string(data))
		if err != nil || holdNumber < 1 {
			return true, 61
		}
	} else if !os.IsNotExist(err) {
		return true, 62
	}
	var heldID json.RawMessage
	for {
		var request json.RawMessage
		if err := decoder.Decode(&request); err == io.EOF {
			break
		} else if err != nil {
			return true, 53
		}
		if _, err := fmt.Fprintln(log, string(request)); err != nil {
			return true, 54
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(request, &msg) != nil {
			return true, 55
		}
		var method string
		if json.Unmarshal(msg["method"], &method) != nil {
			return true, 56
		}
		var body []byte
		switch method {
		case "initialize":
			body = []byte(`"result":{"userAgent":"experimental-feature-fixture","platformFamily":"fixture","platformOs":"fixture"}`)
		case "initialized":
			continue
		case "experimentalFeature/list":
			readNumber++
			if readNumber == holdNumber {
				// Hold the file read, not merely the response. The next client
				// request releases it after the test has published a new page.
				heldID = append(json.RawMessage(nil), msg["id"]...)
				continue
			}
			if heldID != nil {
				late, err := os.ReadFile(experimentalFeatureResponsePath(home, holdNumber))
				if err != nil {
					return true, 63
				}
				if os.WriteFile(filepath.Join(home, "fixture-experimental-feature-held-body"), late, 0600) != nil {
					return true, 64
				}
				if _, err := fmt.Fprintf(os.Stdout, "{\"id\":%s,%s}\n", heldID, late); err != nil {
					return true, 65
				}
				heldID = nil
			}
			body, err = os.ReadFile(experimentalFeatureResponsePath(home, readNumber))
			if err != nil {
				return true, 57
			}
		default:
			return true, 58
		}
		if _, err := fmt.Fprintf(os.Stdout, "{\"id\":%s,%s}\n", msg["id"], body); err != nil {
			return true, 59
		}
	}
	if os.WriteFile(filepath.Join(home, "fixture-experimental-feature-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 60
	}
	return true, 0
}

// JSON injection keeps these tests compilable on the exact pre-feature base.
// Copy this file and its fixture dispatch hook for an assertion-level old-base
// lifecycle red. No new production symbol is needed by these tests.
func experimentalFeatureRunConfig(t *testing.T, enabled bool) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	if enabled {
		settings, err := parseObject(raw(cfg))
		if err != nil {
			t.Fatal(err)
		}
		settings["experimental_feature_list"] = raw("compare-v0.167.0-alpha.3.2")
		if err := json.Unmarshal(raw(settings), &cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, backend := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(backend.Home, "fixture-experimental-feature"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	experimentalFeatureRunResponses(t, cfg, `"result":`+experimentalFeatureFixturePage, `"result":`+experimentalFeatureFixturePage)
	return cfg
}

// Each request reads its own immutable generation. A failed fanout may return
// before its sibling reads the fixture, so replacing a shared response file
// races that late read (including os.WriteFile's truncate/write window).
func experimentalFeatureResponsePath(home string, number int) string {
	return filepath.Join(home, fmt.Sprintf("fixture-experimental-feature-response-%d", number))
}

func experimentalFeatureRunResponses(t *testing.T, cfg Config, first, second string) {
	t.Helper()
	for i, body := range []string{first, second} {
		home := cfg.Backends[i].Home
		counter := filepath.Join(home, "fixture-experimental-feature-response-count")
		number := 1
		data, err := os.ReadFile(counter)
		if err == nil {
			previous, parseErr := strconv.Atoi(string(data))
			if parseErr != nil || previous < 1 {
				t.Fatal("invalid fixture response generation")
			}
			number = previous + 1
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		// Parent publication is sequential and completes before the next
		// client request. Children never read or write the counter.
		path := experimentalFeatureResponsePath(home, number)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString(body)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("cannot publish immutable fixture response")
		}
		if err := os.WriteFile(counter, []byte(strconv.Itoa(number)), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func experimentalFeatureRunFinish(t *testing.T, cfg Config, bridge *runningBridge, stream *requirementsStream, params []string, responses int) {
	t.Helper()
	if err := bridge.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != responses || strings.Contains(string(stream.transcript), "private-") {
		t.Fatal("unexpected feature client transcript or unsafe source data")
	}
	ids := map[string]bool{}
	for _, backend := range cfg.Backends {
		log, err := os.ReadFile(filepath.Join(backend.Home, "fixture-experimental-feature-requests"))
		if err != nil {
			t.Fatal(err)
		}
		reads, initializations := 0, 0
		for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			msg, err := parseObject([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			id := string(msg["id"])
			if ids[id] {
				t.Fatal("fixture transport request ID reused")
			}
			ids[id] = true
			switch text(msg, "method") {
			case "initialize":
				initializations++
				p, err := child(msg, "params")
				if err != nil || string(p["capabilities"]) != "{}" {
					t.Fatal("feature list enabled a capability")
				}
			case "experimentalFeature/list":
				if reads >= len(params) || len(msg) != 3 || exists(msg, "trace") ||
					string(msg["params"]) != authStatusRunCompact(t, params[reads]) {
					t.Fatal("feature params changed, trace propagated or child retried")
				}
				reads++
			default:
				t.Fatal("feature-only fixture received another method")
			}
		}
		if initializations != 1 || reads != len(params) {
			t.Fatal("feature page was not freshly read from both children")
		}
		marker, err := os.ReadFile(filepath.Join(backend.Home, "fixture-experimental-feature-clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatal("feature child did not exit cleanly on EOF")
		}
	}
}

func TestRunExperimentalFeature167FreshPagesAndFailures(t *testing.T) {
	cfg := experimentalFeatureRunConfig(t, true)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	var params []string
	ask := func(p string) object {
		t.Helper()
		params = append(params, p)
		return stream.ask(t, bridge, `{"id":9007199254740993,"method":"experimentalFeature/list","params":`+p+`,"trace":{"traceparent":"private-trace-canary","tracestate":null}}`)
	}
	expectResult := func(result object, source string) {
		t.Helper()
		if string(result["id"]) != "9007199254740993" || exists(result, "error") ||
			string(result["result"]) != authStatusRunCompact(t, source) {
			t.Fatal("fresh complete feature page or original ID was not preserved")
		}
	}
	expectResult(ask(`{"limit":1,"cursor":null,"threadId":null}`), experimentalFeatureFixturePage)
	changed := strings.Replace(experimentalFeatureFixturePage, `"enabled":false`, `"enabled":true`, 1)
	experimentalFeatureRunResponses(t, cfg, `"result":`+changed, `"result":`+changed)
	expectResult(ask(`{ "limit" : 4294967295 }`), changed)
	last := `{"data":[],"nextCursor":null}`
	experimentalFeatureRunResponses(t, cfg, `"result":`+last, `"result":`+last)
	expectResult(ask(`{"cursor":"1","limit":0}`), last)
	for _, bodies := range [][2]string{
		{`"result":` + experimentalFeatureFixturePage, `"result":` + changed},
		{`"error":{"code":-32037,"message":"private-error-canary","data":{"token":"private-data-canary"}}`, `"result":` + changed},
		{`"result":{"data":[],"nextCursor":null,"unknown":"private-extra-canary"}`, `"result":` + changed},
	} {
		experimentalFeatureRunResponses(t, cfg, bodies[0], bodies[1])
		response := ask(`{"limit":1}`)
		failure, _ := child(response, "error")
		if exists(response, "result") || len(failure) != 2 || string(failure["code"]) != "-32000" ||
			text(failure, "message") != "shared experimental features unavailable" {
			t.Fatal("failed native feature read became success or exposed error text")
		}
	}
	experimentalFeatureRunResponses(t, cfg, `"result":`+changed, `"result":`+changed)
	expectResult(ask(`{"cu\u0072sor":null,"limit":17}`), changed)
	blocked := stream.ask(t, bridge, `{"id":"blocked","method":"experimentalFeature/list","params":{"limit":1,"threadId":"private-thread-canary"}}`)
	failure, _ := child(blocked, "error")
	if exists(blocked, "result") || string(failure["code"]) != "-32602" {
		t.Fatal("thread-scoped feature read reached a child")
	}
	experimentalFeatureRunFinish(t, cfg, bridge, stream, params, 2+len(params))
}

func TestRunExperimentalFeature167SoleModePinsAndDefault(t *testing.T) {
	for _, index := range []int{0, 1} {
		for _, pin := range []string{"", strings.Repeat("0", 64)} {
			cfg := experimentalFeatureRunConfig(t, true)
			cfg.Backends[index].StartupProbeSHA256 = pin
			var output discardRequirementsOutput
			if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
				t.Fatal("feature-only mode accepted missing or mismatched pin")
			}
			for _, b := range cfg.Backends {
				for _, marker := range []string{"fixture-report", "fixture-experimental-feature-requests", "fixture-experimental-feature-clean-exit"} {
					if _, err := os.Stat(filepath.Join(b.Home, marker)); !os.IsNotExist(err) {
						t.Fatal("a child started before both pins passed")
					}
				}
			}
		}
	}
	cfg := experimentalFeatureRunConfig(t, false)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	response := stream.ask(t, bridge, `{"id":"disabled","method":"experimentalFeature/list","params":{"limit":1}}`)
	failure, _ := child(response, "error")
	if exists(response, "result") || string(failure["code"]) != "-32601" {
		t.Fatal("default feature-list route changed")
	}
	experimentalFeatureRunFinish(t, cfg, bridge, stream, nil, 2)
}

// Keep the fixture's compacted native source bytes independent of object-map
// decoding, which could accidentally hide field reconstruction.
func TestExperimentalFeatureFixtureIsJSON(t *testing.T) {
	var b bytes.Buffer
	if json.Compact(&b, []byte(experimentalFeatureFixturePage)) != nil {
		t.Fatal("malformed synthetic feature fixture")
	}
}

func TestExperimentalFeatureFixtureLateReadsKeepTheirGeneration(t *testing.T) {
	cfg := Config{Backends: []Backend{{Home: t.TempDir()}, {Home: t.TempDir()}}}
	first := `"error":{"code":-32037,"message":"synthetic failure"}`
	second := `"result":` + experimentalFeatureFixturePage
	experimentalFeatureRunResponses(t, cfg, first, second)
	// Model a sibling that has accepted request 1 but has not opened its
	// response yet. No sleep or scheduler ordering determines this delay.
	latePath := experimentalFeatureResponsePath(cfg.Backends[1].Home, 1)
	next := `"result":{"data":[],"nextCursor":null}`
	experimentalFeatureRunResponses(t, cfg, next, next)
	late, err := os.ReadFile(latePath)
	if err != nil || string(late) != second {
		t.Fatal("publishing the next response changed the pending sibling's generation")
	}
	for _, backend := range cfg.Backends {
		data, err := os.ReadFile(experimentalFeatureResponsePath(backend.Home, 2))
		if err != nil || string(data) != next {
			t.Fatal("new requests did not receive their own response generation")
		}
	}
}

func TestRunExperimentalFeature167LateSiblingUsesOriginalGeneration(t *testing.T) {
	cfg := experimentalFeatureRunConfig(t, true)
	if err := os.WriteFile(filepath.Join(cfg.Backends[1].Home, "fixture-experimental-feature-hold-read"), []byte("2"), 0600); err != nil {
		t.Fatal(err)
	}
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	ask := func(id int) object {
		t.Helper()
		return stream.ask(t, bridge, fmt.Sprintf(`{"id":%d,"method":"experimentalFeature/list","params":{"limit":1}}`, id))
	}
	if response := ask(1); !exists(response, "result") {
		t.Fatal("initial page failed")
	}
	original := `"result":` + experimentalFeatureFixturePage
	experimentalFeatureRunResponses(t, cfg, `"error":{"code":-32037,"message":"synthetic failure"}`, original)
	if response := ask(2); !exists(response, "error") || exists(response, "result") {
		t.Fatal("first backend failure did not end the fanout")
	}
	// The secondary deliberately has not read generation 2. Generation 3
	// publication and this request release it without any timing assumption.
	next := `{"data":[],"nextCursor":null}`
	experimentalFeatureRunResponses(t, cfg, `"result":`+next, `"result":`+next)
	if response := ask(3); !sameJSON(response["result"], []byte(next)) || exists(response, "error") {
		t.Fatal("late sibling prevented the next independent fanout")
	}
	late, err := os.ReadFile(filepath.Join(cfg.Backends[1].Home, "fixture-experimental-feature-held-body"))
	if err != nil || string(late) != original {
		t.Fatal("delayed native child read a different request's fixture response")
	}
	experimentalFeatureRunFinish(t, cfg, bridge, stream, []string{`{"limit":1}`, `{"limit":1}`, `{"limit":1}`}, 4)
}
