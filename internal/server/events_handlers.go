package server

import (
	"net/http"
	"strconv"

	"agenthub/internal/auth"
	"agenthub/internal/db"
	"agenthub/internal/playbook"
)

var validEventTypes = map[string]bool{
	"RESULT":   true,
	"CLAIM":    true,
	"FACT":     true,
	"FAILURE":  true,
	"HUNCH":    true,
	"VERIFY":   true,
	"OPERATOR": true,
}

func (s *Server) handlePostEvent(w http.ResponseWriter, r *http.Request) {
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
		EventType string `json:"event_type"`
		Payload   string `json:"payload"`
		Tags      string `json:"tags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if !validEventTypes[req.EventType] {
		writeError(w, http.StatusBadRequest, "invalid event_type: must be one of RESULT, CLAIM, FACT, FAILURE, HUNCH, VERIFY, OPERATOR")
		return
	}

	if req.Payload == "" {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}

	event, err := s.db.InsertEvent(agent.ID, req.EventType, req.Payload, req.Tags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	s.db.IncrementRateLimit(agent.ID, "post")

	// Run playbooks asynchronously
	go s.runPlaybooks(event)

	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	eventType := r.URL.Query().Get("type")
	agentID := r.URL.Query().Get("agent")
	tags := r.URL.Query().Get("tags")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	events, err := s.db.ListEvents(eventType, agentID, tags, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if events == nil {
		events = []db.Event{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) runPlaybooks(event *db.Event) {
	runner := playbook.NewRunner(s.db)
	actions := runner.Evaluate(event)

	for _, action := range actions {
		// Post playbook alerts to the appropriate channel
		ch, err := s.db.GetChannelByName(action.Channel)
		if err != nil || ch == nil {
			// Create channel if it doesn't exist
			s.db.CreateChannel(action.Channel, "auto-created by playbook")
			ch, _ = s.db.GetChannelByName(action.Channel)
		}
		if ch != nil {
			s.db.CreatePost(ch.ID, "_system", nil, action.Message)
		}
	}
}
