package nativeendpoint

import (
	"context"
	"fmt"
	"github.com/yetone/magpie/internal/proc"
	"io"
	"os"
	"testing"
	"time"
)

func TestProbeExitStatusFixture(t *testing.T) {
	if len(os.Args) != 4 || os.Args[3] != "synthetic-valid-output-nonzero" {
		return
	}
	fmt.Print(`{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true}}`)
	os.Exit(7)
}
func TestWindowsProbeExitStatusIsSeparateFromDrain(t *testing.T) {
	endpoint, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Shutdown(context.Background())
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := proc.Command(self, "-test.run=^TestProbeExitStatusFixture$", "-test.timeout=15s", "synthetic-valid-output-nonzero")
	command.Stdout = writer
	if err := endpoint.Start(command); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil || len(output) == 0 {
		t.Fatal("missing authored output", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := endpoint.WaitForExit(ctx); err == nil {
		t.Fatal("nonzero native exit accepted as successful probe")
	}
	if err := endpoint.Shutdown(ctx); err != nil {
		t.Fatal("nonzero process did not drain", err)
	}
}
