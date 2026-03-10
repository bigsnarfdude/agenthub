package server

import (
	"net/http"
	"strconv"

	"agenthub/internal/auth"
	"agenthub/internal/db"
)

var validMemoryKinds = map[string]bool{
	"fact":    true,
	"failure": true,
	"hunch":   true,
}

func (s *Server) handlePostMemory(w http.ResponseWriter, r *http.Request) {
	agent := auth.AgentFromContext(r.Context())

	// Rate limit
	allowed, err := s.db.CheckRateLimit(agent.ID, "post", s.config.MaxPostsPerHour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rate limit check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "post rate limit exceeded")
		return
	}

	var req struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
		Tags    string `json:"tags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !validMemoryKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "kind must be fact, failure, or hunch")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	mem, err := s.db.InsertMemory(agent.ID, req.Kind, req.Content, req.Tags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert memory")
		return
	}

	s.db.IncrementRateLimit(agent.ID, "post")

	writeJSON(w, http.StatusCreated, mem)
}

func (s *Server) handleListMemory(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	agentID := r.URL.Query().Get("agent")
	tags := r.URL.Query().Get("tags")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	memories, err := s.db.ListMemory(kind, agentID, tags, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if memories == nil {
		memories = []db.Memory{}
	}
	writeJSON(w, http.StatusOK, memories)
}
