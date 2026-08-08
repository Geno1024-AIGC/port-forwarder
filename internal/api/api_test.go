package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	s := New(engine.New())
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	return ts
}

func TestRulesCRUD(t *testing.T) {
	ts := setup(t)

	body := bytes.NewBufferString(`{"name":"echo","listen":"127.0.0.1:0","target":"127.0.0.1:1"}`)
	resp, err := http.Post(ts.URL+"/api/rules", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var rule struct{ ID string }
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		t.Fatalf("decode: %v", err)
	}

	resp, err = http.Get(ts.URL + "/api/rules")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var rules []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/rules/"+rule.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/rules")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("got %d rules after delete, want 0", len(rules))
	}
}

func TestCreateRuleInvalidJSON(t *testing.T) {
	ts := setup(t)
	body := bytes.NewBufferString(`not json`)
	resp, err := http.Post(ts.URL+"/api/rules", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDeleteMissingRule(t *testing.T) {
	ts := setup(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/rules/999", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}