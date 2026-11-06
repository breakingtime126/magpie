package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
)

// One finite source-derived dictionary is shared with the PowerShell sanitizer.
// The pinned facts include upstream blob identities, not upstream source code.
// No runtime download, method discovery or registration is possible.
const rpcMethodFactsSHA256 = "2b7407b2015839aa6de58d6a5b0cfc601c058ad3be0447870182da063362f2e7"

//go:embed rpc_method_facts.json
var rpcMethodFacts []byte

var reviewedClientMethods = parseRPCMethodFacts(rpcMethodFacts)

func parseRPCMethodFacts(raw []byte) map[string]string {
	if len(raw) == 0 || len(raw) > 16384 {
		return nil
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != rpcMethodFactsSHA256 {
		return nil
	}
	var facts struct {
		Methods []string `json:"methods"`
	}
	if json.Unmarshal(raw, &facts) != nil || len(facts.Methods) != 183 {
		return nil
	}
	methods := make(map[string]string, len(facts.Methods))
	for i, method := range facts.Methods {
		if len(method) == 0 || len(method) > 41 || i > 0 && facts.Methods[i-1] >= method {
			return nil
		}
		methods[method] = method
	}
	return methods
}
