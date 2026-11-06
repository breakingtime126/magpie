package appserverbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// Native error strings/data are opaque and may legitimately contain control
// escapes, unlike policy text. Check bounded structure and decoded duplicate
// keys without copying those values into diagnostics. This validator never
// relaxes the independent requirements, capability or trace validators.
func nativeReadJSON(data []byte, byteLimit, containerLimit int) bool {
	if len(data) > byteLimit || !utf8.Valid(data) || !nativeReadSurrogatesValid(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if !nativeReadJSONValue(d, 0, containerLimit) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func nativeReadJSONValue(d *json.Decoder, depth, containerLimit int) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	if depth >= requirementsJSONDepth {
		return false
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for count := 0; d.More(); count++ {
			if count >= containerLimit {
				return false
			}
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !nativeReadJSONValue(d, depth+1, containerLimit) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim('}')
	case '[':
		for count := 0; d.More(); count++ {
			if count >= containerLimit || !nativeReadJSONValue(d, depth+1, containerLimit) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim(']')
	default:
		return false
	}
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject those
// malformed escapes before decoding, but retain genuinely valid U+FFFD text
// in discarded native errors. Escaped backslashes never begin Unicode escapes.
func nativeReadSurrogatesValid(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xDC00 && value <= 0xDFFF {
			return false
		}
		if value < 0xD800 || value > 0xDBFF {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return true
}
