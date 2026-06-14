package storage

var migrations = []string{
	// 0
	`CREATE TABLE IF NOT EXISTS folders (
		id    INTEGER PRIMARY KEY,
		title TEXT NOT NULL
	)`,
	// 1
	`CREATE TABLE IF NOT EXISTS feeds (
		id             INTEGER PRIMARY KEY,
		folder_id      INTEGER REFERENCES folders(id) ON DELETE SET NULL,
		title          TEXT NOT NULL DEFAULT '',
		feed_url       TEXT NOT NULL UNIQUE,
		site_url       TEXT NOT NULL DEFAULT '',
		icon           TEXT,
		last_refreshed DATETIME,
		last_modified  TEXT,
		etag           TEXT
	)`,
	// 2
	`CREATE TABLE IF NOT EXISTS items (
		id      INTEGER PRIMARY KEY,
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		guid    TEXT,
		title   TEXT NOT NULL DEFAULT '',
		link    TEXT NOT NULL DEFAULT '',
		date    DATETIME,
		content TEXT NOT NULL DEFAULT '',
		author  TEXT NOT NULL DEFAULT '',
		status  TEXT NOT NULL DEFAULT 'unread',
		image   TEXT,
		UNIQUE(feed_id, guid)
	)`,
	// 3
	`CREATE TABLE IF NOT EXISTS filters (
		id      INTEGER PRIMARY KEY,
		feed_id INTEGER REFERENCES feeds(id) ON DELETE CASCADE,
		rule    TEXT NOT NULL,
		action  TEXT NOT NULL,
		field   TEXT NOT NULL
	)`,
	// 4
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		val TEXT
	)`,
	// 5
	`CREATE INDEX IF NOT EXISTS idx_items_feed_id ON items(feed_id)`,
	// 6
	`CREATE INDEX IF NOT EXISTS idx_items_status ON items(status)`,
	// 7: フォルダ開閉状態
	`ALTER TABLE folders ADD COLUMN is_expanded BOOLEAN NOT NULL DEFAULT 0`,
	// 8: フィードエラー記録
	`ALTER TABLE feeds ADD COLUMN last_error TEXT`,
	// 9: 日時インデックス（ソート性能）
	`CREATE INDEX IF NOT EXISTS idx_items_date ON items(date)`,
	// 10: FTS4 全文検索テーブル（go-sqlite3 デフォルトで有効）
	`CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts4(title, body)`,
	// 11: 既存アイテムをバックフィル（HTML タグ込みで可）
	`INSERT OR IGNORE INTO search(rowid, title, body) SELECT id, title, content FROM items`,
}

func (s *Storage) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`); err != nil {
		return err
	}

	for i, sql := range migrations {
		var count int
		s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", i).Scan(&count)
		if count > 0 {
			continue
		}
		if _, err := s.db.Exec(sql); err != nil {
			return err
		}
		if _, err := s.db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", i); err != nil {
			return err
		}
	}
	return nil
}
