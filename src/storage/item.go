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
	Image   *string   `json:"image"`
}

type ItemFilter struct {
	Status      string
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
		args = append(args, *f.After)
	}
	if f.Before != nil {
		clauses = append(clauses, "i.date <= ?")
		args = append(args, *f.Before)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

const itemSelect = `
	SELECT i.id, i.feed_id, i.guid, i.title, i.link, i.date, i.content, i.author, i.status, i.image
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
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Image); err != nil {
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
		SELECT id, feed_id, guid, title, link, date, content, author, status, image
		FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Link,
			&item.Date, &item.Content, &item.Author, &item.Status, &item.Image)
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

	insertSearch, err := tx.Prepare(`INSERT OR IGNORE INTO search(rowid, title, body) VALUES (?, ?, ?)`)
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
			item.Date, item.Content, item.Author, item.Image, status)
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
	// Use strftime to get unix seconds — avoids time.Time scanning issues with aggregate MAX().
	rows, err := s.db.Query(`
		SELECT feed_id, COUNT(*), CAST(strftime('%s', MAX(date)) AS INTEGER)
		FROM items
		WHERE status = 'unread'
		GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []FeedUnreadStat
	for rows.Next() {
		var feedID, count, unixSec int64
		if err := rows.Scan(&feedID, &count, &unixSec); err != nil {
			return nil, err
		}
		st := FeedUnreadStat{
			FeedID:     feedID,
			Count:      count,
			NewestDate: time.Unix(unixSec, 0),
		}
		stats = append(stats, st)
	}
	return stats, rows.Err()
}

// DeleteOldItems removes items older than 90 days per feed, keeping:
// - all starred items
// - at least 50 most recent items per feed
func (s *Storage) DeleteOldItems() error {
	_, err := s.db.Exec(`
		DELETE FROM items
		WHERE status != 'starred'
		  AND date < datetime('now', '-90 days')
		  AND id NOT IN (
		      SELECT id FROM items i2
		      WHERE i2.feed_id = items.feed_id
		      ORDER BY i2.date DESC
		      LIMIT 50
		  )`)
	return err
}
