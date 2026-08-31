package api

import "embed"

// Files contains the public API contract.
//
//go:embed openapi.yaml
var Files embed.FS
