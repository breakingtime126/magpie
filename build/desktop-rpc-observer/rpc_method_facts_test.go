package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestPinnedRPCMethodFactsAreComplete(t *testing.T) {
	if bytes.ContainsAny(rpcMethodFacts, "\r\n") {
		t.Fatal("pinned RPC dictionary must be one line without a line ending")
	}
	hash := sha256.Sum256(rpcMethodFacts)
	if hex.EncodeToString(hash[:]) != rpcMethodFactsSHA256 {
		t.Fatal("pinned RPC dictionary bytes changed")
	}
	var facts struct {
		Schema     int    `json:"schema"`
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Sources    map[string]struct {
			BlobSHA1 string `json:"blobSHA1"`
			SHA256   string `json:"sha256"`
		} `json:"sources"`
		Counts  map[string]int `json:"counts"`
		Methods []string       `json:"methods"`
	}
	if json.Unmarshal(rpcMethodFacts, &facts) != nil || facts.Schema != 1 ||
		facts.Repository != "openai/codex" || facts.Commit != "c40729ea5cf8e947481b130c59807273eda2f8e9" ||
		len(facts.Methods) != 183 || len(reviewedClientMethods) != 183 ||
		len(facts.Counts) != 4 || facts.Counts["client"] != 183 || facts.Counts["stable"] != 106 ||
		facts.Counts["experimental"] != 74 || facts.Counts["deprecated"] != 3 {
		t.Fatal("pinned RPC dictionary provenance or complete counts changed")
	}
	// These blob pins were independently rehashed and reconciled at generation.
	// Offline tests intentionally do not fetch upstream or execute its source.
	sources := map[string]string{
		"codex-rs/app-server-protocol/src/protocol/common.rs":                  "bf2ebde276a5b66f57b537d518d0233a3a2f5ac4",
		"codex-rs/app-server-protocol/schema/json/ClientRequest.json":          "d03193a14e5eb7803dda9e2f888d32f7b99cf2f5",
		"codex-rs/app-server-protocol/src/export.rs":                           "6f51edce88aadd7a9ccbe77ab61da308e220831d",
		"codex-rs/app-server-protocol/src/protocol/v2/config.rs":               "09006baf7ee2811e7ad2983c76c43990feb9d6c4",
		"codex-rs/app-server-protocol/src/protocol/v2/experimental_feature.rs": "1ea8fc777901041fc16d7ec09c3cb5817b603922",
	}
	if len(facts.Sources) != len(sources) {
		t.Fatal("pinned RPC source set changed")
	}
	for path, blob := range sources {
		source, exists := facts.Sources[path]
		if !exists || source.BlobSHA1 != blob || !publicDigest(source.SHA256) {
			t.Fatal("pinned RPC source identity changed")
		}
	}
	for i, method := range facts.Methods {
		if allowedMethod(method) != method || len(method) > 41 ||
			i > 0 && facts.Methods[i-1] >= method {
			t.Fatal("complete pinned RPC vocabulary was not recognized exactly")
		}
		for _, poison := range []string{strings.ToUpper(method), method + " ", "PRIVATE_READ/" + method,
			method + "/PRIVATE_READ", method + "\x00", method + "\n"} {
			if allowedMethod(poison) != "unknown" {
				t.Fatal("pinned RPC method used non-exact recognition")
			}
		}
	}
	for _, method := range []string{"getAuthStatus", "gitDiffToRemote", "getConversationSummary",
		"server/diagnostics", "thread/sample", "mock/experimentalMethod"} {
		if reviewedClientMethods[method] != method {
			t.Fatal("filtered schema replaced the complete source vocabulary")
		}
	}
	for _, method := range []string{"initialized", "attestation/generate"} {
		if _, exists := reviewedClientMethods[method]; exists || allowedMethod(method) != method {
			t.Fatal("existing non-client label was merged into the client dictionary")
		}
	}
}

func TestPinnedRPCMethodFactsRejectAnyChangedBytes(t *testing.T) {
	for _, raw := range [][]byte{
		nil, {}, []byte("PRIVATE_READ"), append(append([]byte(nil), rpcMethodFacts...), '\n'),
		bytes.Replace(rpcMethodFacts, []byte("thread/sample"), []byte("PRIVATE_READ"), 1),
		bytes.Repeat([]byte("PRIVATE_READ"), 16384),
	} {
		if parseRPCMethodFacts(raw) != nil {
			t.Fatal("changed RPC dictionary became an empty or partial trusted vocabulary")
		}
	}
}
