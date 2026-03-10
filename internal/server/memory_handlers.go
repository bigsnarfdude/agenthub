package server

import (
	"net/http"
	"strconv"

	"agenthub/internal/auth"
	"agenthub/internal/db"
)

func (s *Server) handlePostMemory(w http.ResponseWriter, r *http.Request) {
	agent := auth.AgentFromContext(r.Context())

	var req struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
		Tags    string `json:"tags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Kind != "fact" && req.Kind != "failure" && req.Kind != "hunch" {
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
