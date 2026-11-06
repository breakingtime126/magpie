package main

import "strings"

// This is a conservative subset of the whole-key trim performed by native
// CliConfigOverrides, not TOML key parsing or Unicode normalization.
// Pinned source: openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9,
// codex-rs/utils/cli/src/config_override.rs and codex-rs/config/src/overrides.rs.
const observedConfigASCIIWhitespace = " \t\r\n\v\f"

// Output order is fixed. No input byte, position, count, length or hash is
// emitted. The "none" sentinel is emitted alone when no class is present.
var observedConfigKeyClassOrder = [...]string{
	"at_sign",
	"slash",
	"backslash",
	"colon",
	"ascii_space",
	"ascii_control",
	"non_ascii",
	"other_ascii_punctuation",
}

func observedUnknownConfigArgument(key, value, shape, family string) event {
	return event{
		Kind: "argv_config", Class: "unknown", ConfigShape: shape, ConfigFamily: family,
		ConfigLiteral: observedConfigValue("bool", value),
		ConfigLexicalRoot: observedConfigLexicalRoot(key), ConfigKeyClasses: observedConfigKeyClasses(key),
	}
}

// This is only a lexical prefix, not a semantic family or a parsed path.
// A fixed root followed immediately by a literal dot is required. The suffix
// may be malformed and is never returned or inspected for this classification.
func observedConfigLexicalRoot(key string) string {
	key = strings.Trim(key, observedConfigASCIIWhitespace)
	first, _, hasDot := strings.Cut(key, ".")
	if hasDot {
		if fixed, known := observedConfigFamilies[first]; known {
			return fixed
		}
	}
	return "other"
}

// Only bytes before the assignment's first '=' are supplied here. SP/TAB are
// ascii_space; other ASCII controls (including DEL) are ascii_control.
// All bytes >= 0x80 are non_ascii, without decoding Unicode or invalid UTF-8.
// Remaining printable ASCII outside bare-key characters and dot is punctuation.
func observedConfigKeyClasses(key string) []string {
	var present [len(observedConfigKeyClassOrder)]bool
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch c {
		case '@':
			present[0] = true
		case '/':
			present[1] = true
		case '\\':
			present[2] = true
		case ':':
			present[3] = true
		case ' ', '\t':
			present[4] = true
		default:
			switch {
			case c < 0x20 || c == 0x7f:
				present[5] = true
			case c >= 0x80:
				present[6] = true
			case !observedConfigKeyByte(c) && c != '.':
				present[7] = true
			}
		}
	}
	var classes []string
	for i, class := range observedConfigKeyClassOrder {
		if present[i] {
			classes = append(classes, class)
		}
	}
	if len(classes) == 0 {
		return []string{"none"}
	}
	return classes
}
