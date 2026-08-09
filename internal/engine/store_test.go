package engine

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(filepath.Join(dir, "rules.json"))

	if err := st.Save([]*Rule{
		{ID: "3", Type: RuleTypeLocal, Name: "web", Listen: "127.0.0.1:9000", Target: "127.0.0.1:8080", Status: StatusRunning},
		{ID: "7", Type: RuleTypeRemote, Name: "api", Listen: "2201", Target: "127.0.0.1:444", Credential: "credp-1", Status: StatusPending},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := NewStore(filepath.Join(dir, "rules.json")).List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rules, want 2", len(got))
	}
	if got[0].ID != "3" || got[0].Status != "" {
		t.Errorf("local rule id/status changed: %+v", got[0])
	}
	if got[1].Credential != "credp-1" {
		t.Errorf("remote credential lost: %+v", got[1])
	}
}

func TestStoreMissingFile(t *testing.T) {
	got, err := NewStore(filepath.Join(t.TempDir(), "nope", "rules.json")).List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d rules, want none", len(got))
	}
}

func TestRestoreRemoteRulesKeepsID(t *testing.T) {
	var added Rule
	eng := New()
	eng.SetRemoteBackend(fakeBackend{add: func(r Rule) { added = r }})

	eng.Restore([]*Rule{
		{ID: "42", Type: RuleTypeRemote, Name: "api", Listen: "2201", Target: "127.0.0.1:444", Credential: "credp-1"},
	})

	if added.ID != "42" {
		t.Errorf("added id = %q, want 42", added.ID)
	}
	if added.Credential != "credp-1" {
		t.Errorf("added credential = %q, want credp-1", added.Credential)
	}
	got := eng.List()
	if len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("restored rule missing: %+v", got)
	}
	n := eng.Add
	if _, err := n(RuleTypeRemote, "x", "2202", "127.0.0.1:1", ""); err != nil {
		t.Fatalf("add: %v", err)
	}
	if r := eng.Get("43"); r == nil {
		t.Error("next auto id did not advance past restored id")
	}
	if len(eng.List()) != 2 {
		t.Error("new rule not in list after restore")
	}
}

type fakeBackend struct {
	add func(Rule)
}

func (f fakeBackend) AddRemote(r Rule) {
	if f.add != nil {
		f.add(r)
	}
}

func (f fakeBackend) RemoveRemote(id string) {}