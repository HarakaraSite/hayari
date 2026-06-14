package storage

type Filter struct {
	ID     int64  `json:"id"`
	FeedID *int64 `json:"feed_id"`
	Rule   string `json:"rule"`
	Action string `json:"action"` // "hide" or "mark_read"
	Field  string `json:"field"`  // "title", "content", "author"
}

func (s *Storage) ListFilters() ([]Filter, error) {
	rows, err := s.db.Query("SELECT id, feed_id, rule, action, field FROM filters")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var filters []Filter
	for rows.Next() {
		var f Filter
		if err := rows.Scan(&f.ID, &f.FeedID, &f.Rule, &f.Action, &f.Field); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, rows.Err()
}

func (s *Storage) CreateFilter(f Filter) (*Filter, error) {
	res, err := s.db.Exec(
		"INSERT INTO filters (feed_id, rule, action, field) VALUES (?, ?, ?, ?)",
		f.FeedID, f.Rule, f.Action, f.Field)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	f.ID = id
	return &f, nil
}

func (s *Storage) UpdateFilter(f Filter) error {
	_, err := s.db.Exec(
		"UPDATE filters SET feed_id = ?, rule = ?, action = ?, field = ? WHERE id = ?",
		f.FeedID, f.Rule, f.Action, f.Field, f.ID)
	return err
}

func (s *Storage) DeleteFilter(id int64) error {
	_, err := s.db.Exec("DELETE FROM filters WHERE id = ?", id)
	return err
}

func (s *Storage) ListFiltersForFeed(feedID int64) ([]Filter, error) {
	rows, err := s.db.Query(
		"SELECT id, feed_id, rule, action, field FROM filters WHERE feed_id IS NULL OR feed_id = ?",
		feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var filters []Filter
	for rows.Next() {
		var f Filter
		if err := rows.Scan(&f.ID, &f.FeedID, &f.Rule, &f.Action, &f.Field); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, rows.Err()
}
