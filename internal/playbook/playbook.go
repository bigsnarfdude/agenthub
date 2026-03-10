package playbook

import (
	"encoding/json"
	"fmt"
	"math"

	"agenthub/internal/db"
)

// Action is what a playbook rule produces — a message to post to a channel.
type Action struct {
	Channel string
	Message string
}

// Runner evaluates playbook rules against incoming events.
type Runner struct {
	db *db.DB
}

func NewRunner(database *db.DB) *Runner {
	return &Runner{db: database}
}

// Evaluate runs all playbook rules against an event and returns any triggered actions.
func (r *Runner) Evaluate(event *db.Event) []Action {
	var actions []Action

	if a := r.deadEndDetector(event); a != nil {
		actions = append(actions, *a)
	}
	if a := r.convergenceSignal(event); a != nil {
		actions = append(actions, *a)
	}

	return actions
}

// deadEndDetector: 3+ failures on same experiment from different agents → alert.
func (r *Runner) deadEndDetector(event *db.Event) *Action {
	if event.EventType != "FAILURE" {
		return nil
	}

	// Extract experiment from payload JSON
	experiment := extractField(event.Payload, "experiment")
	if experiment == "" {
		return nil
	}

	count, err := r.db.CountRecentFailures(experiment)
	if err != nil || count < 3 {
		return nil
	}

	return &Action{
		Channel: "alerts",
		Message: fmt.Sprintf("[dead-end-detector] %d agents have failed on experiment %q in the last hour. Consider a different approach.", count, experiment),
	}
}

// convergenceSignal: 3+ recent results within 5% of each other → converging.
func (r *Runner) convergenceSignal(event *db.Event) *Action {
	if event.EventType != "RESULT" {
		return nil
	}

	experiment := extractField(event.Payload, "experiment")
	if experiment == "" {
		return nil
	}

	scores, err := r.db.RecentResultScores(experiment, 10)
	if err != nil || len(scores) < 3 {
		return nil
	}

	// Check if the top 3 scores are within 5% of each other
	top := scores[:3]
	maxScore := top[0]
	if maxScore == 0 {
		return nil
	}

	for _, s := range top[1:] {
		if math.Abs(maxScore-s)/math.Abs(maxScore) > 0.05 {
			return nil
		}
	}

	return &Action{
		Channel: "status",
		Message: fmt.Sprintf("[convergence-signal] Top 3 scores for experiment %q are within 5%%: %.4f, %.4f, %.4f", experiment, top[0], top[1], top[2]),
	}
}

// extractField pulls a string field from a JSON payload.
func extractField(payload, field string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return ""
	}
	v, ok := m[field]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
