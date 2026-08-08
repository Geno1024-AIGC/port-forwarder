package webui

import (
	"net/http/httptest"
	"testing"
)

func TestNoCache(t *testing.T) {
	s := httptest.NewServer(Handler())
	defer s.Close()
	r, err := s.Client().Get(s.URL + "/app.js")
	if err != nil { t.Fatal(err) }
	defer r.Body.Close()
	if r.StatusCode != 200 { t.Fatalf("status = %d", r.StatusCode) }
	if got := r.Header.Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
