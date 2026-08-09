package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	sshx "github.com/Geno1024-AIGC/port-forwarder/internal/ssh"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := sshx.NewCredStore(t.TempDir() + "/creds.json")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	mgr := sshx.NewManager(context.Background(), store)
	s := New(engine.New(), mgr)
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

func TestCredentialsCRUD(t *testing.T) {
	ts := setup(t)

	body := bytes.NewBufferString(`{"name":"vps","host":"vps.example.com:22","user":"root","authType":"password","password":"hunter2"}`)
	resp, err := http.Post(ts.URL+"/api/credentials", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var got sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Fatal("created credential has no ID")
	}
	if got.AuthType != "password" || got.Password != "hunter2" {
		t.Fatalf("credential roundtrip mismatch: %+v", got)
	}

	resp, err = http.Get(ts.URL + "/api/credentials")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	var list []sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d credentials, want 1", len(list))
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/credentials/"+got.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/credentials")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("got %d credentials after delete, want 0", len(list))
	}
}

func TestRemoteRuleNeedsCredential(t *testing.T) {
	ts := setup(t)
	body := bytes.NewBufferString(`{"name":"web","listen":":8080","target":"127.0.0.1:3000","type":"remote"}`)
	resp, err := http.Post(ts.URL+"/api/rules", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestProbeMissingCredential(t *testing.T) {
	ts := setup(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/credentials/nope/probe", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUpdateCredential(t *testing.T) {
	ts := setup(t)

	body := bytes.NewBufferString(`{"name":"vps","host":"vps.example.com:22","user":"root","authType":"password","password":"hunter2"}`)
	resp, err := http.Post(ts.URL+"/api/credentials", "application/json", body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	// Rename only; blank password should keep the stored value.
	body = bytes.NewBufferString(`{"name":"renamed","host":"other:22","user":"root","authType":"password"}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/credentials/"+created.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", resp.StatusCode)
	}
	var updated sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.Name != "renamed" || updated.Host != "other:22" || updated.Password != "hunter2" {
		t.Fatalf("updated credential mismatch: %+v", updated)
	}
}

func TestCredentialKeyUpload(t *testing.T) {
	ts := setup(t)

	body := bytes.NewBufferString(`{"name":"vps","host":"vps.example.com:22","user":"root","authType":"key","keyContent":[49,50,51]}`)
	resp, err := http.Post(ts.URL+"/api/credentials", "application/json", body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.KeyPath == "" {
		t.Fatal("expected keyPath set after upload")
	}
	data, err := os.ReadFile(created.KeyPath)
	if err != nil {
		t.Fatalf("read saved key: %v", err)
	}
	if string(data) != "123" {
		t.Fatalf("saved key content = %q, want 123", data)
	}

	resp, err = http.Get(ts.URL + "/api/credentials")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	var list []sshx.Credential
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) == 0 || list[0].Password != "" {
		t.Fatalf("password should be masked in list, got %+v", list)
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

func TestUpdateRule(t *testing.T) {
	ts := setup(t)

	body := bytes.NewBufferString(`{"name":"echo","listen":"127.0.0.1:0","target":"127.0.0.1:1"}`)
	resp, err := http.Post(ts.URL+"/api/rules", "application/json", body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created struct{ ID string }
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	body = bytes.NewBufferString(`{"name":"renamed","listen":"127.0.0.1:0","target":"127.0.0.1:9"}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/rules/"+created.ID, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", resp.StatusCode)
	}
	var updated map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated["name"] != "renamed" || updated["target"] != "127.0.0.1:9" {
		t.Fatalf("updated rule mismatch: %+v", updated)
	}
}
