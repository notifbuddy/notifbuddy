package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
)

// serveConn reads HTTP requests off an already-established (MITM-decrypted)
// connection and answers each from the dispatch mux, honoring keep-alive. It
// returns when the peer closes the connection or sends something unparseable.
func serveConn(conn net.Conn, mux *dispatch) {
	br := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			if err != io.EOF {
				// Normal at connection teardown; only note the odd ones.
				log.Printf("fakeapis: read request: %v", err)
			}
			return
		}
		// The request came over a tunnel to a specific host; make it look like a
		// normal server-side request for the handlers.
		req.URL.Scheme = "https"
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		// Drain any body the handler didn't read so the next ReadRequest aligns.
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()

		resp := rec.Result()
		resp.Request = req
		// Force a determinate framing so the client reads exactly one response.
		resp.Header.Set("Content-Length", itoa(rec.Body.Len()))
		resp.ContentLength = int64(rec.Body.Len())
		if err := resp.Write(conn); err != nil {
			return
		}
		if req.Close {
			return
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
