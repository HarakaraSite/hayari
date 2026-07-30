package storage

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

func Open(path string) (*Storage, error) {
	// The database contains feed URLs and article content.  When this is the
	// default, newly-created per-user directory, do not expose it to other
	// local users.  Existing parent directories are intentionally left alone.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	// SQLite permits only one writer at a time. Keep this process to one
	// connection so concurrent feed refreshes and API writes are serialized
	// instead of failing with SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

// Ping verifies that the database remains reachable.
func (s *Storage) Ping() error {
	return s.db.Ping()
}
