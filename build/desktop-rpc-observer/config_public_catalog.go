package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const publicPluginCatalogEnv = "MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES"

// This is a diagnostic dictionary supplied only after the CI harness has bound
// each public source to the signed, hash-verified package. A digest identifies
// the dictionary; it does not authenticate a plugin or grant any capability.
// Runtime records expose only a table index, never a key from this dictionary.
type publicPluginCatalog struct {
	keys   map[string]int
	digest string
}

type publicPluginCatalogWire struct {
	Schema        int      `json:"schema"`
	PackageSHA256 string   `json:"packageSha256"`
	ArchiveSHA256 string   `json:"archiveSha256"`
	KeysSHA256    string   `json:"keysSha256"`
	Keys          []string `json:"keys"`
}

func publicPluginKey(key string) bool {
	if len(key) > 128 {
		return false
	}
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != "plugins" ||
		(parts[2] != "enabled" && parts[2] != "auto_provision") {
		return false
	}
	identity := strings.Split(parts[1], "@")
	if len(identity) != 2 {
		return false
	}
	for _, segment := range identity {
		if len(segment) == 0 || len(segment) > 64 {
			return false
		}
		for i := 0; i < len(segment); i++ {
			c := segment[i]
			alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
			if !alnum && (i == 0 || c != '_' && c != '-') {
				return false
			}
		}
	}
	return true
}

func publicDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

func parsePublicPluginCatalog(value string) (*publicPluginCatalog, error) {
	bad := errors.New("invalid public plugin catalog")
	if value == "" || len(value) > 16384 {
		return nil, bad
	}
	var wire publicPluginCatalogWire
	if json.Unmarshal([]byte(value), &wire) != nil || wire.Schema != 1 ||
		!publicDigest(wire.PackageSHA256) || !publicDigest(wire.ArchiveSHA256) ||
		!publicDigest(wire.KeysSHA256) || len(wire.Keys) == 0 || len(wire.Keys) > 128 {
		return nil, bad
	}
	// Only canonical compact JSON in this field order is accepted. This also
	// rejects ignored/duplicate fields, alternate field casing and escapes.
	canonical, err := json.Marshal(wire)
	if err != nil || string(canonical) != value {
		return nil, bad
	}
	keys := make(map[string]int, len(wire.Keys))
	for i, key := range wire.Keys {
		if !publicPluginKey(key) || i > 0 && wire.Keys[i-1] >= key {
			return nil, bad
		}
		keys[key] = i + 1
	}
	digest := sha256.Sum256([]byte(strings.Join(wire.Keys, "\n")))
	if hex.EncodeToString(digest[:]) != wire.KeysSHA256 {
		return nil, bad
	}
	return &publicPluginCatalog{keys: keys, digest: wire.KeysSHA256}, nil
}

func (catalog *publicPluginCatalog) argument(argument string) (event, bool) {
	key, value, hasEquals := strings.Cut(argument, "=")
	if !hasEquals {
		return event{}, false
	}
	ref, known := catalog.keys[strings.Trim(key, observedConfigASCIIWhitespace)]
	if !known {
		return event{}, false
	}
	return event{
		Kind: "argv_config", PublicPluginRef: ref,
		ConfigValue: observedConfigValue("bool", value),
	}, true
}
