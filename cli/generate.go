// Package cli holds repository-level code-generation directives for the
// notifbuddy CLI. Running `go generate ./...` from cli/ regenerates the typed
// API client from the shared OpenAPI spec. Nothing in internal/api is
// hand-edited.
package cli

//go:generate go tool ogen --config ogen.yml --target internal/api --package api --clean ../spec/openapi.yaml
