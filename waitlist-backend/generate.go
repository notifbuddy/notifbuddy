// Package waitlistbackend anchors `go generate` for the waitlist service.
//
// The OpenAPI spec (../spec/waitlist-openapi.yaml) is the single source of
// truth: ogen generates the Go server stub here, and openapi-typescript
// generates the landing page's client types from the same file.
package waitlistbackend

//go:generate go tool ogen --target internal/api --package api --clean ../spec/waitlist-openapi.yaml
