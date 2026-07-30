package storage

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Item struct {
	ID                    int64     `json:"id"`
	FeedID                int64     `json:"feed_id"`
	GUID                  string    `json:"guid"`
	Title                 string    `json:"title"`
	TranslatedTitle       *string   `json:"translated_title,omitempty"`
	TitleTranslationState string    `json:"title_translation_state"`
	Link                  string    `json:"link"`
	Date                  time.Time `json:"date"`
	Content               string    `json:"content"`
	Author                string    `json:"author"`
	Status                string    `json:"status"`
	Starred               bool      `json:"starred"`
	Image                 *string   `json:"image"`
	Hidden                bool      `json:"-"`
}

type ItemFilter struct {
	Status      string
	Starred     *bool
	FeedID      *int64
	FolderID    *int64
	Search      string
	After       *time.Time // i.date >= After  (GReader ot= param: items after this time)
	Before      *time.Time // i.date <= Before (GReader mark-all-as-read ts= param)
	OldestFirst bool       // ORDER BY date ASC; default is DESC (newest first)
	Cursor      *ItemCursor
}

// ItemCursor identifies the final item from the previous page.
type ItemCursor struct {
	Date time.Time
	ID   int64
}

// where builds the WHERE clause for non-search filters.
func (f ItemFilter) where() (string, []interface{}) {
	clauses := []string{"i.hidden = 0"}
	var args []interface{}

	if f.Status != "" {
		clauses = append(clauses, "i.status = ?")
		args = append(args, f.Status)
	}
	if f.Starred != nil {
		if *f.Starred {
			clauses = append(clauses, "i.starred = 1")
		} else {
			clauses = append(clauses, "i.starred = 0")
		}
	}
	if f.FeedID != nil {
		clauses = append(clauses, "i.feed_id = ?")
		args = append(args, *f.FeedID)
	}
	if f.FolderID != nil {
		clauses = append(clauses, "f.folder_id = ?")
		args = append(args, *f.FolderID)
	}
	if f.After != nil {
		clauses = append(clauses, "i.date >= ?")
		args = append(args, f.After.UTC().Format(time.RFC3339Nano))
	}
	if f.Before != nil {
		clauses = append(clauses, "i.date <= ?")
		args = append(args, f.Before.UTC().Format(time.RFC3339Nano))
	}
	if f.Cursor != nil {
		date := f.Cursor.Date.UTC().Format(time.RFC3339Nano)
		if f.OldestFirst {
			clauses = append(clauses, "(i.date > ? OR (i.date = ? AND i.id > ?))")
		} else {
			clauses = append(clauses, "(i.date < ? OR (i.date = ? AND i.id < ?))")
		}
		args = append(args, date, date, f.Cursor.ID)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

const itemSelect = `
	SELECT i.id, i.feed_id, i.guid, i.title, i.translated_title, i.title_translation_state, i.link, i.date, i.content, i.author, i.status, i.starred, i.image, i.hidden
	FROM items i
	JOIN feeds f ON f.id = i.feed_id`

func (s *Storage) buildItemQuery(filter ItemFilter, suffix string) (string, []interface{}) {
	where, args := filter.where()
	where, args, ftsJoin := applySearchFilter(where, args, filter.Search)

	query := fmt.Sprintf("%s\n%s\n%s\n%s", itemSelect, ftsJoin, where, suffix)
	return query, args
}

func (s *Storage) ListItems(filter ItemFilter, limit, offset int) ([]Item, error) {
	order := "DESC"
	if filter.OldestFirst {
		order = "ASC"
	}
	query, args := s.buildItemQuery(filter, fmt.Sprintf("ORDER BY i.date %s, i.id %s\nLIMIT ? OFFSET ?", order, order))
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.TranslatedTitle, &item.TitleTranslationState, &item.Link,
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Starred, &item.Image, &item.Hidden); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Storage) CountItems(filter ItemFilter) (int64, error) {
	where, args := filter.where()
	where, args, ftsJoin := applySearchFilter(where, args, filter.Search)

	// #nosec G201 -- ftsJoin and where contain only fixed SQL fragments; values are bound in args.
	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM items i
		JOIN feeds f ON f.id = i.feed_id
		%s
		%s`, ftsJoin, where)

	var count int64
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (s *Storage) GetItem(id int64) (*Item, error) {
	item := &Item{}
	err := s.db.QueryRow(`
		SELECT id, feed_id, guid, title, translated_title, title_translation_state, link, date, content, author, status, starred, image, hidden
		FROM items WHERE id = ? AND hidden = 0`, id).
		Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.TranslatedTitle, &item.TitleTranslationState, &item.Link,
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Starred, &item.Image, &item.Hidden)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Storage) CreateItems(items []Item) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insertItem, err := tx.Prepare(`
		INSERT INTO items (feed_id, guid, title, link, date, content, author, image, status, hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_id, guid) DO NOTHING`)
	if err != nil {
		return err
	}
	defer insertItem.Close()

	insertSearch, err := tx.Prepare(`INSERT INTO search(rowid, title, translated_title) VALUES (?, ?, '')`)
	if err != nil {
		return err
	}
	defer insertSearch.Close()
	keywordsByFeed := make(map[int64]string)

	for _, item := range items {
		keywords, ok := keywordsByFeed[item.FeedID]
		if !ok {
			if err := tx.QueryRow(`SELECT title_filter_keywords FROM feeds WHERE id = ?`, item.FeedID).Scan(&keywords); err != nil {
				return err
			}
			keywordsByFeed[item.FeedID] = keywords
		}
		item.Hidden = item.Hidden || TitleMatchesFilter(item.Title, keywords)
		status := item.Status
		if status == "" {
			status = "unread"
		}
		res, err := insertItem.Exec(item.FeedID, item.GUID, item.Title, item.Link,
			item.Date.UTC().Format(time.RFC3339Nano), item.Content, item.Author, item.Image, status, item.Hidden)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// duplicate — already exists, skip FTS insert
			continue
		}
		id, _ := res.LastInsertId()
		if _, err := insertSearch.Exec(id, item.Title); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// applySearchFilter searches article titles only. The trigram FTS index covers
// literal substring queries of three or more characters; short queries fall
// back to a title-only scan because trigram cannot index them.
func applySearchFilter(where string, args []interface{}, search string) (string, []interface{}, string) {
	if search == "" {
		return where, args, ""
	}

	clause := ""
	ftsJoin := ""
	if utf8.RuneCountInString(search) < 3 {
		clause = "(instr(i.title, ?) > 0 OR instr(COALESCE(i.translated_title, ''), ?) > 0)"
		args = append(args, search, search)
	} else {
		ftsJoin = "JOIN search ON search.rowid = i.id"
		clause = "search MATCH ?"
		args = append(args, `"`+strings.ReplaceAll(search, `"`, `""`)+`"`)
	}
	if where == "" {
		where = "WHERE " + clause
	} else {
		where += " AND " + clause
	}
	return where, args, ftsJoin
}

func (s *Storage) UpdateItemStatus(id int64, status string) error {
	if status != "read" && status != "unread" {
		return fmt.Errorf("invalid item status: %q", status)
	}
	_, err := s.db.Exec("UPDATE items SET status = ? WHERE id = ?", status, id)
	return err
}

func (s *Storage) SetStarred(id int64, starred bool) error {
	v := 0
	if starred {
		v = 1
	}
	_, err := s.db.Exec("UPDATE items SET starred = ? WHERE id = ?", v, id)
	return err
}

func (s *Storage) MarkAllRead(filter ItemFilter) error {
	where, args := filter.where()
	if where == "" {
		where = "WHERE i.status = 'unread'"
	} else {
		where += " AND i.status = 'unread'"
	}
	// #nosec G201 -- where is assembled from fixed ItemFilter clauses; values are bound in args.
	query := fmt.Sprintf(`
		UPDATE items SET status = 'read'
		WHERE id IN (
			SELECT i.id FROM items i
			JOIN feeds f ON f.id = i.feed_id
			%s
		)`, where)
	_, err := s.db.Exec(query, args...)
	return err
}

// FeedUnreadStat holds unread count and newest unread date for a feed.
type FeedUnreadStat struct {
	FeedID     int64
	Count      int64
	NewestDate time.Time
}

// FeedCount holds an item count for a feed.
type FeedCount struct {
	FeedID int64
	Count  int64
}

// GetUnreadCountsByFeed returns unread count and newest unread item date per feed in a single query.
func (s *Storage) GetUnreadCountsByFeed() ([]FeedUnreadStat, error) {
	rows, err := s.db.Query(`
		SELECT feed_id, COUNT(*), MAX(date)
		FROM items
		WHERE status = 'unread' AND hidden = 0
		GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []FeedUnreadStat
	for rows.Next() {
		var feedID, count int64
		var dateStr string
		if err := rows.Scan(&feedID, &count, &dateStr); err != nil {
			return nil, err
		}
		newestDate, err := parseDate(dateStr)
		if err != nil {
			return nil, err
		}
		stats = append(stats, FeedUnreadStat{
			FeedID:     feedID,
			Count:      count,
			NewestDate: newestDate,
		})
	}
	return stats, rows.Err()
}

// GetStarredCountsByFeed returns the number of starred items for each feed.
func (s *Storage) GetStarredCountsByFeed() ([]FeedCount, error) {
	rows, err := s.db.Query(`
		SELECT feed_id, COUNT(*)
		FROM items
		WHERE starred = 1 AND hidden = 0
		GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []FeedCount
	for rows.Next() {
		var stat FeedCount
		if err := rows.Scan(&stat.FeedID, &stat.Count); err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

// parseDate parses a date string stored by the SQLite driver.
// Both go-sqlite3 and modernc.org/sqlite use RFC3339Nano.
var dateFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range dateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date format: %q", s)
}

// DeleteOldItems removes items older than 90 days per feed, keeping:
// - all starred items
// - at least 50 most recent items per feed
func (s *Storage) DeleteOldItems() error {
	const oldItemIDs = `SELECT id FROM items
		WHERE starred = 0
		  AND date < datetime('now', '-90 days')
		  AND id NOT IN (
		      SELECT id FROM items i2
		      WHERE i2.feed_id = items.feed_id
		      ORDER BY i2.date DESC
		      LIMIT 50
		  )`
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM search WHERE rowid IN (" + oldItemIDs + ")"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM items WHERE id IN (" + oldItemIDs + ")"); err != nil {
		return err
	}
	return tx.Commit()
}
