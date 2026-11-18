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
	// 10: FTS 全文検索テーブル（新規インストールは fts5 で作成）
	`CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(title, body)`,
	// 11: 既存アイテムをバックフィル
	`INSERT OR IGNORE INTO search(rowid, title, body) SELECT id, title, content FROM items`,
	// 12: starred を独立 BOOLEAN カラムに分離
	`ALTER TABLE items ADD COLUMN starred BOOLEAN NOT NULL DEFAULT 0`,
	// 13: status='starred' の行を starred=1, status='read' に変換
	`UPDATE items SET starred = 1, status = 'read' WHERE status = 'starred'`,
	// 14: fts4 → fts5 移行: 旧テーブルを削除（fts4 で作成済みの既存 DB 向け）
	`DROP TABLE IF EXISTS search`,
	// 15: fts5 テーブルを再作成
	`CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(title, body)`,
	// 16: items から再バックフィル
	`INSERT INTO search(rowid, title, body) SELECT id, title, content FROM items`,
	// 17: 本文を検索対象から外し、日本語の部分一致用に索引を作り直す
	`DROP TABLE IF EXISTS search`,
	// 18: 記事タイトル専用の trigram FTS5 索引
	`CREATE VIRTUAL TABLE search USING fts5(title, tokenize='trigram')`,
	// 19: 既存記事のタイトルをバックフィル
	`INSERT INTO search(rowid, title) SELECT id, title FROM items`,
	// 20: フィードごとの記事タイトル非表示キーワード（カンマ区切り）
	`ALTER TABLE feeds ADD COLUMN title_filter_keywords TEXT NOT NULL DEFAULT ''`,
	// 21: フィードのタイトル非表示設定に一致した記事
	`ALTER TABLE items ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT 0`,
	// 22: Web UI 用の記事タイトル翻訳
	`ALTER TABLE items ADD COLUMN translated_title TEXT`,
	// 23: タイトル翻訳の処理状態と所有中の claim
	`ALTER TABLE items ADD COLUMN title_translation_state TEXT NOT NULL DEFAULT 'pending'`,
	// 24: 翻訳タイトルも検索できるよう trigram FTS を作り直す
	`DROP TABLE IF EXISTS search`,
	// 25
	`CREATE VIRTUAL TABLE search USING fts5(title, translated_title, tokenize='trigram')`,
	// 26
	`INSERT INTO search(rowid, title, translated_title) SELECT id, title, translated_title FROM items`,
	// 27
	`ALTER TABLE items ADD COLUMN title_translation_claim TEXT`,
	// 28
	`CREATE INDEX IF NOT EXISTS idx_items_title_translation_claim ON items(title_translation_claim)`,
	// 29: title translation state invariants for all future writes.
	`CREATE TRIGGER items_title_translation_insert
	BEFORE INSERT ON items
	WHEN NEW.title_translation_state NOT IN ('pending', 'processing', 'translated', 'skipped', 'failed')
	  OR (NEW.title_translation_state = 'translated') != (NEW.translated_title IS NOT NULL)
	  OR (NEW.title_translation_state = 'processing') != (NEW.title_translation_claim IS NOT NULL)
	BEGIN SELECT RAISE(ABORT, 'invalid title translation state'); END`,
	// 30
	`CREATE TRIGGER items_title_translation_update
	BEFORE UPDATE OF translated_title, title_translation_state, title_translation_claim ON items
	WHEN NEW.title_translation_state NOT IN ('pending', 'processing', 'translated', 'skipped', 'failed')
	  OR (NEW.title_translation_state = 'translated') != (NEW.translated_title IS NOT NULL)
	  OR (NEW.title_translation_state = 'processing') != (NEW.title_translation_claim IS NOT NULL)
	BEGIN SELECT RAISE(ABORT, 'invalid title translation state'); END`,
	// 31: first storage time for GReader incremental sync. Older rows have no recorded time.
	`ALTER TABLE items ADD COLUMN stored_at INTEGER`,
	// 32: preserve the largest assigned article ID across reader synchronization.
	`CREATE TABLE item_id_sequence AS SELECT COALESCE(MAX(id), 0) AS last_id FROM items`,
	// 33
	`CREATE TRIGGER items_advance_id_sequence AFTER INSERT ON items
	BEGIN UPDATE item_id_sequence SET last_id = MAX(last_id, NEW.id); END`,
	// 34: Skip previously stored links on feed refresh.
	`CREATE INDEX idx_items_feed_link ON items(feed_id, link)`,
	// 35: Read visible articles for one feed in date order.
	`CREATE INDEX idx_items_visible_feed_date ON items(feed_id, date, id) WHERE hidden = 0`,
	// 36: Count visible starred articles without scanning other articles.
	`CREATE INDEX idx_items_visible_starred_feed ON items(feed_id) WHERE hidden = 0 AND starred = 1`,
	// 37: A new subscription must not inherit refreshes for a deleted feed.
	`CREATE TABLE feed_id_sequence AS SELECT COALESCE(MAX(id), 0) AS last_id FROM feeds`,
	// 38
	`CREATE TRIGGER feeds_advance_id_sequence AFTER INSERT ON feeds
	BEGIN UPDATE feed_id_sequence SET last_id = MAX(last_id, NEW.id); END`,
}

func (s *Storage) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`); err != nil {
		return err
	}

	needsTitleFilterReapply := false
	for i, sql := range migrations {
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", i).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := s.applyMigration(i, sql); err != nil {
			return err
		}
		if i == 21 {
			needsTitleFilterReapply = true
		}
	}
	if needsTitleFilterReapply {
		return s.reapplyTitleFilterKeywords()
	}
	return nil
}

func (s *Storage) applyMigration(version int, statement string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(statement); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return err
	}
	return tx.Commit()
}
