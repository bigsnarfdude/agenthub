package server

import (
	"net/http"
	"strconv"

	"agenthub/internal/auth"
	"agenthub/internal/db"
)

func (s *Server) handlePostResult(w http.ResponseWriter, r *http.Request) {
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
		Experiment   string  `json:"experiment"`
		Metric       string  `json:"metric"`
		Score        float64 `json:"score"`
		Platform     string  `json:"platform"`
		CodeSnapshot string  `json:"code_snapshot"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Experiment == "" {
		writeError(w, http.StatusBadRequest, "experiment is required")
		return
	}

	result, err := s.db.InsertResult(agent.ID, req.Experiment, req.Metric, req.Score, req.Platform, req.CodeSnapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert result")
		return
	}

	s.db.IncrementRateLimit(agent.ID, "post")

	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListResults(w http.ResponseWriter, r *http.Request) {
	experiment := r.URL.Query().Get("experiment")
	agentID := r.URL.Query().Get("agent")
	platform := r.URL.Query().Get("platform")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	results, err := s.db.ListResults(experiment, agentID, platform, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if results == nil {
		results = []db.Result{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	experiment := r.URL.Query().Get("experiment")
	platform := r.URL.Query().Get("platform")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	results, err := s.db.Leaderboard(experiment, platform, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if results == nil {
		results = []db.Result{}
	}
	writeJSON(w, http.StatusOK, results)
}
