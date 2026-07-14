package main

import (
	"log"
	"net"
	"net/http"
	"sort"

	"xolo/backend/e2e/fakeapis/github"
	"xolo/backend/e2e/fakeapis/linear"
	"xolo/backend/e2e/fakeapis/respond"
	"xolo/backend/e2e/fakeapis/workos"
)

// dispatch routes a captured request to the fake for its Host. Add a provider
// by dropping a package under fakeapis/<host> that exports Host + Handler and
// registering it here — no cert or proxy changes.
type dispatch struct {
	byHost map[string]http.Handler
}

func newDispatch() *dispatch {
	d := &dispatch{byHost: map[string]http.Handler{}}
	d.byHost[workos.Host] = workos.Handler()
	// Scaffolds — loud 501 until a real flow needs them.
	d.byHost[linear.Host] = linear.Handler()
	d.byHost[github.Host] = github.Handler()
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
		respond.JSON(w, http.StatusBadGateway, map[string]string{"error": "fakeapis: unhandled host " + host})
		return
	}
	h.ServeHTTP(w, r)
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
