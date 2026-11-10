package nativeconnect

import (
	"crypto/sha256"
	"flag"
	"testing"
)

var expectedPackageDigest = flag.String("native-package-sha256", "", "packager-only expected adapter digest")
var expectedPackageRevision = flag.String("native-package-revision", "", "packager-only expected source revision")

// The Windows packager uses the same linker arguments for this test and the
// original GUI. Go intentionally omits -ldflags from trimmed build information;
// this verifies injection without removing -trimpath or trusting a manifest.
// The running GUI independently verifies its sibling bytes at every use.
func TestPackagedLauncherLinkerPins(t *testing.T) {
	if *expectedPackageDigest == "" && *expectedPackageRevision == "" {
		t.Skip("only the packager supplies linker-pin expectations")
	}
	if !canonicalHex(*expectedPackageDigest, sha256.Size) || !canonicalHex(*expectedPackageRevision, 20) {
		t.Fatal("invalid package identity expectation")
	}
	if packagedLauncherSHA256 != *expectedPackageDigest || packagedLauncherSourceRevision != *expectedPackageRevision {
		t.Fatal("compiled launcher pins do not match the built adapter and source")
	}
}
