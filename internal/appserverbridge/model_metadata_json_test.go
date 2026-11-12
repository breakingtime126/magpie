package appserverbridge

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestModelMetadataJSONAllowsDisplayWhitespaceOnly(t *testing.T) {
	positive := []string{
		`{"description":"first\nsecond\r\nthird\tcolumn"}`,
		`{"supportedReasoningEfforts":[{"reasoningEffort":"medium","description":"a\nb\tc"}]}`,
		`{"description":"paired Unicode \ud83d\ude00"}`,
	}
	for _, data := range positive {
		if validateModelMetadataJSON([]byte(data)) != nil { t.Fatal("valid model display text rejected") }
	}
	for _, data := range positive[:2] {
		if validateRequirementsJSON([]byte(data)) == nil { t.Fatal("policy text validator unexpectedly relaxed") }
	}
	for _, data := range []string{
		`{"description":"a","description":"b"}`,
		`{"description":"a","\u0064escription":"b"}`,
		`{"nested":{"x":1,"x":2}}`,
		`{"description":"\ud800"}`,
		`{"description":"\udc00"}`,
		`{"description":"\ud800x"}`,
		`{"description":"\u0000"}`,
		`{"description":"\u000b"}`,
		`{"description":"\u007f"}`,
		`{"description":"\u0085"}`,
		`{"bad\nkey":"value"}`,
		`{"description":"x"}{}`,
		"{\"description\":\""+string([]byte{0xff})+"\"}",
	} {
		if validateModelMetadataJSON([]byte(data)) == nil { t.Fatal("ambiguous or unsafe metadata JSON accepted") }
	}
}

func TestModelMetadataJSONRetainsBounds(t *testing.T) {
	exact := `{"description":""}`
	exact = `{"description":"`+strings.Repeat("a",modelMetadataJSONLimit-len(exact))+`"}`
	if len(exact)!=modelMetadataJSONLimit || validateModelMetadataJSON([]byte(exact))!=nil {t.Fatal("exact byte bound rejected")}
	for _, data := range []string{
		exact+" ",
		strings.Repeat("[",modelMetadataJSONDepth+1)+"0"+strings.Repeat("]",modelMetadataJSONDepth+1),
		"["+strings.Repeat("0,",modelMetadataContainerLimit)+"0]",
	} {
		if !errors.Is(validateModelMetadataJSON([]byte(data)),errModelMetadataJSONLimit) {t.Fatal("metadata structural limit lost")}
	}
}

func TestNativeSettingsMultilineModelDescriptions(t *testing.T) {
	r := nativeSettingsRouter167(t)
	rows := nativeSettingsRows167(t)
	m,_ := parseObject(rows["one"][0])
	m["description"] = raw("authored first\nsecond\r\nthird\tcolumn")
	m["supportedReasoningEfforts"] = raw([]object{
		{"reasoningEffort":raw("medium"),"description":raw("authored\nmedium")},
		{"reasoningEffort":raw("high"),"description":raw("authored\thigh")},
	})
	rows["one"] = []json.RawMessage{raw(m)}
	if r.ownedCatalog.validateNative(rows,r.backends)!=nil {t.Fatal("native display metadata blocked settings catalog")}
	got := r.ownedCatalog.projection[alias("one",text(m,"model"))]
	if !sameJSON(got["description"],m["description"]) || !sameJSON(got["supportedReasoningEfforts"],m["supportedReasoningEfforts"]) {t.Fatal("display metadata rewritten")}
}
