package api

import (
	"encoding/json"
	"net/http"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
)

// Server exposes the engine over an HTTP/JSON API.
type Server struct {
	eng *engine.Engine
}

// New creates an API server backed by eng.
func New(eng *engine.Engine) *Server {
	return &Server{eng: eng}
}

// Handler returns the HTTP handler that serves the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rules", s.listRules)
	mux.HandleFunc("POST /api/rules", s.createRule)
	mux.HandleFunc("DELETE /api/rules/{id}", s.deleteRule)
	mux.HandleFunc("POST /api/rules/{id}/restart", s.restartRule)
	mux.HandleFunc("GET /api/health", s.health)
	return mux
}

type ruleRequest struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Listen string `json:"listen"`
	Target string `json:"target"`
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
	rule, err := s.eng.Add(engine.RuleType(req.Type), req.Name, req.Listen, req.Target)
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
