// Package backend holds repository-level code-generation directives.
//
// Running `go generate ./...` from the backend/ directory regenerates the
// entire HTTP transport layer (server interface, request/response types,
// router, and validation) from the OpenAPI spec. None of the generated code in
// internal/api is hand-edited. The paths below are relative to this file's
// directory (backend/).
package backend

//go:generate go tool ogen --config ogen.yml --target internal/api --package api --clean ../spec/openapi.yaml
