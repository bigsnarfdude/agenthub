package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Model structs

type Agent struct {
	ID        string    `json:"id"`
	APIKey    string    `json:"api_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Commit struct {
	Hash       string    `json:"hash"`
	ParentHash string    `json:"parent_hash"`
	AgentID    string    `json:"agent_id"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

type Channel struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Post struct {
	ID        int       `json:"id"`
	ChannelID int       `json:"channel_id"`
	AgentID   string    `json:"agent_id"`
	ParentID  *int      `json:"parent_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Event struct {
	ID        int       `json:"id"`
	AgentID   string    `json:"agent_id"`
	EventType string    `json:"event_type"`
	Payload   string    `json:"payload"`
	Tags      string    `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

type Result struct {
	ID           int       `json:"id"`
	AgentID      string    `json:"agent_id"`
	Experiment   string    `json:"experiment"`
	Metric       string    `json:"metric"`
	Score        float64   `json:"score"`
	Platform     string    `json:"platform"`
	CodeSnapshot string    `json:"code_snapshot,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// DB wraps the SQLite connection.
type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite pragmas for performance and correctness
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := sqldb.Exec(pragma); err != nil {
			sqldb.Close()
			return nil, fmt.Errorf("set pragma %q: %w", pragma, err)
		}
	}
	return &DB{db: sqldb}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) Migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			api_key TEXT UNIQUE NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS commits (
			hash TEXT PRIMARY KEY,
			parent_hash TEXT,
			agent_id TEXT REFERENCES agents(id),
			message TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			description TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL REFERENCES channels(id),
			agent_id TEXT NOT NULL REFERENCES agents(id),
			parent_id INTEGER REFERENCES posts(id),
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS rate_limits (
			agent_id TEXT NOT NULL,
			action TEXT NOT NULL,
			window_start TIMESTAMP NOT NULL,
			count INTEGER DEFAULT 1,
			PRIMARY KEY (agent_id, action, window_start)
		);

		CREATE TABLE IF NOT EXISTS results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL REFERENCES agents(id),
			experiment TEXT NOT NULL,
			metric TEXT NOT NULL DEFAULT 'score',
			score REAL NOT NULL,
			platform TEXT NOT NULL DEFAULT 'unknown',
			code_snapshot TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_commits_parent ON commits(parent_hash);
		CREATE INDEX IF NOT EXISTS idx_commits_agent ON commits(agent_id);
		CREATE INDEX IF NOT EXISTS idx_posts_channel ON posts(channel_id);
		CREATE INDEX IF NOT EXISTS idx_posts_parent ON posts(parent_id);
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL REFERENCES agents(id),
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '',
			tags TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_results_experiment ON results(experiment);
		CREATE INDEX IF NOT EXISTS idx_results_agent ON results(agent_id);
		CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
		CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent_id);
	`)
	return err
}

// --- Agents ---

func (d *DB) CreateAgent(id, apiKey string) error {
	_, err := d.db.Exec("INSERT INTO agents (id, api_key) VALUES (?, ?)", id, apiKey)
	return err
}

func (d *DB) GetAgentByAPIKey(apiKey string) (*Agent, error) {
	var a Agent
	err := d.db.QueryRow("SELECT id, api_key, created_at FROM agents WHERE api_key = ?", apiKey).
		Scan(&a.ID, &a.APIKey, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

func (d *DB) GetAgentByID(id string) (*Agent, error) {
	var a Agent
	err := d.db.QueryRow("SELECT id, api_key, created_at FROM agents WHERE id = ?", id).
		Scan(&a.ID, &a.APIKey, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

// --- Commits ---

func (d *DB) InsertCommit(hash, parentHash, agentID, message string) error {
	_, err := d.db.Exec(
		"INSERT INTO commits (hash, parent_hash, agent_id, message) VALUES (?, ?, ?, ?)",
		hash, parentHash, agentID, message,
	)
	return err
}

func (d *DB) GetCommit(hash string) (*Commit, error) {
	var c Commit
	var parentHash sql.NullString
	err := d.db.QueryRow(
		"SELECT hash, parent_hash, agent_id, message, created_at FROM commits WHERE hash = ?", hash,
	).Scan(&c.Hash, &parentHash, &c.AgentID, &c.Message, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if parentHash.Valid {
		c.ParentHash = parentHash.String
	}
	return &c, err
}

func (d *DB) ListCommits(agentID string, limit, offset int) ([]Commit, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if agentID != "" {
		rows, err = d.db.Query(
			"SELECT hash, parent_hash, agent_id, message, created_at FROM commits WHERE agent_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?",
			agentID, limit, offset,
		)
	} else {
		rows, err = d.db.Query(
			"SELECT hash, parent_hash, agent_id, message, created_at FROM commits ORDER BY created_at DESC LIMIT ? OFFSET ?",
			limit, offset,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommits(rows)
}

func (d *DB) GetChildren(hash string) ([]Commit, error) {
	rows, err := d.db.Query(
		"SELECT hash, parent_hash, agent_id, message, created_at FROM commits WHERE parent_hash = ? ORDER BY created_at DESC",
		hash,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommits(rows)
}

func (d *DB) GetLineage(hash string) ([]Commit, error) {
	var lineage []Commit
	current := hash
	for current != "" {
		c, err := d.GetCommit(current)
		if err != nil {
			return lineage, err
		}
		if c == nil {
			break
		}
		lineage = append(lineage, *c)
		current = c.ParentHash
	}
	return lineage, nil
}

func (d *DB) GetLeaves() ([]Commit, error) {
	rows, err := d.db.Query(`
		SELECT c.hash, c.parent_hash, c.agent_id, c.message, c.created_at
		FROM commits c
		LEFT JOIN commits child ON child.parent_hash = c.hash
		WHERE child.hash IS NULL
		ORDER BY c.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommits(rows)
}

func scanCommits(rows *sql.Rows) ([]Commit, error) {
	var commits []Commit
	for rows.Next() {
		var c Commit
		var parentHash sql.NullString
		if err := rows.Scan(&c.Hash, &parentHash, &c.AgentID, &c.Message, &c.CreatedAt); err != nil {
			return nil, err
		}
		if parentHash.Valid {
			c.ParentHash = parentHash.String
		}
		commits = append(commits, c)
	}
	return commits, rows.Err()
}

// --- Channels ---

func (d *DB) CreateChannel(name, description string) error {
	_, err := d.db.Exec("INSERT INTO channels (name, description) VALUES (?, ?)", name, description)
	return err
}

func (d *DB) ListChannels() ([]Channel, error) {
	rows, err := d.db.Query("SELECT id, name, description, created_at FROM channels ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Description, &ch.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (d *DB) GetChannelByName(name string) (*Channel, error) {
	var ch Channel
	err := d.db.QueryRow("SELECT id, name, description, created_at FROM channels WHERE name = ?", name).
		Scan(&ch.ID, &ch.Name, &ch.Description, &ch.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &ch, err
}

// --- Posts ---

func (d *DB) CreatePost(channelID int, agentID string, parentID *int, content string) (*Post, error) {
	res, err := d.db.Exec(
		"INSERT INTO posts (channel_id, agent_id, parent_id, content) VALUES (?, ?, ?, ?)",
		channelID, agentID, parentID, content,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return d.GetPost(int(id))
}

func (d *DB) ListPosts(channelID, limit, offset int) ([]Post, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(
		"SELECT id, channel_id, agent_id, parent_id, content, created_at FROM posts WHERE channel_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?",
		channelID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (d *DB) GetPost(id int) (*Post, error) {
	var p Post
	var parentID sql.NullInt64
	err := d.db.QueryRow(
		"SELECT id, channel_id, agent_id, parent_id, content, created_at FROM posts WHERE id = ?", id,
	).Scan(&p.ID, &p.ChannelID, &p.AgentID, &parentID, &p.Content, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if parentID.Valid {
		v := int(parentID.Int64)
		p.ParentID = &v
	}
	return &p, err
}

func (d *DB) GetReplies(postID int) ([]Post, error) {
	rows, err := d.db.Query(
		"SELECT id, channel_id, agent_id, parent_id, content, created_at FROM posts WHERE parent_id = ? ORDER BY created_at ASC",
		postID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func scanPosts(rows *sql.Rows) ([]Post, error) {
	var posts []Post
	for rows.Next() {
		var p Post
		var parentID sql.NullInt64
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.AgentID, &parentID, &p.Content, &p.CreatedAt); err != nil {
			return nil, err
		}
		if parentID.Valid {
			v := int(parentID.Int64)
			p.ParentID = &v
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// --- Dashboard queries ---

type Stats struct {
	AgentCount  int
	CommitCount int
	PostCount   int
}

func (d *DB) GetStats() (*Stats, error) {
	var s Stats
	d.db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&s.AgentCount)
	d.db.QueryRow("SELECT COUNT(*) FROM commits").Scan(&s.CommitCount)
	d.db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&s.PostCount)
	return &s, nil
}

func (d *DB) ListAgents() ([]Agent, error) {
	rows, err := d.db.Query("SELECT id, '', created_at FROM agents ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.APIKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.APIKey = "" // never expose
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// RecentPosts returns recent posts across all channels with channel name joined in.
type PostWithChannel struct {
	Post
	ChannelName string
}

func (d *DB) RecentPosts(limit int) ([]PostWithChannel, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`
		SELECT p.id, p.channel_id, p.agent_id, p.parent_id, p.content, p.created_at, c.name
		FROM posts p JOIN channels c ON p.channel_id = c.id
		ORDER BY p.created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []PostWithChannel
	for rows.Next() {
		var p PostWithChannel
		var parentID sql.NullInt64
		if err := rows.Scan(&p.ID, &p.ChannelID, &p.AgentID, &parentID, &p.Content, &p.CreatedAt, &p.ChannelName); err != nil {
			return nil, err
		}
		if parentID.Valid {
			v := int(parentID.Int64)
			p.ParentID = &v
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// --- Rate Limiting ---

// CheckRateLimit returns true if the agent is within the allowed rate.
func (d *DB) CheckRateLimit(agentID, action string, maxPerHour int) (bool, error) {
	var count int
	err := d.db.QueryRow(
		"SELECT COALESCE(SUM(count), 0) FROM rate_limits WHERE agent_id = ? AND action = ? AND window_start > datetime('now', '-1 hour')",
		agentID, action,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count < maxPerHour, nil
}

func (d *DB) IncrementRateLimit(agentID, action string) error {
	_, err := d.db.Exec(`
		INSERT INTO rate_limits (agent_id, action, window_start, count)
		VALUES (?, ?, strftime('%Y-%m-%d %H:%M:00', 'now'), 1)
		ON CONFLICT(agent_id, action, window_start) DO UPDATE SET count = count + 1
	`, agentID, action)
	return err
}

func (d *DB) CleanupRateLimits() error {
	_, err := d.db.Exec("DELETE FROM rate_limits WHERE window_start < datetime('now', '-2 hours')")
	return err
}

// --- Results ---

func (d *DB) InsertResult(agentID, experiment, metric string, score float64, platform, codeSnapshot string) (*Result, error) {
	if metric == "" {
		metric = "score"
	}
	if platform == "" {
		platform = "unknown"
	}
	res, err := d.db.Exec(
		"INSERT INTO results (agent_id, experiment, metric, score, platform, code_snapshot) VALUES (?, ?, ?, ?, ?, ?)",
		agentID, experiment, metric, score, platform, codeSnapshot,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var r Result
	err = d.db.QueryRow(
		"SELECT id, agent_id, experiment, metric, score, platform, code_snapshot, created_at FROM results WHERE id = ?", id,
	).Scan(&r.ID, &r.AgentID, &r.Experiment, &r.Metric, &r.Score, &r.Platform, &r.CodeSnapshot, &r.CreatedAt)
	return &r, err
}

func (d *DB) ListResults(experiment, agentID, platform string, limit, offset int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	query := "SELECT id, agent_id, experiment, metric, score, platform, code_snapshot, created_at FROM results WHERE 1=1"
	var args []any
	if experiment != "" {
		query += " AND experiment = ?"
		args = append(args, experiment)
	}
	if agentID != "" {
		query += " AND agent_id = ?"
		args = append(args, agentID)
	}
	if platform != "" {
		query += " AND platform = ?"
		args = append(args, platform)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

func (d *DB) Leaderboard(experiment, platform string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT r.id, r.agent_id, r.experiment, r.metric, r.score, r.platform, r.code_snapshot, r.created_at
		FROM results r
		INNER JOIN (
			SELECT agent_id, experiment, MAX(score) AS best_score
			FROM results
			WHERE 1=1`
	var args []any
	if experiment != "" {
		query += " AND experiment = ?"
		args = append(args, experiment)
	}
	if platform != "" {
		query += " AND platform = ?"
		args = append(args, platform)
	}
	query += `
			GROUP BY agent_id, experiment
		) best ON r.agent_id = best.agent_id AND r.experiment = best.experiment AND r.score = best.best_score`
	if experiment != "" {
		query += " WHERE r.experiment = ?"
		args = append(args, experiment)
	}
	if platform != "" {
		if experiment != "" {
			query += " AND r.platform = ?"
		} else {
			query += " WHERE r.platform = ?"
		}
		args = append(args, platform)
	}
	query += " ORDER BY r.score DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

func scanResults(rows *sql.Rows) ([]Result, error) {
	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Experiment, &r.Metric, &r.Score, &r.Platform, &r.CodeSnapshot, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// --- Events ---

func (d *DB) InsertEvent(agentID, eventType, payload, tags string) (*Event, error) {
	res, err := d.db.Exec(
		"INSERT INTO events (agent_id, event_type, payload, tags) VALUES (?, ?, ?, ?)",
		agentID, eventType, payload, tags,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var e Event
	err = d.db.QueryRow(
		"SELECT id, agent_id, event_type, payload, tags, created_at FROM events WHERE id = ?", id,
	).Scan(&e.ID, &e.AgentID, &e.EventType, &e.Payload, &e.Tags, &e.CreatedAt)
	return &e, err
}

func (d *DB) ListEvents(eventType, agentID, tags string, limit, offset int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	query := "SELECT id, agent_id, event_type, payload, tags, created_at FROM events WHERE 1=1"
	var args []any
	if eventType != "" {
		query += " AND event_type = ?"
		args = append(args, eventType)
	}
	if agentID != "" {
		query += " AND agent_id = ?"
		args = append(args, agentID)
	}
	if tags != "" {
		query += " AND tags LIKE ?"
		args = append(args, "%"+tags+"%")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.AgentID, &e.EventType, &e.Payload, &e.Tags, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CountRecentFailures counts FAILURE events for an experiment within the last hour.
func (d *DB) CountRecentFailures(experiment string) (int, error) {
	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(DISTINCT agent_id) FROM events WHERE event_type = 'FAILURE' AND payload LIKE ? AND created_at > datetime('now', '-1 hour')",
		"%"+experiment+"%",
	).Scan(&count)
	return count, err
}

// RecentResultScores returns recent RESULT event scores for an experiment to detect convergence.
func (d *DB) RecentResultScores(experiment string, limit int) ([]float64, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.db.Query(
		"SELECT score FROM results WHERE experiment = ? ORDER BY created_at DESC LIMIT ?",
		experiment, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scores []float64
	for rows.Next() {
		var s float64
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		scores = append(scores, s)
	}
	return scores, rows.Err()
}
