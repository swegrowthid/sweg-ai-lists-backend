package api

import _ "embed"

//go:embed openapi.yaml
var specYAML []byte

// Spec returns the raw OpenAPI document bytes.
// The YAML file stays the single source of truth.
func Spec() []byte { return specYAML }
