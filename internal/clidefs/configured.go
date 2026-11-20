package clidefs

import "os"

// Aviary is the built-in definition that runs a turn on an Aviary agent
// in-process: aviary/<agent> runs a turn on <agent> through core's agent
// runner.
const Aviary = "aviary"

// FromConfig builds the CLI registry from the models: section's
// cli_definitions. `aviary doctor`, the providers and Projects dispatch all
// load it here, so a binary override in a definition is the executable the
// doctor probes and the one jobs run. The aviary definition points at the
// running executable unless cli_definitions names another.
func FromConfig(definitions map[string]any) (*Registry, error) {
	return Load(withAviaryExecutable(definitions))
}

// withAviaryExecutable sets the aviary definition's binary to the running
// executable when definitions do not set one.
func withAviaryExecutable(definitions map[string]any) map[string]any {
	override, _ := definitions[Aviary].(map[string]any)
	if override["binary"] != nil {
		return definitions
	}
	executable, err := os.Executable()
	if err != nil {
		return definitions
	}
	merged := make(map[string]any, len(definitions)+1)
	for name, definition := range definitions {
		merged[name] = definition
	}
	aviary := map[string]any{"binary": executable}
	for field, value := range override {
		if field != "binary" {
			aviary[field] = value
		}
	}
	merged[Aviary] = aviary
	return merged
}
