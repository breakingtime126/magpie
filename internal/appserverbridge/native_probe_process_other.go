//go:build !windows

package appserverbridge

import "context"

// Production bootstrap/endpoint allocation remains unsupported on this OS.
// This branch serves authored portable process fixtures only; it is not a
// substitute for Windows retained-Job acceptance.
func runProductionPrimaryProbe(ctx context.Context, b Backend, directory string, args, environment []string) ([]byte, error) {
	return runStartupProbeEnvironment(ctx, b, directory, args, environment)
}
