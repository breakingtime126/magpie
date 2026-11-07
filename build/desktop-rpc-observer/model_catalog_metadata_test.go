package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Synthetic wire shapes from the exact 4239 ModelListResponse and the bridge's
// existing aliases/display suffixes. These are not a captured Desktop response.
const modelCatalogAlpha = `{"id":"magpie/alpha/gpt-5.5","model":"magpie/alpha/gpt-5.5","displayName":"GPT-5.5 [alpha]","hidden":false,"isDefault":true,"description":"PRIVATE_CANARY"}`
const modelCatalogBeta = `{"id":"magpie/beta/gpt-5.5","model":"magpie/beta/gpt-5.5","displayName":"GPT-5.5 [beta]","hidden":false,"isDefault":false}`

func modelCatalogResponse(data, cursor string) string {
	return `{"id":"PRIVATE_ID","result":{"data":[` + data + `],"nextCursor":` + cursor + `}}`
}

func observeModelCatalog(t *testing.T, request, response string) (event, string) {
	t.Helper()
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(request))
	o.line("from_bridge", []byte(response))
	es := events(t, log.Bytes())
	if len(es) != 2 {
		t.Fatal("missing observation")
	}
	if strings.Contains(log.String(), "PRIVATE") || strings.Contains(log.String(), "gpt-5.5") || strings.Contains(log.String(), "GPT-5.5") {
		t.Fatal("catalog raw values escaped")
	}
	return es[1], log.String()
}

const modelCatalogRequest = `{"id":"PRIVATE_ID","method":"model/list","params":{"cursor":"PRIVATE_CURSOR"}}`

func TestModelCatalogCountsAreCorrelatedAndValueFree(t *testing.T) {
	e, _ := observeModelCatalog(t, modelCatalogRequest, modelCatalogResponse(modelCatalogAlpha+","+modelCatalogBeta, "null"))
	if e.Method != "model/list" || e.Correlation == 0 || e.ModelCatalog == nil || e.ModelCatalog.Status != "observed" || e.ModelCatalog.Counts == nil {
		t.Fatal("correlated model directory summary unavailable")
	}
	c := *e.ModelCatalog.Counts
	if c != (modelCatalogCounts{Items: 2, Alpha: 1, AlphaDefault: 1, Beta: 1}) {
		t.Fatal("known tuple counts were lost")
	}
	for _, tc := range []struct {
		data, cursor string
		want         modelCatalogCounts
		status       string
	}{
		{"", "null", modelCatalogCounts{}, "observed"},
		{strings.Replace(modelCatalogAlpha, `"hidden":false`, `"hidden":true`, 1), "null", modelCatalogCounts{Items: 1, Alpha: 1, AlphaHidden: 1, AlphaDefault: 1}, "observed"},
		{modelCatalogBeta, `"PRIVATE_CURSOR"`, modelCatalogCounts{Items: 1, Beta: 1, HasMore: true}, "partial"},
		{strings.ReplaceAll(modelCatalogBeta, "magpie/beta/gpt-5.5", "PRIVATE_MODEL"), "null", modelCatalogCounts{Items: 1, Unknown: 1}, "partial"},
		{strings.Replace(modelCatalogBeta, `"id":"magpie/beta/gpt-5.5"`, `"id":"magpie/alpha/gpt-5.5"`, 1), "null", modelCatalogCounts{Items: 1, Unknown: 1}, "partial"},
		{strings.Replace(modelCatalogBeta, "GPT-5.5 [beta]", "PRIVATE_LABEL", 1), "null", modelCatalogCounts{Items: 1, Unknown: 1}, "partial"},
	} {
		e, _ := observeModelCatalog(t, modelCatalogRequest, modelCatalogResponse(tc.data, tc.cursor))
		if e.ModelCatalog == nil || e.ModelCatalog.Status != tc.status || e.ModelCatalog.Counts == nil || *e.ModelCatalog.Counts != tc.want {
			t.Fatal("bounded directory facts changed")
		}
	}
}

func TestModelCatalogMalformedIsNotEmptyOrSuccessful(t *testing.T) {
	good := modelCatalogResponse(modelCatalogAlpha, "null")
	for _, response := range []string{
		strings.Replace(good, `"result":`, `"result":{},"result":`, 1),
		strings.Replace(good, `"id":"PRIVATE_ID"`, `"id":"PRIVATE_OTHER","id":"PRIVATE_ID"`, 1),
		strings.Replace(good, `"data":[`, `"data":null,"data":[`, 1),
		strings.Replace(good, `"hidden":false`, `"hidden":null`, 1),
		strings.Replace(good, `"hidden":false`, `"hidden":false,"hidden":true`, 1),
		strings.Replace(good, `"isDefault":true`, `"isDefault":"true"`, 1),
		strings.Replace(good, `"isDefault":true,`, "", 1),
		strings.Replace(good, `,"nextCursor":null`, "", 1),
		strings.Replace(good, `"nextCursor":null`, `"nextCursor":false`, 1),
		strings.Replace(good, `"data":[`+modelCatalogAlpha+`]`, `"data":null`, 1),
	} {
		e, _ := observeModelCatalog(t, modelCatalogRequest, response)
		if e.ModelCatalog == nil || e.ModelCatalog.Status != "unavailable" || e.ModelCatalog.Counts != nil {
			t.Fatal("malformed response became directory evidence")
		}
	}
	over := strings.TrimSuffix(strings.Repeat(modelCatalogBeta+",", 33), ",")
	e, _ := observeModelCatalog(t, modelCatalogRequest, modelCatalogResponse(over, "null"))
	if e.ModelCatalog == nil || e.ModelCatalog.Status != "limit" || e.ModelCatalog.Counts != nil {
		t.Fatal("over-limit response became directory evidence")
	}
}

func TestModelCatalogWrongIDAndAmbiguityNeverAttribute(t *testing.T) {
	response := modelCatalogResponse(modelCatalogAlpha, "null")
	for _, request := range []string{
		strings.Replace(modelCatalogRequest, "PRIVATE_ID", "PRIVATE_OTHER", 1),
		strings.Replace(modelCatalogRequest, "model/list", "account/read", 1),
		strings.Replace(modelCatalogRequest, `"method":"model/list"`, `"method":"account/read","method":"model/list"`, 1),
		strings.Replace(modelCatalogRequest, `"id":"PRIVATE_ID"`, `"id":"PRIVATE_OTHER","id":"PRIVATE_ID"`, 1),
	} {
		e, _ := observeModelCatalog(t, request, response)
		if e.ModelCatalog != nil {
			t.Fatal("unrelated or ambiguous request gained model evidence")
		}
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(modelCatalogRequest))
	o.line("to_bridge", []byte(modelCatalogRequest))
	o.line("from_bridge", []byte(response))
	o.line("to_bridge", []byte(response))
	for _, e := range events(t, log.Bytes()) {
		if e.ModelCatalog != nil {
			t.Fatal("quarantined or wrong-direction response gained model evidence")
		}
	}
}

func TestModelCatalogTapLeavesEveryForwardedByteUnchanged(t *testing.T) {
	for _, step := range []int{1, 7, 8192} {
		var log, forwarded bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(modelCatalogRequest))
		payload := []byte(modelCatalogResponse(modelCatalogAlpha+","+modelCatalogBeta, "null") + "\r\n")
		if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), step}, &lineObserver{o: o, direction: "from_bridge"}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, forwarded.Bytes()) || strings.Contains(log.String(), "PRIVATE") {
			t.Fatal("catalog observation changed wire bytes or leaked input")
		}
		es := events(t, log.Bytes())
		if es[1].ModelCatalog == nil || es[1].ModelCatalog.Status != "observed" {
			t.Fatal("fragmented response evidence missing")
		}
		encoded, _ := json.Marshal(es[1])
		if len(encoded) > 4096 {
			t.Fatal("summary exceeds existing publication record bound")
		}
	}
}

func TestModelCatalogErrorsLimitsAndReverseRequestsStayOpaque(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("from_bridge", []byte(modelCatalogRequest))
	o.line("to_bridge", []byte(modelCatalogResponse(modelCatalogAlpha, "null")))
	o.line("to_bridge", []byte(modelCatalogRequest))
	o.line("from_bridge", []byte(`{"id":"PRIVATE_ID","error":{"code":-32602,"message":"PRIVATE_CANARY"}}`))
	for _, e := range events(t, log.Bytes()) {
		if e.ModelCatalog != nil {
			t.Fatal("error or reverse request gained model evidence")
		}
	}
	log.Reset()
	o = newObserver(&log)
	o.line("to_bridge", []byte(modelCatalogRequest))
	payload := []byte(modelCatalogResponse(modelCatalogAlpha, "null"))
	payload = append(payload, bytes.Repeat([]byte(" "), maxLine)...)
	payload = append(payload, '\n')
	var forwarded bytes.Buffer
	if pump(&forwarded, bytes.NewReader(payload), &lineObserver{o: o, direction: "from_bridge"}) != nil || !bytes.Equal(payload, forwarded.Bytes()) {
		t.Fatal("oversized response forwarding changed")
	}
	found := false
	for _, e := range events(t, log.Bytes()) {
		found = found || e.Class == "oversized"
		if e.ModelCatalog != nil {
			t.Fatal("oversized line became directory evidence")
		}
	}
	if !found || strings.Contains(log.String(), "PRIVATE") {
		t.Fatal("oversized response was not safely classified")
	}
}
