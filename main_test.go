package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalOnly(t *testing.T) {
	const addr = "127.0.0.1:1234"
	h := localOnly(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := []struct {
		method, host, origin string
		want                 int
	}{
		{"GET", addr, "", 200},
		{"POST", addr, "http://" + addr, 200},
		{"POST", addr, "", 200},                      // non-browser client
		{"GET", "evil.example:1234", "", 403},        // DNS rebinding
		{"POST", addr, "https://evil.example", 403},  // foreign page
		{"POST", addr, "http://localhost:1234", 403}, // not the address the app opened
		{"GET", addr, "https://evil.example", 200},   // reads are guarded by the browser's same-origin policy
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "/api/reset", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s host=%s origin=%q: got %d, want %d", c.method, c.host, c.origin, rec.Code, c.want)
		}
	}
}
