package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sort"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// dispatch routes a captured request to the fake for its Host. Add a provider
// by registering one more host in newDispatch — no cert or proxy changes.
type dispatch struct {
	byHost map[string]http.Handler
}

func newDispatch() *dispatch {
	d := &dispatch{byHost: map[string]http.Handler{}}
	d.byHost["api.workos.com"] = workosFake()
	// Scaffolds — wired to loudly 501 until a real flow needs them, so the
	// first Linear/GitHub call this suite makes is impossible to miss.
	d.byHost["api.linear.app"] = notImplemented("linear")
	d.byHost["api.github.com"] = notImplemented("github")
	return d
}

func (d *dispatch) hosts() []string {
	out := make([]string, 0, len(d.byHost))
	for h := range d.byHost {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func (d *dispatch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := hostname(r.Host)
	log.Printf("fakeapis: capture %s %s%s", r.Method, host, r.URL.Path)
	h, ok := d.byHost[host]
	if !ok {
		log.Printf("fakeapis: no fake registered for host %q", host)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "fakeapis: unhandled host " + host})
		return
	}
	h.ServeHTTP(w, r)
}

// workosFake serves the subset of the WorkOS API the backend touches in the
// tested flows. Today that is only the organization-membership list that /me
// fans out to; extend the mux as more flows are covered.
func workosFake() http.Handler {
	mux := http.NewServeMux()

	// GET /user_management/organization_memberships — /me lists the caller's
	// orgs. Empty is a valid answer (a fresh user in no WorkOS org); it also
	// keeps the response instant instead of the SDK retrying a dead host.
	mux.HandleFunc("GET /user_management/organization_memberships", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":          []any{},
			"list_metadata": map[string]any{"before": nil, "after": nil},
		})
	})

	// Anything else WorkOS-shaped that a flow hits is a gap in the fake — make
	// it visible rather than silently wrong.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: unhandled workos path %s %s", r.Method, r.URL.Path)
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "fakeapis: workos path not faked: " + r.URL.Path})
	})
	return mux
}

// notImplemented is a placeholder host handler for providers not yet faked.
func notImplemented(provider string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: %s not implemented: %s %s", provider, r.Method, r.URL.Path)
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "fakeapis: " + provider + " fake not implemented"})
	})
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
