package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const (
	readTargetRootsEnv  = "MAGPIE_CI_READ_TARGET_ROOTS"
	maxReadTargetConfig = 16 << 10
	maxReadTargetPath   = 4096
)

type readTargetRoot struct {
	label string
	path  string
}

// These paths exist only in observer memory. They are harness declarations,
// never discovered, resolved, opened, hashed, or evidence of filesystem access.
type readTargetRoots struct {
	roots []readTargetRoot
}

func parseReadTargetRoots(value string) (*readTargetRoots, error) {
	bad := errors.New("invalid test-only read target roots")
	if len(value) == 0 || len(value) > maxReadTargetConfig {
		return nil, bad
	}
	fields, ok := authMetadataObject([]byte(value), 7)
	if !ok || len(fields) == 0 {
		return nil, bad
	}
	roots := &readTargetRoots{}
	for key, raw := range fields {
		label := ""
		switch key {
		case "primaryHome":
			label = "primary_home"
		case "primaryCodexHome":
			label = "primary_codex_home"
		case "secondaryHome":
			label = "secondary_home"
		case "secondaryCodexHome":
			label = "secondary_codex_home"
		case "controlHome":
			label = "control_home"
		case "workspace":
			label = "workspace"
		case "verifiedPackage":
			label = "verified_package"
		default:
			return nil, bad
		}
		raw = bytes.TrimSpace(raw)
		if !nativeJSONString(raw) {
			return nil, bad
		}
		var path string
		if json.Unmarshal(raw, &path) != nil {
			return nil, bad
		}
		canonical, valid := lexicalWindowsAbsolute(path, true)
		if !valid {
			return nil, bad
		}
		roots.roots = append(roots.roots, readTargetRoot{label, canonical})
	}
	return roots, nil
}

// Classify only strict client request shapes. This does not accept or route
// either RPC; the stdio tap forwards the original bytes even on invalid input.
func (o *observer) observedProjectList(raw []byte, e *event) {
	params, eligible, ok := observedReadParams(raw, "project/list", 4)
	if !eligible {
		return
	}
	valid := false
	e.ProjectListParamsValid = &valid
	if !ok {
		return
	}
	for key := range params {
		if key != "cursor" && key != "limit" && key != "sortKey" && key != "sortDirection" {
			return
		}
	}
	cursor, cursorOK := observedOptionalString(params["cursor"])
	limit, limitOK := observedOptionalUint32(params["limit"])
	sortKey, keyOK := observedReadSort(params["sortKey"], "position", "recencyAt")
	sortDirection, directionOK := observedReadSort(params["sortDirection"], "asc", "desc")
	if !cursorOK || !limitOK || !keyOK || !directionOK ||
		(sortDirection != "absent" && sortKey == "absent") {
		return
	}
	valid = true
	e.ProjectListCursorProvided = &cursor
	e.ProjectListLimitProvided = &limit
	e.ProjectListSortKey = sortKey
	e.ProjectListSortDirection = sortDirection
}

func observedReadSort(raw json.RawMessage, first, second string) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "absent", true
	}
	if !nativeJSONString(raw) {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	switch value {
	case first:
		return first, true
	case second:
		return second, true
	default:
		return "", false
	}
}

func (o *observer) observedFSReadFile(raw []byte, e *event) {
	params, eligible, ok := observedReadParams(raw, "fs/readFile", 1)
	if !eligible {
		return
	}
	valid := false
	e.FSReadParamsShapeValid = &valid
	if !ok || len(params) != 1 {
		return
	}
	pathRaw := bytes.TrimSpace(params["path"])
	if !nativeJSONString(pathRaw) {
		return
	}
	var path string
	if json.Unmarshal(pathRaw, &path) != nil || len(path) > maxReadTargetPath {
		return
	}
	valid = true
	if o.ownedModelCatalog != nil {
		matched := o.ownedModelCatalog.matchesPath(path)
		e.OwnedCatalogPathMatches = &matched
		return
	}
	e.FSReadPathForm = observedWindowsPathForm(path)
	e.FSReadBasename = observedReadBasename(path)
	e.FSReadLexicalRoot = o.readTargetRoots.relation(path)
}

// Path form is a spelling category, not native AbsolutePathBuf validity.
func observedWindowsPathForm(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	switch {
	case p == "":
		return "empty"
	case strings.HasPrefix(p, "//?/"), strings.HasPrefix(p, "//./"),
		strings.HasPrefix(p, "/??/"):
		return "device"
	case len(p) >= 2 && asciiDriveLetter(p[0]) && p[1] == ':':
		if len(p) >= 3 && p[2] == '/' {
			return "drive_absolute"
		}
		return "drive_relative"
	case strings.HasPrefix(p, "//"):
		return "unc_absolute"
	case strings.HasPrefix(p, "/"):
		return "rooted"
	default:
		return "relative"
	}
}

func observedReadBasename(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	base := p[strings.LastIndexByte(p, '/')+1:]
	// Exact ASCII case variants share a fixed label. Do not trim or resolve
	// components: trailing dots/spaces and streams are not known basenames.
	switch asciiPathFold(base) {
	case "config.toml":
		return "config.toml"
	case "agents.md":
		return "AGENTS.md"
	case "skill.md":
		return "SKILL.md"
	case "auth.json":
		return "auth.json"
	default:
		return "other"
	}
}

func (roots *readTargetRoots) relation(path string) string {
	canonical, ok := lexicalWindowsAbsolute(path, false)
	if !ok {
		return "unclassified"
	}
	if roots == nil || len(roots.roots) == 0 {
		return "unconfigured"
	}
	label, longest := "outside", -1
	for _, root := range roots.roots {
		// A path must equal the root or start at its next component.
		prefix := root.path
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if canonical != root.path && !strings.HasPrefix(canonical, prefix) {
			continue
		}
		if len(root.path) > longest {
			label, longest = root.label, len(root.path)
		} else if len(root.path) == longest {
			label = "ambiguous"
		}
	}
	return label
}

// A deliberately conservative, platform-independent lexical comparison.
// Slash and backslash are equivalent; ASCII case is folded. Non-ASCII bytes,
// device paths, drive-relative paths, ADS, dot/empty components, DOS aliases,
// reserved devices, and trailing dots/spaces are never matched. Root input
// alone may have one terminal separator. No filesystem APIs belong here.
func lexicalWindowsAbsolute(path string, root bool) (string, bool) {
	if len(path) == 0 || len(path) > maxReadTargetPath {
		return "", false
	}
	for i := 0; i < len(path); i++ {
		if path[i] < 0x20 || path[i] >= 0x7f {
			return "", false
		}
	}
	p := asciiPathFold(strings.ReplaceAll(path, "\\", "/"))
	form := observedWindowsPathForm(p)
	start := 0
	switch form {
	case "drive_absolute":
		start = 3
	case "unc_absolute":
		start = 2
	default:
		return "", false
	}
	if root && len(p) > start && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
		if len(p) == start {
			return "", false // Do not turn a doubled volume separator into a root.
		}
	}
	if len(p) == start {
		// Drive-volume roots are syntactically absolute; bare UNC prefixes
		// are not. A file target must still supply a component.
		return p, root && form == "drive_absolute"
	}
	parts := strings.Split(p[start:], "/")
	if form == "unc_absolute" && len(parts) < 2 {
		return "", false
	}
	for _, part := range parts {
		if !lexicalWindowsComponent(part) {
			return "", false
		}
	}
	return p, true
}

func lexicalWindowsComponent(part string) bool {
	if part == "" || part == "." || part == ".." ||
		strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") ||
		strings.ContainsAny(part, ":*?\"<>|~") {
		return false
	}
	stem, _, _ := strings.Cut(part, ".")
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "con", "prn", "aux", "nul", "conin$", "conout$":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "com") || strings.HasPrefix(stem, "lpt")) &&
		stem[3] >= '0' && stem[3] <= '9' {
		return false
	}
	return true
}

func asciiDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func asciiPathFold(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
