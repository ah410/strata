package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	_, port, _ := net.SplitHostPort(ok.Listener.Addr().String())
	if got := healthcheck(":" + port); got != 0 {
		t.Fatalf("healthy server: got %d, want 0", got)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	_, port, _ = net.SplitHostPort(bad.Listener.Addr().String())
	if got := healthcheck("127.0.0.1:" + port); got != 1 {
		t.Fatalf("unhealthy server: got %d, want 1", got)
	}

	if got := healthcheck("not-an-addr"); got != 1 {
		t.Fatalf("bad addr: got %d, want 1", got)
	}
}
