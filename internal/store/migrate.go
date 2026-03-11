package store

import "database/sql"

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			fingerprint TEXT NOT NULL UNIQUE,
			public_key TEXT NOT NULL,
			bio TEXT NOT NULL DEFAULT '',
			public BOOLEAN NOT NULL DEFAULT 0,
			joined_at DATETIME NOT NULL DEFAULT (datetime('now')),
			invited_by INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_id INTEGER NOT NULL REFERENCES agents(id),
			to_id INTEGER NOT NULL REFERENCES agents(id),
			body TEXT NOT NULL,
			file_name TEXT,
			file_path TEXT,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			read_at DATETIME
		);

		CREATE INDEX IF NOT EXISTS idx_messages_to ON messages(to_id, read_at);
		CREATE INDEX IF NOT EXISTS idx_messages_from ON messages(from_id);

		CREATE TABLE IF NOT EXISTS group_members (
			group_id INTEGER NOT NULL REFERENCES agents(id),
			member_id INTEGER NOT NULL REFERENCES agents(id),
			role TEXT NOT NULL DEFAULT 'member',
			joined_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (group_id, member_id)
		);

		CREATE TABLE IF NOT EXISTS agent_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id INTEGER NOT NULL REFERENCES agents(id),
			fingerprint TEXT NOT NULL UNIQUE,
			public_key TEXT NOT NULL,
			added_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS invites (
			code TEXT PRIMARY KEY,
			created_by INTEGER NOT NULL REFERENCES agents(id),
			redeemed_by INTEGER REFERENCES agents(id),
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			redeemed_at DATETIME
		);
	`)
	if err != nil {
		return err
	}
	// Seed public board agent (fingerprint="board" so nobody can auth as it directly)
	_, err = db.Exec(`INSERT OR IGNORE INTO agents (name, fingerprint, public_key, bio, public) VALUES ('board', 'board', '', 'Public bulletin board — anyone can read', 1)`)
	if err != nil {
		return err
	}

	// Migrate existing agent keys into agent_keys table
	_, err = db.Exec(`
		INSERT OR IGNORE INTO agent_keys (agent_id, fingerprint, public_key)
		SELECT id, fingerprint, public_key FROM agents
		WHERE public_key != '' AND fingerprint NOT LIKE 'group:%'
	`)
	if err != nil {
		return err
	}

	// Add email column to agents (ignore error if column already exists)
	db.Exec(`ALTER TABLE agents ADD COLUMN email TEXT DEFAULT NULL`)
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_email ON agents(email) WHERE email IS NOT NULL`)
	return err
}
