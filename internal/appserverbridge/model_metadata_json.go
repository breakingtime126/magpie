package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode"
	"unicode/utf8"
)

const modelMetadataJSONLimit = 64 << 10
const modelMetadataJSONDepth = 12
const modelMetadataContainerLimit = 256

var errModelMetadataJSON = errors.New("model metadata JSON invalid")
var errModelMetadataJSONLimit = errors.New("model metadata JSON limit exceeded")

// Model metadata is display text, not a policy/token string. Preserve ordinary
// escaped line breaks and tabs in string values. Keys retain startupText's
// stricter rule; duplicate decoded keys, malformed Unicode, all other controls,
// trailing data and the existing structural/size bounds remain rejected.
// Policy validators are deliberately not used or changed here.
func validateModelMetadataJSON(data []byte) error {
	if len(data) > modelMetadataJSONLimit {
		return errModelMetadataJSONLimit
	}
	if !utf8.Valid(data) {
		return errModelMetadataJSON
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := modelMetadataJSONValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errModelMetadataJSON
	}
	return nil
}

func modelMetadataText(value string) bool {
	if !utf8.ValidString(value) { return false }
	for _, r := range value {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') {
			return false
		}
	}
	return true
}

func modelMetadataJSONValue(d *json.Decoder, depth int) error {
	token, err := d.Token()
	if err != nil { return errModelMetadataJSON }
	if value, ok := token.(string); ok {
		if !modelMetadataText(value) { return errModelMetadataJSON }
		return nil
	}
	delim, container := token.(json.Delim)
	if !container { return nil }
	if depth >= modelMetadataJSONDepth { return errModelMetadataJSONLimit }
	switch delim {
	case '{':
		seen := map[string]bool{}
		for count := 0; d.More(); count++ {
			if count >= modelMetadataContainerLimit { return errModelMetadataJSONLimit }
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || !startupText(name) || seen[name] { return errModelMetadataJSON }
			seen[name] = true
			if err := modelMetadataJSONValue(d, depth+1); err != nil { return err }
		}
		last, err := d.Token()
		if err != nil || last != json.Delim('}') { return errModelMetadataJSON }
	case '[':
		for count := 0; d.More(); count++ {
			if count >= modelMetadataContainerLimit { return errModelMetadataJSONLimit }
			if err := modelMetadataJSONValue(d, depth+1); err != nil { return err }
		}
		last, err := d.Token()
		if err != nil || last != json.Delim(']') { return errModelMetadataJSON }
	default:
		return errModelMetadataJSON
	}
	return nil
}
