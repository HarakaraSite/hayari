package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

func Open(path string) (*Storage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}

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

// stripHTML extracts plain text from an HTML string for FTS indexing.
func stripHTML(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.TextToken {
			b.Write(z.Text())
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(b.String())
}
