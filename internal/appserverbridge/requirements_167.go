package appserverbridge

import "bytes"

// ConfigRequirementsCompareNull167 selects only the reviewed native null view.
const ConfigRequirementsCompareNull167 = "compare-null-v0.167.0-alpha.3.2"

// The source oracle is openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9:
// protocol/common.rs, protocol/v2/config.rs and config_processor.rs. A native
// null requires absent requirements AND unrestricted allowed login methods;
// empty homes alone do not establish it. Never adapt a non-null policy.
func validateConfigRequirementsNull167(data []byte) error {
	if validateRequirementsJSON(data) != nil {
		return errConfigRequirements
	}
	root, ok := requirementsObject(data, "requirements", "supportsIndependentSpeedModes")
	if !ok || !requirementsNull(root["requirements"]) ||
		!bytes.Equal(bytes.TrimSpace(root["supportsIndependentSpeedModes"]), []byte("true")) {
		return errConfigRequirements
	}
	return nil
}

// Keep the existing explicit empty-object client adapter, but send only the
// native no-argument params:null call. The only additional native envelope
// field is bounded trace metadata; it is accepted but never sent to children.
// No selector, override or unknown envelope field may enter this operation.
func validateRequirementsRequest167(line []byte) error {
	if validateRequirementsJSON(line) != nil {
		return errConfigRequirements
	}
	m, err := parseObject(line)
	if err != nil || len(m) < 2 || len(m) > 4 {
		return errConfigRequirements
	}
	if !requirementsEnum(m["method"], "configRequirements/read") {
		return errConfigRequirements
	}
	if _, err := authCompatibilityID(m["id"]); err != nil {
		return errConfigRequirements
	}
	for key := range m {
		if key != "id" && key != "method" && key != "params" && key != "trace" {
			return errConfigRequirements
		}
	}
	if trace, present := m["trace"]; present && !NativeTrace167Valid(trace) {
		return errConfigRequirements
	}
	if params, present := m["params"]; present && !requirementsNull(params) {
		if _, ok := requirementsObject(params); !ok {
			return errConfigRequirements
		}
	}
	return nil
}
