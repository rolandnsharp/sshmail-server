package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(dataDir string) (*SQLiteStore, error) {
	dbPath := filepath.Join(dataDir, "hub.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// --- Agents ---

func (s *SQLiteStore) AgentByFingerprint(fingerprint string) (*Agent, error) {
	return s.scanAgent(`SELECT id, name, fingerprint, public_key, bio, public, guest, accept_anon, joined_at, invited_by FROM agents WHERE fingerprint = ?`, fingerprint)
}

func (s *SQLiteStore) AgentByID(id int64) (*Agent, error) {
	return s.scanAgent(`SELECT id, name, fingerprint, public_key, bio, public, guest, accept_anon, joined_at, invited_by FROM agents WHERE id = ?`, id)
}

func (s *SQLiteStore) AgentByName(name string) (*Agent, error) {
	return s.scanAgent(`SELECT id, name, fingerprint, public_key, bio, public, guest, accept_anon, joined_at, invited_by FROM agents WHERE name = ?`, name)
}

func (s *SQLiteStore) scanAgent(query string, arg any) (*Agent, error) {
	a := &Agent{}
	err := s.db.QueryRow(query, arg).Scan(&a.ID, &a.Name, &a.Fingerprint, &a.PublicKey, &a.Bio, &a.Public, &a.Guest, &a.AcceptAnon, &a.JoinedAt, &a.InvitedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *SQLiteStore) CreateAgent(name, fingerprint, publicKey string, invitedBy int64) (*Agent, error) {
	res, err := s.db.Exec(
		`INSERT INTO agents (name, fingerprint, public_key, invited_by) VALUES (?, ?, ?, ?)`,
		name, fingerprint, publicKey, invitedBy,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.AgentByID(id)
}

func (s *SQLiteStore) UpdateBio(id int64, bio string) error {
	_, err := s.db.Exec(`UPDATE agents SET bio = ? WHERE id = ?`, bio, id)
	return err
}

func (s *SQLiteStore) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT id, name, fingerprint, public_key, bio, public, guest, accept_anon, joined_at, invited_by FROM agents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Fingerprint, &a.PublicKey, &a.Bio, &a.Public, &a.Guest, &a.AcceptAnon, &a.JoinedAt, &a.InvitedBy); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// --- Guest Agents ---

func (s *SQLiteStore) GetOrCreateGuest(fingerprint string) (*Agent, error) {
	// Check if a guest agent already exists for this fingerprint
	a, err := s.AgentByFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a, nil
	}

	// Create a guest agent with a short fingerprint-based name
	name := "guest-" + fingerprint[len("SHA256:"):][:8]
	res, err := s.db.Exec(
		`INSERT INTO agents (name, fingerprint, public_key, bio, guest) VALUES (?, ?, '', 'anonymous sender', 1)`,
		name, fingerprint,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.AgentByID(id)
}

// --- Recipient Controls ---

func (s *SQLiteStore) SetAcceptAnon(id int64, accept bool) error {
	_, err := s.db.Exec(`UPDATE agents SET accept_anon = ? WHERE id = ?`, accept, id)
	return err
}

func (s *SQLiteStore) BlockFingerprint(agentID int64, fingerprint string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO blocks (agent_id, fingerprint) VALUES (?, ?)`,
		agentID, fingerprint,
	)
	return err
}

func (s *SQLiteStore) UnblockFingerprint(agentID int64, fingerprint string) error {
	_, err := s.db.Exec(
		`DELETE FROM blocks WHERE agent_id = ? AND fingerprint = ?`,
		agentID, fingerprint,
	)
	return err
}

func (s *SQLiteStore) IsBlocked(agentID int64, fingerprint string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM blocks WHERE agent_id = ? AND fingerprint = ?`,
		agentID, fingerprint,
	).Scan(&count)
	return count > 0, err
}

func (s *SQLiteStore) ListBlocks(agentID int64) ([]Block, error) {
	rows, err := s.db.Query(
		`SELECT id, agent_id, fingerprint, created_at FROM blocks WHERE agent_id = ? ORDER BY created_at DESC`,
		agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blocks []Block
	for rows.Next() {
		var b Block
		if err := rows.Scan(&b.ID, &b.AgentID, &b.Fingerprint, &b.CreatedAt); err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

// --- Messages ---

func (s *SQLiteStore) SendMessage(fromID, toID int64, body string, fileName, filePath *string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO messages (from_id, to_id, body, file_name, file_path) VALUES (?, ?, ?, ?, ?)`,
		fromID, toID, body, fileName, filePath,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) Inbox(agentID int64, all bool) ([]Message, error) {
	query := `
		SELECT m.id, m.from_id, f.name, m.to_id, t.name, m.body, m.file_name, m.file_path, m.created_at, m.read_at
		FROM messages m
		JOIN agents f ON m.from_id = f.id
		JOIN agents t ON m.to_id = t.id
		WHERE m.to_id = ?`
	if !all {
		query += ` AND m.read_at IS NULL`
	}
	query += ` ORDER BY m.created_at DESC`

	rows, err := s.db.Query(query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromID, &m.FromName, &m.ToID, &m.ToName, &m.Body, &m.FileName, &m.FilePath, &m.CreatedAt, &m.ReadAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *SQLiteStore) GetMessage(id int64) (*Message, error) {
	m := &Message{}
	err := s.db.QueryRow(`
		SELECT m.id, m.from_id, f.name, m.to_id, t.name, m.body, m.file_name, m.file_path, m.created_at, m.read_at
		FROM messages m
		JOIN agents f ON m.from_id = f.id
		JOIN agents t ON m.to_id = t.id
		WHERE m.id = ?
	`, id).Scan(&m.ID, &m.FromID, &m.FromName, &m.ToID, &m.ToName, &m.Body, &m.FileName, &m.FilePath, &m.CreatedAt, &m.ReadAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *SQLiteStore) MarkRead(id int64) error {
	_, err := s.db.Exec(`UPDATE messages SET read_at = datetime('now') WHERE id = ? AND read_at IS NULL`, id)
	return err
}

func (s *SQLiteStore) UnreadCount(agentID int64) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE to_id = ? AND read_at IS NULL`, agentID).Scan(&count)
	return count, err
}

// --- Invites ---

func (s *SQLiteStore) CreateInvite(createdBy int64) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := hex.EncodeToString(b)
	_, err := s.db.Exec(`INSERT INTO invites (code, created_by) VALUES (?, ?)`, code, createdBy)
	return code, err
}

func (s *SQLiteStore) RedeemInvite(code, name, fingerprint, publicKey string) (*Agent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var createdBy int64
	var redeemedBy sql.NullInt64
	err = tx.QueryRow(`SELECT created_by, redeemed_by FROM invites WHERE code = ?`, code).Scan(&createdBy, &redeemedBy)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid invite code")
	}
	if err != nil {
		return nil, err
	}
	if redeemedBy.Valid {
		return nil, fmt.Errorf("invite already redeemed")
	}

	res, err := tx.Exec(
		`INSERT INTO agents (name, fingerprint, public_key, invited_by) VALUES (?, ?, ?, ?)`,
		name, fingerprint, publicKey, createdBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	agentID, _ := res.LastInsertId()

	_, err = tx.Exec(`UPDATE invites SET redeemed_by = ?, redeemed_at = datetime('now') WHERE code = ?`, agentID, code)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.AgentByID(agentID)
}
