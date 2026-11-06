package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func targetRequest(method, params string) string {
	return "{\"id\":\"PRIVATE_TARGET_ID\",\"method\":\"" + method + "\",\"params\":" + params + "}"
}

func fsTargetParams(path string) string {
	raw, _ := json.Marshal(map[string]string{"path": path})
	return string(raw)
}

func targetRootsFixture(t *testing.T, values map[string]string) *readTargetRoots {
	t.Helper()
	raw, _ := json.Marshal(values)
	roots, err := parseReadTargetRoots(string(raw))
	if err != nil {
		t.Fatal("synthetic target roots were rejected")
	}
	return roots
}

func targetRootRecord(t *testing.T, roots *readTargetRoots, path string) map[string]json.RawMessage {
	t.Helper()
	var log bytes.Buffer
	o := newObserver(&log)
	o.readTargetRoots = roots
	o.line("to_bridge", []byte(targetRequest("fs/readFile", fsTargetParams(path))))
	if bytes.Contains(log.Bytes(), []byte("PRIVATE_TARGET")) {
		t.Fatal("target or declared root text leaked")
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(log.Bytes()), &record) != nil {
		t.Fatal("target metadata was not valid JSON")
	}
	return record
}

func fsTargetWant(form, base, root string) map[string]string {
	return map[string]string{
		"fsReadParamsShapeValid": "true", "fsReadPathForm": "\"" + form + "\"",
		"fsReadBasename": "\"" + base + "\"", "fsReadLexicalRoot": "\"" + root + "\"",
	}
}

func TestProjectListStrictOptionalValues(t *testing.T) {
	for _, cursor := range []string{"", "\"cursor\":null", "\"cursor\":\"\"", "\"cursor\":\"PRIVATE_TARGET_CURSOR\"",
		"\"cursor\":\"\\ud83d\\ude00\"", "\"cursor\":\"\\ufffd\""} {
		for _, limit := range []string{"", "\"limit\":null", "\"limit\":0", "\"limit\":4294967295"} {
			for _, key := range []string{"", "\"sortKey\":null", "\"sortKey\":\"position\"", "\"sortKey\":\"recencyAt\""} {
				for _, direction := range []string{"", "\"sortDirection\":null", "\"sortDirection\":\"asc\"", "\"sortDirection\":\"desc\""} {
					fields := []string{}
					for _, field := range []string{cursor, limit, key, direction} {
						if field != "" {
							fields = append(fields, field)
						}
					}
					record := targetMetadataRecord(t, "to_bridge", targetRequest("project/list", "{"+strings.Join(fields, ",")+"}"))
					keyAbsent := key == "" || key == "\"sortKey\":null"
					directionAbsent := direction == "" || direction == "\"sortDirection\":null"
					if keyAbsent && !directionAbsent {
						assertTargetMetadata(t, record, map[string]string{"projectListParamsValid": "false"})
						continue
					}
					keyLabel, directionLabel := "\"absent\"", "\"absent\""
					if !keyAbsent {
						_, keyLabel, _ = strings.Cut(key, ":")
					}
					if !directionAbsent {
						_, directionLabel, _ = strings.Cut(direction, ":")
					}
					cursorFlag, limitFlag := "false", "false"
					if cursor != "" && cursor != "\"cursor\":null" {
						cursorFlag = "true"
					}
					if limit != "" && limit != "\"limit\":null" {
						limitFlag = "true"
					}
					assertTargetMetadata(t, record, map[string]string{
						"projectListParamsValid": "true", "projectListCursorProvided": cursorFlag,
						"projectListLimitProvided": limitFlag, "projectListSortKey": keyLabel,
						"projectListSortDirection": directionLabel,
					})
				}
			}
		}
	}
	record := targetMetadataRecord(t, "to_bridge",
		"{\"id\":1,\"method\":\"project\\u002flist\",\"params\":{\"\\u0063ursor\":null,\"li\\u006dit\":0,\"sort\\u004bey\":\"recency\\u0041t\",\"sort\\u0044irection\":\"d\\u0065sc\"},\"trace\":null}")
	assertTargetMetadata(t, record, map[string]string{
		"projectListParamsValid": "true", "projectListCursorProvided": "false",
		"projectListLimitProvided": "true", "projectListSortKey": "\"recencyAt\"",
		"projectListSortDirection": "\"desc\"", "tracePresent": "true", "traceValid": "true",
	})
}

func TestTargetInvalidParamsEmitNoPartialMetadata(t *testing.T) {
	project := []string{
		"null", "false", "[]", "1", "{\"PRIVATE_TARGET_KEY\":null}",
		"{\"cursor\":0}", "{\"cursor\":false}", "{\"cursor\":[]}", "{\"cursor\":{}}",
		"{\"cursor\":\"\\ud800\"}", "{\"cursor\":\"\\udfff\"}",
		"{\"cursor\":null,\"\\u0063ursor\":\"PRIVATE_TARGET\"}",
		"{\"limit\":0,\"li\\u006dit\":1}", "{\"sortKey\":null,\"sort\\u004bey\":\"position\"}",
		"{\"sortDirection\":null,\"sort\\u0044irection\":\"asc\"}",
		"{\"sortKey\":\"POSITION\"}", "{\"sortKey\":\"private\"}", "{\"sortDirection\":\"ascending\"}",
		"{\"sortKey\":0}", "{\"sortKey\":true}", "{\"sortKey\":[]}", "{\"sortKey\":{}}",
		"{\"sortDirection\":0}", "{\"sortDirection\":true}", "{\"sortDirection\":[]}", "{\"sortDirection\":{}}",
		"{\"SortKey\":\"position\"}", "{\"sort_key\":\"position\"}",
		"{\"sortKey\":\"position\",\"sortDirection\":\"\\ud800\"}",
		"{\"cursor\":\"PRIVATE_TARGET\",\"limit\":0,\"sortKey\":\"position\",\"sortDirection\":\"asc\",\"PRIVATE_TARGET\":null}",
	}
	for _, limit := range []string{"false", "\"0\"", "-1", "-0", "4294967296", "1.0", "1e0", "0.5", "[]", "{}"} {
		project = append(project, "{\"cursor\":\"PRIVATE_TARGET\",\"limit\":"+limit+"}")
	}
	fs := []string{
		"null", "[]", "false", "1", "{}", "{\"path\":null}", "{\"path\":0}", "{\"path\":false}",
		"{\"path\":[]}", "{\"path\":{}}", "{\"Path\":\"PRIVATE_TARGET\"}",
		"{\"path\":\"\\ud800\"}", "{\"path\":\"\\udfff\"}", "{\"path\":\"\\ud800\\u0041\"}",
		"{\"path\":\"C:/PRIVATE_TARGET/config.toml\",\"\\u0070ath\":\"C:/PRIVATE_TARGET/AGENTS.md\"}",
		"{\"path\":\"C:/PRIVATE_TARGET/config.toml\",\"PRIVATE_TARGET\":null}",
		fsTargetParams(strings.Repeat("a", maxReadTargetPath+1)),
	}
	for method, values := range map[string][]string{"project/list": project, "fs/readFile": fs} {
		field := "projectListParamsValid"
		if method == "fs/readFile" {
			field = "fsReadParamsShapeValid"
		}
		for _, params := range values {
			record := targetMetadataRecord(t, "to_bridge", targetRequest(method, params))
			assertTargetMetadata(t, record, map[string]string{field: "false"})
		}
		record := targetMetadataRecord(t, "to_bridge", "{\"id\":1,\"method\":\""+method+"\"}")
		assertTargetMetadata(t, record, map[string]string{field: "false"})
	}
}

func TestTargetWindowsFormsAreSpellingOnly(t *testing.T) {
	for _, tc := range []struct {
		path string
		form string
		base string
		root string
	}{
		{"", "empty", "other", "unclassified"},
		{"config.toml", "relative", "config.toml", "unclassified"},
		{"C:config.toml", "drive_relative", "other", "unclassified"},
		{"/PRIVATE_TARGET/AGENTS.md", "rooted", "AGENTS.md", "unclassified"},
		{"\\PRIVATE_TARGET\\SKILL.md", "rooted", "SKILL.md", "unclassified"},
		{"C:/PRIVATE_TARGET/config.toml", "drive_absolute", "config.toml", "unconfigured"},
		{"c:\\PRIVATE_TARGET\\aGeNtS.Md", "drive_absolute", "AGENTS.md", "unconfigured"},
		{"//PRIVATE_TARGET_HOST/share/auth.json", "unc_absolute", "auth.json", "unconfigured"},
		{"\\\\PRIVATE_TARGET_HOST\\share\\SKILL.md", "unc_absolute", "SKILL.md", "unconfigured"},
		{"\\\\?\\C:\\PRIVATE_TARGET\\config.toml", "device", "config.toml", "unclassified"},
		{"\\\\.\\C:\\PRIVATE_TARGET\\config.toml", "device", "config.toml", "unclassified"},
		{"\\??\\C:\\PRIVATE_TARGET\\config.toml", "device", "config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/config.toml:stream", "drive_absolute", "other", "unclassified"},
		{"C:/PRIVATE_TARGET/AGENTS.md ", "drive_absolute", "other", "unclassified"},
		{"C:/PRIVATE_TARGET/SKILL.md.", "drive_absolute", "other", "unclassified"},
		{"C:/PRIVATE_TARGET/other.txt", "drive_absolute", "other", "unconfigured"},
		{"C:/PRIVATE_TARGET/\u00e9/config.toml", "drive_absolute", "config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/\x00/auth.json", "drive_absolute", "auth.json", "unclassified"},
		{"C:/PRIVATE_TARGET/\ufffd/auth.json", "drive_absolute", "auth.json", "unclassified"},
	} {
		record := targetRootRecord(t, nil, tc.path)
		assertTargetMetadata(t, record, fsTargetWant(tc.form, tc.base, tc.root))
	}
	record := targetMetadataRecord(t, "to_bridge",
		"{\"id\":1,\"method\":\"fs\\u002freadFile\",\"params\":{\"\\u0070ath\":\"C:\\\\PRIVATE_TARGET\\\\config\\u002etoml\"}}")
	assertTargetMetadata(t, record, fsTargetWant("drive_absolute", "config.toml", "unconfigured"))
	boundary := "C:/" + strings.Repeat("a", maxReadTargetPath-3)
	assertTargetMetadata(t, targetRootRecord(t, nil, boundary), fsTargetWant("drive_absolute", "other", "unconfigured"))
}

func TestTargetRootsMatchOnlySafeLexicalComponents(t *testing.T) {
	roots := targetRootsFixture(t, map[string]string{
		"primaryHome": "C:/PRIVATE_TARGET/primary", "primaryCodexHome": "C:/PRIVATE_TARGET/primary/.codex",
		"secondaryHome": "C:/PRIVATE_TARGET/secondary", "secondaryCodexHome": "C:/PRIVATE_TARGET/secondary/.codex",
		"workspace": "D:/PRIVATE_TARGET/work/", "verifiedPackage": "//PRIVATE_TARGET_HOST/share/package",
		"controlHome": "C:/PRIVATE_TARGET/control",
	})
	for _, tc := range []struct {
		path string
		root string
	}{
		{"c:\\private_target\\PRIMARY\\config.toml", "primary_home"},
		{"C:/PRIVATE_TARGET/primary/.codex/config.toml", "primary_codex_home"},
		{"C:/PRIVATE_TARGET/secondary/AGENTS.md", "secondary_home"},
		{"C:/PRIVATE_TARGET/secondary/.codex/auth.json", "secondary_codex_home"},
		{"C:/PRIVATE_TARGET/control", "control_home"},
		{"C:/PRIVATE_TARGET/control/state/config.toml", "control_home"},
		{"C:/PRIVATE_TARGET/control-other/config.toml", "outside"},
		{"d:\\PRIVATE_TARGET\\work\\SKILL.md", "workspace"},
		{"\\\\private_target_host\\SHARE\\package\\SKILL.md", "verified_package"},
		{"C:/PRIVATE_TARGET/primary", "primary_home"},
		{"C:/PRIVATE_TARGET/primary/.codex", "primary_codex_home"},
		{"C:/PRIVATE_TARGET/primary-other/config.toml", "outside"},
		{"C:/PRIVATE_TARGET/primary/.codex-other/auth.json", "primary_home"},
		{"D:/PRIVATE_TARGET/work2/SKILL.md", "outside"},
		{"//PRIVATE_TARGET_HOST/share/package2/SKILL.md", "outside"},
		{"//PRIVATE_TARGET_HOST/share2/package/SKILL.md", "outside"},
		{"E:/PRIVATE_TARGET/primary/config.toml", "outside"},
		{"C:/PRIVATE_TARGET/primary/../secondary/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/./config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary//config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary /config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary./config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/config.toml:PRIVATE_TARGET", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/CONFIG~1.TOM", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/NUL/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/CON.txt/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/LPT1/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/COM9.txt/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/conin$/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/CON .txt/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/?/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/*/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/\x7f/config.toml", "unclassified"},
		{"C:/PRIVATE_TARGET/primary/config.toml/", "unclassified"},
		{"\\\\?\\C:\\PRIVATE_TARGET\\primary\\config.toml", "unclassified"},
		{"\\\\.\\C:\\PRIVATE_TARGET\\primary\\config.toml", "unclassified"},
		{"C:PRIVATE_TARGET/primary/config.toml", "unclassified"},
		{"/PRIVATE_TARGET/primary/config.toml", "unclassified"},
	} {
		record := targetRootRecord(t, roots, tc.path)
		assertTargetMetadata(t, record, fsTargetWant(observedWindowsPathForm(tc.path), observedReadBasename(tc.path), tc.root))
	}
}

func TestTargetRootOverlapsAreExplicitAndOrderIndependent(t *testing.T) {
	roots := targetRootsFixture(t, map[string]string{
		"primaryHome": "C:/PRIVATE_TARGET/root", "workspace": "c:\\private_target\\ROOT\\",
		"primaryCodexHome": "C:/PRIVATE_TARGET/root/.codex",
	})
	if roots.relation("C:/PRIVATE_TARGET/root/config.toml") != "ambiguous" {
		t.Fatal("equal most-specific roots chose an arbitrary label")
	}
	if roots.relation("C:/PRIVATE_TARGET/root/.codex/config.toml") != "primary_codex_home" {
		t.Fatal("more-specific root was hidden by an overlapping parent")
	}
	for i, j := 0, len(roots.roots)-1; i < j; i, j = i+1, j-1 {
		roots.roots[i], roots.roots[j] = roots.roots[j], roots.roots[i]
	}
	if roots.relation("c:\\private_target\\ROOT\\config.toml") != "ambiguous" ||
		roots.relation("c:\\private_target\\ROOT\\.codex\\config.toml") != "primary_codex_home" {
		t.Fatal("root iteration order changed lexical classification")
	}
	volume := targetRootsFixture(t, map[string]string{"workspace": "D:\\"})
	if volume.relation("D:/PRIVATE_TARGET/config.toml") != "workspace" ||
		volume.relation("D:PRIVATE_TARGET/config.toml") != "unclassified" {
		t.Fatal("drive-volume root boundary was not preserved")
	}
}

func TestTargetControlHomeOverlapsRemainLexical(t *testing.T) {
	roots := targetRootsFixture(t, map[string]string{
		"controlHome": "C:/PRIVATE_TARGET/control", "workspace": "C:/PRIVATE_TARGET/control/work",
	})
	assertTargetMetadata(t, targetRootRecord(t, roots, "c:\\private_target\\CONTROL\\config.toml"),
		fsTargetWant("drive_absolute", "config.toml", "control_home"))
	assertTargetMetadata(t, targetRootRecord(t, roots, "C:/PRIVATE_TARGET/control/work/AGENTS.md"),
		fsTargetWant("drive_absolute", "AGENTS.md", "workspace"))
	assertTargetMetadata(t, targetRootRecord(t, roots, "C:/PRIVATE_TARGET/control/work-other/SKILL.md"),
		fsTargetWant("drive_absolute", "SKILL.md", "control_home"))
	equal := targetRootsFixture(t, map[string]string{
		"controlHome": "C:/PRIVATE_TARGET/control", "primaryHome": "c:\\private_target\\CONTROL\\",
	})
	assertTargetMetadata(t, targetRootRecord(t, equal, "C:/PRIVATE_TARGET/control/auth.json"),
		fsTargetWant("drive_absolute", "auth.json", "ambiguous"))
}

func TestTargetRootConfigIsBoundedStrictAndNonDisclosing(t *testing.T) {
	badValues := []string{
		"", "null", "[]", "{}", "false", "{\"workspace\":null}", "{\"workspace\":0}",
		"{\"workspace\":true}", "{\"workspace\":[]}", "{\"workspace\":{}}",
		"{\"controlHome\":null}", "{\"ControlHome\":\"C:/PRIVATE_TARGET\"}",
		"{\"controlHome\":\"C:/PRIVATE_TARGET\",\"control\\u0048ome\":\"D:/PRIVATE_TARGET\"}",
		"{\"PRIVATE_TARGET\":\"C:/PRIVATE_TARGET\"}", "{\"Workspace\":\"C:/PRIVATE_TARGET\"}",
		"{\"workspace\":\"C:/PRIVATE_TARGET\",\"work\\u0073pace\":\"D:/PRIVATE_TARGET\"}",
		"{\"workspace\":\"\\ud800\"}", "{\"workspace\":\"\\udfff\"}",
		"{\"workspace\":\"C:/PRIVATE_TARGET\"} false",
		"{\"workspace\":\"C:/PRIVATE_TARGET\",\"primaryHome\":\"C:/PRIVATE_TARGET\",\"primaryCodexHome\":\"C:/PRIVATE_TARGET\",\"secondaryHome\":\"C:/PRIVATE_TARGET\",\"secondaryCodexHome\":\"C:/PRIVATE_TARGET\",\"verifiedPackage\":\"C:/PRIVATE_TARGET\",\"controlHome\":\"C:/PRIVATE_TARGET\",\"PRIVATE_TARGET\":\"C:/PRIVATE_TARGET\"}",
	}
	for _, path := range []string{
		"", "relative", "C:relative", "/PRIVATE_TARGET", "//PRIVATE_TARGET", "C://", "\\\\?\\C:\\PRIVATE_TARGET",
		"\\\\.\\C:\\PRIVATE_TARGET", "C:/PRIVATE_TARGET/..", "C:/PRIVATE_TARGET/.", "C:/PRIVATE_TARGET//child",
		"C:/PRIVATE_TARGET//", "C:/PRIVATE_TARGET.", "C:/PRIVATE_TARGET ", "C:/PRIVATE_TARGET:stream",
		"C:/PRIVATE_TARGET/NUL", "C:/PRIVATE_TARGET/PRIVAT~1", "C:/PRIVATE_TARGET/\u00e9",
		"C:/PRIVATE_TARGET/\x00", "C:/PRIVATE_TARGET/\xff", "C:/" + strings.Repeat("a", maxReadTargetPath-2),
	} {
		raw, _ := json.Marshal(map[string]string{"workspace": path})
		badValues = append(badValues, string(raw))
	}
	badValues = append(badValues, string([]byte{'{', '"', 'w', '"', ':', '"', 0xff, '"', '}'}))
	for _, value := range badValues {
		roots, err := parseReadTargetRoots(value)
		if err == nil || roots != nil || err.Error() != "invalid test-only read target roots" {
			t.Fatal("invalid target root config did not fail with a fixed error")
		}
	}
	minimal := "{\"workspace\":\"C:/PRIVATE_TARGET\"}"
	padded := minimal + strings.Repeat(" ", maxReadTargetConfig-len(minimal))
	if _, err := parseReadTargetRoots(padded); err != nil {
		t.Fatal("exact root-config byte bound rejected")
	}
	if roots, err := parseReadTargetRoots(padded + " "); err == nil || roots != nil {
		t.Fatal("oversized root-config byte bound accepted")
	}
	boundary, _ := json.Marshal(map[string]string{"workspace": "C:/" + strings.Repeat("a", maxReadTargetPath-3)})
	if _, err := parseReadTargetRoots(string(boundary)); err != nil {
		t.Fatal("exact decoded root-path byte bound rejected")
	}
	escaped := "{\"work\\u0073pace\":\"C:\\\\PRIVATE_TARGET\\\\root\\\\\"}"
	if roots, err := parseReadTargetRoots(escaped); err != nil || roots.relation("c:/private_target/root/config.toml") != "workspace" {
		t.Fatal("single decoded key or separator alias rejected")
	}
}

func TestTargetEnvelopeTraceAndDirectionStayOpaque(t *testing.T) {
	for _, method := range []string{"project/list", "fs/readFile"} {
		params := "{}"
		if method == "fs/readFile" {
			params = fsTargetParams("C:/PRIVATE_TARGET/config.toml")
		}
		valid := targetRequest(method, params)
		for _, direction := range []string{"from_bridge", "stderr"} {
			assertTargetMetadata(t, targetMetadataRecord(t, direction, valid), nil)
		}
		for _, raw := range []string{
			"{\"method\":\"" + method + "\",\"params\":" + params + "}",
			"{\"id\":null,\"method\":\"" + method + "\",\"params\":" + params + "}",
			strings.Replace(valid, "\"PRIVATE_TARGET_ID\"", "1.5", 1),
			strings.Replace(valid, "\"PRIVATE_TARGET_ID\"", "1e2", 1),
			strings.Replace(valid, "\"PRIVATE_TARGET_ID\"", "9223372036854775808", 1),
			strings.Replace(valid, "\"PRIVATE_TARGET_ID\"", "\"\\ud800\"", 1),
			strings.Replace(valid, "\"PRIVATE_TARGET_ID\"", "\""+strings.Repeat("a", 257)+"\"", 1),
			valid[:len(valid)-1] + ",\"id\":2}",
			valid[:len(valid)-1] + ",\"\\u0069d\":2}",
			valid[:len(valid)-1] + ",\"method\":\"" + method + "\"}",
			valid[:len(valid)-1] + ",\"params\":" + params + "}",
			valid[:len(valid)-1] + ",\"trace\":null,\"tr\\u0061ce\":{}}",
			valid[:len(valid)-1] + ",\"jsonrpc\":\"2.0\"}",
			valid[:len(valid)-1] + ",\"result\":null}",
			valid[:len(valid)-1] + ",\"error\":null}",
			valid[:len(valid)-1] + ",\"PRIVATE_TARGET\":null}",
			"{\"id\":1,\"result\":{\"path\":\"C:/PRIVATE_TARGET/config.toml\"}}",
			"{\"id\":1,\"error\":{\"code\":-32601,\"message\":\"PRIVATE_TARGET\"}}",
			strings.Replace(valid, "PRIVATE_TARGET_ID", string([]byte{0xff}), 1),
		} {
			assertTargetMetadata(t, targetMetadataRecord(t, "to_bridge", raw), nil)
		}
		for _, trace := range []string{
			"false", "[]", "{\"PRIVATE_TARGET\":null}", "{\"traceparent\":0}",
			"{\"traceparent\":\"\\ud800\"}", "{\"traceparent\":\"\\ufffd\"}",
			"{\"traceparent\":\"PRIVATE_TARGET\\n\"}", "{\"traceparent\":\"" + strings.Repeat("a", 1025) + "\"}",
			"{\"traceparent\":null,\"trace\\u0070arent\":\"PRIVATE_TARGET\"}",
		} {
			raw := valid[:len(valid)-1] + ",\"trace\":" + trace + "}"
			assertTargetMetadata(t, targetMetadataRecord(t, "to_bridge", raw),
				map[string]string{"tracePresent": "true", "traceValid": "false"})
		}
		for _, trace := range []string{"null", "{}", "{\"traceparent\":\"PRIVATE_TARGET_TRACE\"}"} {
			record := targetMetadataRecord(t, "to_bridge", valid[:len(valid)-1]+",\"trace\":"+trace+"}")
			want := map[string]string{"projectListParamsValid": "true", "projectListCursorProvided": "false",
				"projectListLimitProvided": "false", "projectListSortKey": "\"absent\"", "projectListSortDirection": "\"absent\""}
			if method == "fs/readFile" {
				want = fsTargetWant("drive_absolute", "config.toml", "unconfigured")
			}
			want["tracePresent"], want["traceValid"] = "true", "true"
			assertTargetMetadata(t, record, want)
		}
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(valid))
		log.Reset()
		o.line("to_bridge", []byte(valid))
		o.line("to_bridge", []byte(valid))
		o.line("from_bridge", []byte("{\"id\":\"PRIVATE_TARGET_ID\",\"result\":{\"PRIVATE_TARGET\":\"content\"}}"))
		for _, raw := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
			var record map[string]json.RawMessage
			if json.Unmarshal(raw, &record) != nil {
				t.Fatal("duplicate request metadata was not JSON")
			}
			assertTargetMetadata(t, record, nil)
		}
		log.Reset()
		o.correlationDisabled = true
		o.line("to_bridge", []byte(strings.Replace(valid, "PRIVATE_TARGET_ID", "PRIVATE_TARGET_NEW_ID", 1)))
		var record map[string]json.RawMessage
		if json.Unmarshal(bytes.TrimSpace(log.Bytes()), &record) != nil {
			t.Fatal("exhausted correlation metadata was not JSON")
		}
		assertTargetMetadata(t, record, nil)
	}
}

func TestTargetObservationPreservesAllWireBytesAndCaps(t *testing.T) {
	roots := targetRootsFixture(t, map[string]string{"workspace": "C:/PRIVATE_TARGET"})
	request := targetRequest("fs/readFile", fsTargetParams("C:/PRIVATE_TARGET/AGENTS.md"))
	project := strings.Replace(targetRequest("project/list", "{\"cursor\":\"PRIVATE_TARGET_CURSOR\",\"limit\":4294967295}"),
		"PRIVATE_TARGET_ID", "PRIVATE_TARGET_PROJECT", 1)
	payload := []byte(" \t" + request + "\r\n" + project + "\n" +
		"{\"id\":8,\"method\":\"fs/readFile\",\"params\":{\"path\":\"\\ud800\"}}\n" +
		"{\"id\":9,\"method\":\"fs/readFile\",\"params\":{\"path\":\"\xff\"}}\n" +
		strings.Repeat("PRIVATE_TARGET", maxLine/4) + "\nPRIVATE_TARGET_PARTIAL\x00\xff")
	for _, direction := range []string{"to_bridge", "from_bridge"} {
		for _, step := range []int{1, 7, 8192} {
			var forwarded, log bytes.Buffer
			o := newObserver(&log)
			o.readTargetRoots = roots
			p := &lineObserver{o: o, direction: direction}
			if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), step}, p); err != nil ||
				!bytes.Equal(forwarded.Bytes(), payload) {
				t.Fatal("target classification changed forwarded bytes")
			}
			if log.Len() > maxTrace || bytes.Contains(log.Bytes(), []byte("PRIVATE_TARGET")) ||
				len(p.buf) > maxLine || cap(p.buf) > 2*maxLine {
				t.Fatal("target observation leaked or exceeded existing bounds")
			}
			records := events(t, log.Bytes())
			if len(records) != 7 || records[4].Class != "oversized" || records[5].Class != "unterminated" {
				t.Fatal("target observation changed stream classifications")
			}
		}
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.readTargetRoots = roots
	for i := 0; i < 2000; i++ {
		raw, _ := json.Marshal(map[string]any{"id": i, "method": "fs/readFile",
			"params": map[string]string{"path": "C:/PRIVATE_TARGET/config.toml"}})
		o.line("to_bridge", raw)
	}
	if log.Len() > maxTrace || len(o.pending) > maxPending || bytes.Contains(log.Bytes(), []byte("PRIVATE_TARGET")) {
		t.Fatal("target metadata exceeded trace or pending bounds")
	}
	events(t, log.Bytes())
}

func TestTargetEnvironmentChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_TARGET_CHILD") != "1" {
		return
	}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, "MAGPIE_CI_READ_TARGET_ROOTS") {
			os.Exit(41)
		}
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		os.Exit(42)
	}
	os.Stderr.WriteString("PRIVATE_TARGET_STDERR\x00\xff\r\n")
	os.Exit(27)
}

func TestTargetRootsStrippedFromActualChildAndStreamsUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MAGPIE_OBSERVER_TARGET_CHILD", "1")
	t.Setenv(readTargetRootsEnv, "{\"workspace\":\"C:/PRIVATE_TARGET\"}")
	t.Setenv(strings.ToLower(readTargetRootsEnv), "{\"workspace\":\"C:/PRIVATE_TARGET_ALIAS\"}")
	self, err := os.Executable()
	if err != nil {
		t.Fatal("could not locate synthetic fixture executable")
	}
	payload := []byte(targetRequest("fs/readFile", fsTargetParams("C:/PRIVATE_TARGET/config.toml")) + "\r\n\x00\xff")
	var output, diagnostics captureCloser
	var log bytes.Buffer
	o := newObserver(&log)
	o.readTargetRoots = targetRootsFixture(t, map[string]string{"workspace": "C:/PRIVATE_TARGET"})
	code := runChild(self, []string{"-test.run=^TestTargetEnvironmentChildFixture$"},
		io.NopCloser(bytes.NewReader(payload)), &output, &diagnostics, o)
	if code != 27 || !bytes.Equal(output.Bytes(), payload) ||
		diagnostics.String() != "PRIVATE_TARGET_STDERR\x00\xff\r\n" || !output.closed || !diagnostics.closed {
		t.Fatal("target config leaked to child or child lifecycle/streams changed")
	}
	if bytes.Contains(log.Bytes(), []byte("PRIVATE_TARGET")) {
		t.Fatal("child target bytes entered diagnostic metadata")
	}
}
