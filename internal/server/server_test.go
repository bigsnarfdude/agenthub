package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"agenthub/internal/db"
	"agenthub/internal/gitrepo"
)

// testServer creates a fresh server with a temp database for each test.
func testServer(t *testing.T) (*Server, *db.DB, func()) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repoPath := filepath.Join(dir, "repo.git")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repo, err := gitrepo.Init(repoPath)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	srv := New(database, repo, "test-admin-key", Config{
		MaxBundleSize:    50 * 1024 * 1024,
		MaxPushesPerHour: 100,
		MaxPostsPerHour:  100,
		ListenAddr:       ":0",
	})

	cleanup := func() { database.Close() }
	return srv, database, cleanup
}

// createTestAgent creates an agent directly in the DB, returns its API key.
func createTestAgent(t *testing.T, database *db.DB, id string) string {
	t.Helper()
	key := "testkey-" + id
	if err := database.CreateAgent(id, key); err != nil {
		t.Fatalf("create agent %s: %v", id, err)
	}
	return key
}

// doRequest executes a request against the server mux and returns the response.
func doRequest(srv *Server, method, path string, body any, apiKey string) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	return w
}

// --- Health ---

func TestHealth(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "GET", "/api/health", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// --- Registration ---

func TestRegister(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "POST", "/api/register", map[string]string{"id": "test-agent"}, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "test-agent" {
		t.Errorf("id = %q, want test-agent", resp["id"])
	}
	if resp["api_key"] == "" {
		t.Error("api_key should not be empty")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	doRequest(srv, "POST", "/api/register", map[string]string{"id": "test-agent"}, "")
	w := doRequest(srv, "POST", "/api/register", map[string]string{"id": "test-agent"}, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestRegisterInvalidID(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "POST", "/api/register", map[string]string{"id": "bad agent!"}, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- Admin agent creation ---

func TestAdminCreateAgent(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "POST", "/api/admin/agents", map[string]string{"id": "admin-agent"}, "test-admin-key")
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminCreateAgentNoKey(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "POST", "/api/admin/agents", map[string]string{"id": "admin-agent"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminCreateAgentWrongKey(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "POST", "/api/admin/agents", map[string]string{"id": "admin-agent"}, "wrong-key")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminCreateAgentDuplicate(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	doRequest(srv, "POST", "/api/admin/agents", map[string]string{"id": "dup-agent"}, "test-admin-key")
	w := doRequest(srv, "POST", "/api/admin/agents", map[string]string{"id": "dup-agent"}, "test-admin-key")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// --- Channels ---

func TestCreateChannel(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "POST", "/api/channels", map[string]string{
		"name": "results", "description": "experiment results",
	}, key)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ch db.Channel
	json.Unmarshal(w.Body.Bytes(), &ch)
	if ch.Name != "results" {
		t.Errorf("name = %q, want results", ch.Name)
	}
}

func TestCreateChannelDuplicate(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "results"}, key)
	w := doRequest(srv, "POST", "/api/channels", map[string]string{"name": "results"}, key)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateChannelInvalidName(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "POST", "/api/channels", map[string]string{"name": "Bad Name!"}, key)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChannels(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "alpha"}, key)
	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "beta"}, key)

	w := doRequest(srv, "GET", "/api/channels", nil, key)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var channels []db.Channel
	json.Unmarshal(w.Body.Bytes(), &channels)
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
}

func TestListChannelsEmpty(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/channels", nil, key)
	if w.Body.String() == "null\n" {
		t.Error("empty channels should return [], not null")
	}
}

// --- Posts ---

func TestCreatePost(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "general"}, key)

	w := doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{
		"content": "hello world",
	}, key)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var post db.Post
	json.Unmarshal(w.Body.Bytes(), &post)
	if post.Content != "hello world" {
		t.Errorf("content = %q, want hello world", post.Content)
	}
	if post.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want agent-1", post.AgentID)
	}
}

func TestCreatePostEmptyContent(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "general"}, key)

	w := doRequest(srv, "POST", "/api/channels/general/posts", map[string]string{
		"content": "",
	}, key)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreatePostNoChannel(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "POST", "/api/channels/nonexistent/posts", map[string]string{
		"content": "hello",
	}, key)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListPosts(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "general"}, key)
	doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{"content": "post 1"}, key)
	doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{"content": "post 2"}, key)

	w := doRequest(srv, "GET", "/api/channels/general/posts", nil, key)
	var posts []db.Post
	json.Unmarshal(w.Body.Bytes(), &posts)
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
}

// --- Replies ---

func TestReply(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "general"}, key)
	doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{"content": "parent post"}, key)

	// Reply to post 1
	w := doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{
		"content": "reply", "parent_id": 1,
	}, key)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get replies
	w = doRequest(srv, "GET", "/api/posts/1/replies", nil, key)
	var replies []db.Post
	json.Unmarshal(w.Body.Bytes(), &replies)
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
	if replies[0].Content != "reply" {
		t.Errorf("reply content = %q, want reply", replies[0].Content)
	}
}

func TestGetPost(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	doRequest(srv, "POST", "/api/channels", map[string]string{"name": "general"}, key)
	doRequest(srv, "POST", "/api/channels/general/posts", map[string]any{"content": "find me"}, key)

	w := doRequest(srv, "GET", "/api/posts/1", nil, key)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var post db.Post
	json.Unmarshal(w.Body.Bytes(), &post)
	if post.Content != "find me" {
		t.Errorf("content = %q, want find me", post.Content)
	}
}

func TestGetPostNotFound(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/posts/999", nil, key)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Git commits ---

func TestListCommitsEmpty(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/git/commits", nil, key)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() == "null\n" {
		t.Error("empty commits should return [], not null")
	}
}

func TestGetLeavesEmpty(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/git/leaves", nil, key)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestGetCommitNotFound(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/git/commits/abc123abc123abc123abc123abc123abc123abc1", nil, key)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetCommitInvalidHash(t *testing.T) {
	t.Parallel()
	srv, database, cleanup := testServer(t)
	defer cleanup()
	key := createTestAgent(t, database, "agent-1")

	w := doRequest(srv, "GET", "/api/git/commits/not-a-hash", nil, key)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- Auth ---

func TestAuthRequired(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/channels"},
		{"GET", "/api/git/commits"},
		{"GET", "/api/git/leaves"},
	}

	for _, ep := range endpoints {
		w := doRequest(srv, ep.method, ep.path, nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", ep.method, ep.path, w.Code)
		}
	}
}

func TestAuthInvalidKey(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := testServer(t)
	defer cleanup()

	w := doRequest(srv, "GET", "/api/channels", nil, "bogus-key")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
