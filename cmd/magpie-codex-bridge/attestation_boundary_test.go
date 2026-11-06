package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

func TestCheckDescribesAttestationBoundaryWithoutStartingChildren(t *testing.T) {
	for _, mode := range []string{"", appserverbridge.AttestationUnavailable} {
		t.Run(mode, func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			loads := 0
			code := execute(context.Background(), []string{"--check"}, nil, &output, &diagnostics, dependencies{
				getenv: func(string) string { return filepath.Join(t.TempDir(), "bridge.json") },
				load: func(string) (appserverbridge.Config, error) {
					loads++
					// Real file validation is covered by TestCheckLoadsRealConfig.
					// Nil run/probe dependencies make any attempted child launch fail.
					return appserverbridge.Config{Attestation: mode}, nil
				},
			})
			if code != 0 || loads != 1 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "no children started") {
				t.Fatalf("code=%d loads=%d stdout=%q stderr=%q", code, loads, output.String(), diagnostics.String())
			}
			warning := "attestation mode returns RPC failure, not Windows Desktop error tokens"
			if strings.Contains(diagnostics.String(), warning) != (mode != "") {
				t.Fatal("configuration check concealed or invented an attestation mode")
			}
		})
	}
}
