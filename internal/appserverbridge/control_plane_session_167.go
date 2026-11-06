package appserverbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"unicode/utf8"
)

// Bound the complete layered result, including wire escaping and both config
// copies. This leaves ample room for the already bounded client ID/envelope.
const controlPlaneResultLimit167 = 64 << 10

// A service-owned session layer is a real runtime authority, not a native file.
// Source oracle c40729ea5cf8e947481b130c59807273eda2f8e9:
// codex-rs/config/src/thread_config.rs maps service-owned SessionThreadConfig
// to SessionFlags; state.rs can also replace session plugins at runtime.
// This bridge owns only routing settings. No native execution policy is merged.
type controlPlaneSession167 struct {
	model   string
	config  json.RawMessage
	origins json.RawMessage
	layer   json.RawMessage
}

func (r *Router) enableControlPlane167() error {
	defaultModel := ""
	if _, found := r.models[r.defaultModel]; found {
		defaultModel = r.defaultModel
	}
	session, err := newControlPlaneSession167(r.controlPlaneSessionConfig167(defaultModel))
	if err != nil {
		return errors.New(controlPlaneUnavailable167)
	}
	r.controlSession167 = session
	r.controlPlaneRouting167 = true
	return nil
}

func newControlPlaneSession167(config object) (*controlPlaneSession167, error) {
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	if len(encoded) > controlPlaneResultLimit167 {
		return nil, errors.New("owned routing config exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	canonical, err := appendControlPlaneJSON167(nil, value)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	metadata := object{
		"name":    raw(object{"type": raw("sessionFlags")}),
		"version": raw("sha256:" + hex.EncodeToString(digest[:])),
	}
	origins := object{}
	controlPlaneOrigins167(value, "", raw(metadata), origins)
	layer := clone(metadata)
	layer["config"] = canonical
	session := &controlPlaneSession167{
		model:   text(config, "model"),
		config:  canonical,
		origins: raw(origins),
		layer:   raw(layer),
	}
	if len(raw(session.read(true))) > controlPlaneResultLimit167 {
		return nil, errors.New("owned layered routing result exceeds size limit")
	}
	return session, nil
}

func (s *controlPlaneSession167) read(includeLayers bool) object {
	result := object{
		"config":  append(json.RawMessage(nil), s.config...),
		"origins": append(json.RawMessage(nil), s.origins...),
	}
	if includeLayers {
		result["layers"] = raw([]json.RawMessage{s.layer})
	}
	return result
}

// Match config/src/fingerprint.rs record_origins: only leaves have origins;
// array indexes are path segments and empty containers contribute no origin.
// The owned schema has no quoted/dotted keys or structured feature aliases.
func controlPlaneOrigins167(value any, path string, metadata json.RawMessage, origins object) {
	childPath := func(key string) string {
		if path == "" {
			return key
		}
		return path + "." + key
	}
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			controlPlaneOrigins167(item, childPath(key), metadata, origins)
		}
	case []any:
		for index, item := range value {
			controlPlaneOrigins167(item, childPath(strconv.Itoa(index)), metadata, origins)
		}
	default:
		if path != "" {
			origins[path] = metadata
		}
	}
}

// Match the native version_for_toml input for this owned schema: recursively
// sorted, compact JSON with UTF-8 strings. encoding/json's HTML and U+2028/U+2029
// escaping differs from serde_json, so do not hash json.Marshal output.
// Only the primitive types actually used by the routing schema are supported.
func appendControlPlaneJSON167(out []byte, value any) ([]byte, error) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out = append(out, '{')
		for index, key := range keys {
			if index != 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendControlPlaneString167(out, key)
			if err != nil {
				return nil, err
			}
			out = append(out, ':')
			out, err = appendControlPlaneJSON167(out, value[key])
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	case []any:
		out = append(out, '[')
		for index, item := range value {
			if index != 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendControlPlaneJSON167(out, item)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case string:
		return appendControlPlaneString167(out, value)
	case bool:
		return strconv.AppendBool(out, value), nil
	case json.Number:
		number, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil {
			return nil, errors.New("non-integer owned config value")
		}
		return strconv.AppendInt(out, number, 10), nil
	default:
		return nil, errors.New("unsupported owned config value")
	}
}

func appendControlPlaneString167(out []byte, value string) ([]byte, error) {
	if !utf8.ValidString(value) {
		return nil, errors.New("invalid owned config text")
	}
	const hexDigits = "0123456789abcdef"
	out = append(out, '"')
	for _, char := range value {
		switch char {
		case '"', '\\':
			out = append(out, '\\', byte(char))
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if char < 0x20 {
				out = append(out, '\\', 'u', '0', '0', hexDigits[char>>4], hexDigits[char&0xf])
			} else {
				out = utf8.AppendRune(out, char)
			}
		}
	}
	return append(out, '"'), nil
}
