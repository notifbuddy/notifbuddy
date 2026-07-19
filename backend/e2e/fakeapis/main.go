// Command fakeapis is the e2e egress interceptor: a single TLS-terminating
// forward proxy that captures every outbound call the backend's SDKs make to
// third-party APIs (Linear and GitHub as they're wired) and serves them from
// in-process fakes. It also serves the authd (Better Auth) fake directly over
// plain HTTP — authd is first-party, so the backend reaches it via
// auth.base_url rather than through the proxy.
//
// Why a proxy and not an SDK base-URL override: the production code keeps
// calling the real hostnames over the real SDKs — nothing test-specific leaks
// into it. The backend container just gets HTTPS_PROXY pointed here plus our
// CA in its trust store (SSL_CERT_FILE), so the Go SDKs' default transport
// (ProxyFromEnvironment) tunnels through us. We MITM the CONNECT with a leaf
// cert minted on the fly from our own CA, then dispatch by Host to a fake.
//
// Expand it by adding a host handler in dispatch.go — no new certs, DNS, or
// app changes.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"xolo/backend/e2e/fakeapis/authd"
	"xolo/backend/e2e/fakeapis/session"
)

func main() {
	addr := envOr("FAKEAPIS_ADDR", ":8888")
	caOut := envOr("FAKEAPIS_CA_OUT", "/certs/ca.pem")

	ca, err := newCA()
	if err != nil {
		log.Fatalf("fakeapis: build CA: %v", err)
	}
	// Publish the CA cert (PEM) for the backend to trust before we accept any
	// traffic, so a healthcheck on this server doubles as "CA is ready".
	if err := ca.writeCertPEM(caOut); err != nil {
		log.Fatalf("fakeapis: write CA to %s: %v", caOut, err)
	}
	log.Printf("fakeapis: CA written to %s", caOut)

	// The shared e2e secret signs session tokens; the authd fake verifies them.
	secret := os.Getenv("E2E_SESSION_SECRET")
	if secret == "" {
		log.Fatal("fakeapis: E2E_SESSION_SECRET is required")
	}

	// Mint the shared signed-in session and publish it onto the same volume, so
	// the Playwright UI suite can authenticate its browser without a live
	// sign-in.
	sessOut := envOr("FAKEAPIS_SESSION_OUT", "/certs/session.json")
	if err := session.Write(sessOut, secret); err != nil {
		log.Fatalf("fakeapis: write session to %s: %v", sessOut, err)
	}
	log.Printf("fakeapis: session written to %s", sessOut)

	mux := newDispatch()
	authdFake := authd.Handler(secret)

	srv := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxy(ca, mux, authdFake, w, r) }),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down cleanly on the compose stop signal so teardown isn't logged as a
	// crash.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("fakeapis: proxy listening on %s (intercepting %v)", addr, mux.hosts())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("fakeapis: serve: %v", err)
	}
}

// proxy is the top-level proxy handler. CONNECT starts a MITM TLS tunnel;
// /api/auth/* is the first-party authd fake (reached directly, not proxied);
// anything else is either the healthcheck or a plain-HTTP proxied request.
func proxy(ca *ca, mux *dispatch, authdFake http.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		handleConnect(ca, mux, w, r)
		return
	}
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/auth/") {
		authdFake.ServeHTTP(w, r)
		return
	}
	// Plain-HTTP proxied request (target carried in the absolute URL / Host).
	mux.ServeHTTP(w, r)
}

// handleConnect terminates the client's TLS to the requested host with a leaf
// cert from our CA, then serves the decrypted request stream via the dispatch
// mux — so upstream fakes see ordinary *http.Request values.
func handleConnect(ca *ca, mux *dispatch, w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hij.Hijack()
	if err != nil {
		log.Printf("fakeapis: hijack: %v", err)
		return
	}
	defer clientConn.Close()
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	tlsConn := tls.Server(clientConn, &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = host
			}
			return ca.leafFor(name)
		},
	})
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("fakeapis: TLS handshake with %s: %v", host, err)
		return
	}
	serveConn(tlsConn, mux)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
