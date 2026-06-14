package storage

import "time"

type Feed struct {
	ID            int64      `json:"id"`
	FolderID      *int64     `json:"folder_id"`
	Title         string     `json:"title"`
	FeedURL       string     `json:"feed_url"`
	SiteURL       string     `json:"site_url"`
	Icon          *string    `json:"icon"`
	LastRefreshed *time.Time `json:"last_refreshed"`
	LastError     *string    `json:"last_error,omitempty"`
	LastModified  *string    `json:"-"`
	ETag          *string    `json:"-"`
}

const feedColumns = `id, folder_id, title, feed_url, site_url, icon, last_refreshed, last_error, last_modified, etag`

func scanFeed(row interface{ Scan(...any) error }, f *Feed) error {
	return row.Scan(
		&f.ID, &f.FolderID, &f.Title, &f.FeedURL, &f.SiteURL, &f.Icon,
		&f.LastRefreshed, &f.LastError, &f.LastModified, &f.ETag,
	)
}

func (s *Storage) ListFeeds() ([]Feed, error) {
	rows, err := s.db.Query(`SELECT ` + feedColumns + ` FROM feeds ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := scanFeed(rows, &f); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func (s *Storage) GetFeed(id int64) (*Feed, error) {
	f := &Feed{}
	err := scanFeed(s.db.QueryRow(`SELECT `+feedColumns+` FROM feeds WHERE id = ?`, id), f)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Storage) GetFeedByURL(url string) (*Feed, error) {
	f := &Feed{}
	err := scanFeed(s.db.QueryRow(`SELECT `+feedColumns+` FROM feeds WHERE feed_url = ?`, url), f)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Storage) CreateFeed(feedURL string, folderID *int64) (*Feed, error) {
	res, err := s.db.Exec(`
		INSERT INTO feeds (feed_url, folder_id) VALUES (?, ?)
		ON CONFLICT(feed_url) DO NOTHING`, feedURL, folderID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return s.GetFeedByURL(feedURL)
	}
	id, _ := res.LastInsertId()
	return s.GetFeed(id)
}

func (s *Storage) UpdateFeed(id int64, title *string, folderID *int64) error {
	_, err := s.db.Exec(`
		UPDATE feeds SET
			title     = COALESCE(?, title),
			folder_id = COALESCE(?, folder_id)
		WHERE id = ?`, title, folderID, id)
	return err
}

// UpdateFeedFolder sets folder_id unconditionally; nil moves the feed out of
// any folder. Use this instead of UpdateFeed when "no folder" must be
// expressible (UpdateFeed treats nil as "keep current value").
func (s *Storage) UpdateFeedFolder(id int64, folderID *int64) error {
	_, err := s.db.Exec(`UPDATE feeds SET folder_id = ? WHERE id = ?`, folderID, id)
	return err
}

// UpdateFeedMeta fills in title and site_url from the parsed feed, but only
// for fields that are currently empty — user-set values are never overwritten.
func (s *Storage) UpdateFeedMeta(id int64, title, siteURL string) error {
	_, err := s.db.Exec(`
		UPDATE feeds SET
			title    = CASE WHEN title    = '' AND ? != '' THEN ? ELSE title    END,
			site_url = CASE WHEN site_url = '' AND ? != '' THEN ? ELSE site_url END
		WHERE id = ?`,
		title, title, siteURL, siteURL, id)
	return err
}

// UpdateFeedIcon stores the favicon only when none is set yet, so a slow
// favicon fetch can never clobber a newer value.
func (s *Storage) UpdateFeedIcon(id int64, icon *string) error {
	_, err := s.db.Exec(`UPDATE feeds SET icon = ? WHERE id = ? AND icon IS NULL`, icon, id)
	return err
}

func (s *Storage) UpdateFeedRefresh(id int64, lastModified, etag *string) error {
	now := time.Now()
	_, err := s.db.Exec(`
		UPDATE feeds SET last_refreshed = ?, last_modified = ?, etag = ?, last_error = NULL WHERE id = ?`,
		now, lastModified, etag, id)
	return err
}

func (s *Storage) UpdateFeedError(id int64, errMsg string) error {
	_, err := s.db.Exec(`UPDATE feeds SET last_error = ? WHERE id = ?`, errMsg, id)
	return err
}

// ListFeedErrors returns a map of feed_id → error message for feeds that have errors.
func (s *Storage) ListFeedErrors() (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT id, last_error FROM feeds WHERE last_error IS NOT NULL AND last_error != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]string)
	for rows.Next() {
		var id int64
		var msg string
		if err := rows.Scan(&id, &msg); err != nil {
			return nil, err
		}
		result[id] = msg
	}
	return result, rows.Err()
}

func (s *Storage) DeleteFeed(id int64) error {
	_, err := s.db.Exec("DELETE FROM feeds WHERE id = ?", id)
	return err
}
