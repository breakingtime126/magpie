package provider

import (
	"encoding/json"
	"errors"
	"os"
	"slices"

	"github.com/yetone/magpie/internal/buildvariant"
)

var errNativeGPTBuiltin = errors.New("this build leaves ChatGPT accounts to the official Codex app")

// This is a built-in account boundary, not a model-name or hostname filter.
// API-key providers and third-party plugins retain their existing behavior.
func nativeGPTBuiltinBlocked(agent string) bool {
	return buildvariant.NativeCodex && (agent == "codex" || agent == ChatGPTAPIID)
}

// preserveNativeGPTRecords keeps protected records from the actual file, not a
// filtered in-memory account list. In this build no caller can add, change or
// remove those records through writeLogins. Unknown fields and duplicate account
// records are retained. Formatting of the surrounding JSON can still change.
// Existing file locking remains the caller's responsibility.
func preserveNativeGPTRecords(path string, proposed []byte) ([]byte, error) {
	if !buildvariant.NativeCodex {
		return proposed, nil
	}
	previous, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errNativeGPTBuiltin
	}
	if errors.Is(err, os.ErrNotExist) {
		previous = []byte("[]")
	}
	return preserveNativeGPTJSON(previous, proposed)
}

func preserveNativeGPTJSON(previous, proposed []byte) ([]byte, error) {
	var old, next []json.RawMessage
	if json.Unmarshal(previous, &old) != nil || json.Unmarshal(proposed, &next) != nil {
		return nil, errNativeGPTBuiltin
	}
	protected := func(value json.RawMessage) (bool, error) {
		var record struct {
			Agent string `json:"agent"`
		}
		if string(value) == "null" || json.Unmarshal(value, &record) != nil {
			return false, errNativeGPTBuiltin
		}
		return record.Agent == "codex" || record.Agent == ChatGPTAPIID, nil
	}
	// Compare the typed records that ordinary callers can edit before preserving
	// the raw originals. A deletion/import/change must fail, not appear to
	// succeed and then be silently undone. Unknown fields survive from disk.
	fingerprints := func(rows []json.RawMessage) ([]string, error) {
		var values []string
		for _, row := range rows {
			keep, err := protected(row)
			if err != nil {
				return nil, err
			}
			if keep {
				var record savedLogin
				if json.Unmarshal(row, &record) != nil {
					return nil, errNativeGPTBuiltin
				}
				b, err := json.Marshal(record)
				if err != nil {
					return nil, errNativeGPTBuiltin
				}
				values = append(values, string(b))
			}
		}
		slices.Sort(values)
		return values, nil
	}
	was, err := fingerprints(old)
	if err != nil {
		return nil, err
	}
	wanted, err := fingerprints(next)
	if err != nil || !slices.Equal(was, wanted) {
		return nil, errNativeGPTBuiltin
	}
	var out []json.RawMessage
	for _, row := range next {
		keep, err := protected(row)
		if err != nil {
			return nil, err
		}
		if !keep {
			out = append(out, row)
		}
	}
	for _, row := range old {
		keep, err := protected(row)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, row)
		}
	}
	return json.MarshalIndent(out, "", "  ")
}
