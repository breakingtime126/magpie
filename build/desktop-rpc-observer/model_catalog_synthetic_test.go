package main

import (
	"bytes"
	"strings"
	"testing"
)

// Self-authored test metadata only, not a native response capture or a model record.
const syntheticCatalogAlpha = `{"id":"magpie/alpha/test-alpha","model":"magpie/alpha/test-alpha","displayName":"Synthetic Alpha [alpha]","hidden":false,"isDefault":true}`
const syntheticCatalogBeta = `{"id":"magpie/beta/test-beta","model":"magpie/beta/test-beta","displayName":"Synthetic Beta [beta]","hidden":false,"isDefault":false}`

func TestSelfAuthoredModelCatalogCountsOnlyExactTuples(t *testing.T) {
	e, log := observeModelCatalog(t, modelCatalogRequest, modelCatalogResponse(syntheticCatalogAlpha+","+syntheticCatalogBeta, "null"))
	if e.ModelCatalog == nil || e.ModelCatalog.Status != "observed" || e.ModelCatalog.Counts == nil ||
		*e.ModelCatalog.Counts != (modelCatalogCounts{Items: 2, Alpha: 1, AlphaDefault: 1, Beta: 1}) {
		t.Fatal("self-authored model tuples were not recognized as exact counted metadata")
	}
	for _, private := range []string{"test-alpha", "test-beta", "Synthetic Alpha", "Synthetic Beta"} {
		if strings.Contains(log, private) {
			t.Fatal("self-authored model values escaped the fixed summary")
		}
	}
	for _, changed := range []string{
		strings.Replace(syntheticCatalogAlpha, `"model":"magpie/alpha/test-alpha"`, `"model":"magpie/beta/test-beta"`, 1),
		strings.Replace(syntheticCatalogAlpha, "Synthetic Alpha [alpha]", "Synthetic Beta [beta]", 1),
		strings.ReplaceAll(syntheticCatalogBeta, "magpie/beta/test-beta", "magpie/alpha/test-beta"),
		strings.Replace(syntheticCatalogBeta, "Synthetic Beta [beta]", "Synthetic Beta [beta] ", 1),
	} {
		e, _ := observeModelCatalog(t, modelCatalogRequest, modelCatalogResponse(changed, "null"))
		if e.ModelCatalog == nil || e.ModelCatalog.Status != "partial" || e.ModelCatalog.Counts == nil ||
			*e.ModelCatalog.Counts != (modelCatalogCounts{Items: 1, Unknown: 1}) {
			t.Fatal("wrong-route or changed self-authored tuple became known")
		}
	}
}

func TestSelfAuthoredModelCatalogForwardingIsUnchanged(t *testing.T) {
	var log, forwarded bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(modelCatalogRequest))
	payload := []byte(modelCatalogResponse(syntheticCatalogAlpha+","+syntheticCatalogBeta, "null") + "\r\n")
	if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), 7}, &lineObserver{o: o, direction: "from_bridge"}); err != nil {
		t.Fatal("fragmented self-authored catalog forwarding failed")
	}
	if !bytes.Equal(payload, forwarded.Bytes()) || strings.Contains(log.String(), "test-alpha") || strings.Contains(log.String(), "test-beta") {
		t.Fatal("self-authored catalog wire bytes changed or raw values escaped")
	}
}
