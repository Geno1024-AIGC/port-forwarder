package api

import (
	"encoding/json"
	"net/http"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	sshx "github.com/Geno1024-AIGC/port-forwarder/internal/ssh"
)

// Server exposes the engine and its credential store over an HTTP/JSON API.
type Server struct {
	eng   *engine.Engine
	creds *sshx.Manager
	store *sshx.CredStore
}

// New creates an API server backed by eng and the SSH manager.
func New(eng *engine.Engine, mgr *sshx.Manager) *Server {
	return &Server{eng: eng, creds: mgr, store: mgr.Store()}
}

// Handler returns the HTTP handler that serves the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rules", s.listRules)
	mux.HandleFunc("POST /api/rules", s.createRule)
	mux.HandleFunc("DELETE /api/rules/{id}", s.deleteRule)
	mux.HandleFunc("POST /api/rules/{id}/restart", s.restartRule)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/credentials", s.listCredentials)
	mux.HandleFunc("POST /api/credentials", s.createCredential)
	mux.HandleFunc("POST /api/credentials/{id}/probe", s.probeCredential)
	mux.HandleFunc("DELETE /api/credentials/{id}", s.deleteCredential)
	return mux
}

type ruleRequest struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Listen     string `json:"listen"`
	Target     string `json:"target"`
	Credential string `json:"credential"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.eng.List())
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Type == "remote" && req.Credential == "" {
		writeError(w, http.StatusBadRequest, "remote rules need a credential")
		return
	}
	rule, err := s.eng.Add(engine.RuleType(req.Type), req.Name, req.Listen, req.Target, req.Credential)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.eng.Remove(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restartRule(w http.ResponseWriter, r *http.Request) {
	// Restart the whole engine for now; fine-grained restart later.
	s.eng.Restart()
	writeJSON(w, http.StatusOK, s.eng.List())
}

// --- credentials -----------------------------------------------------------

func (s *Server) listCredentials(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	var c sshx.Credential
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	got, err := s.store.Add(&c)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, got)
}

func (s *Server) probeCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.creds.Probe(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Stop rules using this credential, then remove the credential itself.
	for _, rule := range s.eng.List() {
		if rule.Type == engine.RuleTypeRemote && rule.Credential == id {
			_ = s.eng.Remove(rule.ID)
		}
	}
	if err := s.creds.RemoveCredential(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
