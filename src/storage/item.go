package storage

import (
	"fmt"
	"strings"
	"time"
)

type Item struct {
	ID      int64     `json:"id"`
	FeedID  int64     `json:"feed_id"`
	GUID    string    `json:"guid"`
	Title   string    `json:"title"`
	Link    string    `json:"link"`
	Date    time.Time `json:"date"`
	Content string    `json:"content"`
	Author  string    `json:"author"`
	Status  string    `json:"status"`
	Starred bool      `json:"starred"`
	Image   *string   `json:"image"`
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
}

// where builds the WHERE clause for non-search filters.
func (f ItemFilter) where() (string, []interface{}) {
	var clauses []string
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

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

const itemSelect = `
	SELECT i.id, i.feed_id, i.guid, i.title, i.link, i.date, i.content, i.author, i.status, i.starred, i.image
	FROM items i
	JOIN feeds f ON f.id = i.feed_id`

func (s *Storage) buildItemQuery(filter ItemFilter, suffix string) (string, []interface{}) {
	where, args := filter.where()

	ftsJoin := ""
	if filter.Search != "" {
		ftsJoin = "JOIN search ON search.rowid = i.id"
		ftsClause := "search MATCH ?"
		if where == "" {
			where = "WHERE " + ftsClause
		} else {
			where += " AND " + ftsClause
		}
		args = append(args, filter.Search)
	}

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
		if err := rows.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link,
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Starred, &item.Image); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Storage) CountItems(filter ItemFilter) (int64, error) {
	where, args := filter.where()

	ftsJoin := ""
	if filter.Search != "" {
		ftsJoin = "JOIN search ON search.rowid = i.id"
		ftsClause := "search MATCH ?"
		if where == "" {
			where = "WHERE " + ftsClause
		} else {
			where += " AND " + ftsClause
		}
		args = append(args, filter.Search)
	}

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
		SELECT id, feed_id, guid, title, link, date, content, author, status, starred, image
		FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link,
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Starred, &item.Image)
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
		INSERT INTO items (feed_id, guid, title, link, date, content, author, image, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_id, guid) DO NOTHING`)
	if err != nil {
		return err
	}
	defer insertItem.Close()

	insertSearch, err := tx.Prepare(`INSERT INTO search(rowid, title, body) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertSearch.Close()

	for _, item := range items {
		status := item.Status
		if status == "" {
			status = "unread"
		}
		res, err := insertItem.Exec(item.FeedID, item.GUID, item.Title, item.Link,
			item.Date.UTC().Format(time.RFC3339Nano), item.Content, item.Author, item.Image, status)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// duplicate — already exists, skip FTS insert
			continue
		}
		id, _ := res.LastInsertId()
		if _, err := insertSearch.Exec(id, item.Title, stripHTML(item.Content)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) UpdateItemStatus(id int64, status string) error {
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

// GetUnreadCountsByFeed returns unread count and newest unread item date per feed in a single query.
func (s *Storage) GetUnreadCountsByFeed() ([]FeedUnreadStat, error) {
	rows, err := s.db.Query(`
		SELECT feed_id, COUNT(*), MAX(date)
		FROM items
		WHERE status = 'unread'
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
	_, err := s.db.Exec(`
		DELETE FROM items
		WHERE starred = 0
		  AND date < datetime('now', '-90 days')
		  AND id NOT IN (
		      SELECT id FROM items i2
		      WHERE i2.feed_id = items.feed_id
		      ORDER BY i2.date DESC
		      LIMIT 50
		  )`)
	return err
}
