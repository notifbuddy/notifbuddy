package main

// Code generation entrypoint. Running `go generate ./...` regenerates the
// entire HTTP transport layer (server interface, request/response types,
// router, and validation) from the OpenAPI spec. None of the generated code
// in internal/api is hand-edited.
//
//go:generate go tool ogen --target internal/api --package api --clean ../spec/openapi.yaml
