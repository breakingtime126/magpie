package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func desktopRequest(prompt string) []byte {
	return jsonBytes(map[string]any{"model": modelName, "stream": true, "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}}})
}

func TestDesktopFixtureAllowsUnsequencedSyntheticTurns(t *testing.T) {
	f := newDesktopFixture("alpha")
	defer f.close()
	for _, prompt := range []string{"synthetic-alpha-first UI text", "synthetic-alpha-another 42"} {
		resp, err := http.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(desktopRequest(prompt)))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || !strings.Contains(string(body), "fixture-answer-alpha") {
			t.Fatal("unsequenced synthetic Desktop text must receive its route answer")
		}
	}
	if n, bad := f.counts(); n != 2 || bad != 0 {
		t.Fatalf("counts = %d, %d", n, bad)
	}
}

func TestDesktopFixtureRejectsUnsafeInput(t *testing.T) {
	cases := map[string][]byte{
		"arbitrary": desktopRequest("hello"),
		"other route": desktopRequest("synthetic-beta-first"),
		"url": desktopRequest("synthetic-alpha-https://example.com"),
		"duplicate": []byte(`{"model":"gpt-5.5","model":"gpt-5.5","stream":true,"input":[]}`),
		"tool": []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"function_call_output","output":"synthetic-alpha-first"}]}`),
		"image": []byte(`{"model":"gpt-5.5","stream":true,"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com"}]}]}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDesktopFixture("alpha")
			defer f.close()
			r, err := http.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.StatusCode != 400 {
				t.Fatal("unsafe input was accepted")
			}
			if n, bad := f.counts(); n != 0 || bad != 1 {
				t.Fatalf("counts = %d, %d", n, bad)
			}
		})
	}
}

func TestDesktopFixtureRejectsHeadersAndBoundsRequests(t *testing.T) {
	f := newDesktopFixture("alpha")
	defer f.close()
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key", "X-Oai-Attestation"} {
		r, _ := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses", bytes.NewReader(desktopRequest("synthetic-alpha-test")))
		r.Header.Set(key, "synthetic-forbidden")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("credential header accepted")
		}
	}
	for i := 0; i < 17; i++ {
		resp, err := http.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(desktopRequest("synthetic-alpha-test")))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 200
		if i == 16 {
			want = 400
		}
		if resp.StatusCode != want {
			t.Fatal("request cap not enforced")
		}
	}
}

func TestDesktopInputRejectsWrongAssistantAndToolItems(t *testing.T) {
	for _, item := range []string{
		`{"role":"assistant","content":[{"type":"output_text","text":"fixture-answer-beta"}]}`,
		`{"type":"function_call","name":"shell"}`,
	} {
		raw := json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"synthetic-alpha-first"}]},`+item+`]`)
		if desktopInputAllowed(raw, "alpha") {
			t.Fatal("wrong-route or tool history accepted")
		}
	}
}

func TestDesktopInputStrictReviewedMessageShape(t *testing.T) {
	user := `{"type":"message","role":"user","content":[{"type":"input_text","text":"synthetic-alpha-first"}]}`
	allowed := []string{
		user,
		`{"type":"message","id":"native-id","status":"completed","role":"assistant","phase":"final_answer","encrypted_content":null,"internal_chat_message_metadata_passthrough":null,"content":[{"type":"output_text","text":"fixture-answer-alpha","annotations":[],"logprobs":null}]}`,
		`{"type":"message","role":"developer","content":[{"type":"input_text","text":"Native instructions may mention https://example.com without being fetched."}]}`,
	}
	for _, item := range allowed {
		if !desktopInputAllowed(json.RawMessage("["+user+","+item+"]"), "alpha") {
			t.Fatal("reviewed plaintext message shape rejected")
		}
	}
	denied := []string{
		strings.Replace(user, `"role":"user"`, `"role":"user","image_url":"https://example.com"`, 1),
		strings.Replace(user, `"text":"synthetic-alpha-first"`, `"text":"synthetic-alpha-first","image_url":"https://example.com"`, 1),
		strings.Replace(user, `"text":"synthetic-alpha-first"`, `"text":"synthetic-alpha-first","tool_call":{"name":"shell"}`, 1),
		strings.Replace(user, `"role":"user"`, `"role":"user","arguments":"shell command"`, 1),
		strings.Replace(user, `"role":"user"`, `"role":"user","encrypted_content":"opaque"`, 1),
		strings.Replace(user, `"role":"user"`, `"role":"user","id":42`, 1),
		strings.Replace(user, `"role":"user"`, `"role":"user","phase":"unknown"`, 1),
		strings.Replace(user, `"role":"user"`, `"role":"user","internal_chat_message_metadata_passthrough":{"executed_tool_calls":[]}`, 1),
		strings.Replace(user, `"text":"synthetic-alpha-first"`, `"text":null`, 1),
		strings.Replace(user, `"type":"input_text"`, `"type":"output_text"`, 1),
		strings.Replace(user, `"text":"synthetic-alpha-first"`, `"text":"synthetic-alpha-first","annotations":[]`, 1),
		`{"role":"assistant","content":[{"type":"output_text","text":"fixture-answer-alpha","annotations":[{"url":"https://example.com"}]}]}`,
		`{"role":"user","content":[{"type":"input_text","text":"synthetic-alpha-first"},{"type":"input_image","image_url":"https://example.com"}]}`,
		`{"type":"function_call","name":"shell","arguments":"synthetic-alpha-first"}`,
	}
	for _, item := range denied {
		if desktopInputAllowed(json.RawMessage("["+user+","+item+"]"), "alpha") {
			t.Fatal("hidden payload, wrong type or mixed input was accepted")
		}
	}
}
