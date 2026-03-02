package config

import _ "embed"

//go:embed schema.json
var schemaJSON []byte

// Schema returns the raw JSON schema bytes.
func Schema() []byte {
	return schemaJSON
}
