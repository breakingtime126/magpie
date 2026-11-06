package appserverbridge

import "testing"

// Copy this file alone onto b047d781d1c26a2ee2b65a6c64cf2a9b94a5c9dc.
// It uses the existing validator and fails by assertion on native defaults.
func TestControlPlane167OptionalParamsBehavioralRed(t *testing.T) {
	for _, params := range []string{
		`{}`, `{"includeLayers":false}`, `{"cwd":null}`, `{"includeLayers":false,"cwd":null}`,
	} {
		t.Run(params, func(t *testing.T) {
			line := `{"id":1,"method":"config/read","params":` + params + `}`
			if !validateControlPlaneRead167([]byte(line)) {
				t.Fatal("native defaulted config/read params must be accepted")
			}
		})
	}
}
