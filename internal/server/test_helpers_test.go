package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skip HTTP server test: loopback listen unavailable: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()
	return srv
}
