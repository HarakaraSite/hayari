package storage

import (
	"slices"
	"strings"
	"time"
)

// ItemSummary contains the fields needed to display an article in the Web list.
type ItemSummary struct {
	ID                    int64     `json:"id"`
	FeedID                int64     `json:"feed_id"`
	Title                 string    `json:"title"`
	TranslatedTitle       *string   `json:"translated_title,omitempty"`
	TitleTranslationState string    `json:"title_translation_state"`
	Link                  string    `json:"link"`
	Date                  time.Time `json:"date"`
	StoredDate            string    `json:"-"`
	Author                string    `json:"author"`
	Status                string    `json:"status"`
	Starred               bool      `json:"starred"`
}

func (s *Storage) ListItemSummaries(filter ItemFilter, limit, offset int) ([]ItemSummary, error) {
	const columns = `i.id, i.feed_id, i.title, i.translated_title, i.title_translation_state, i.link, i.date, CAST(i.date AS TEXT), i.author, i.status, i.starred`
	query, args := itemListQuery(columns, filter, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ItemSummary
	for rows.Next() {
		var item ItemSummary
		if err := rows.Scan(&item.ID, &item.FeedID, &item.Title, &item.TranslatedTitle, &item.TitleTranslationState,
			&item.Link, &item.Date, &item.StoredDate, &item.Author, &item.Status, &item.Starred); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListItemIDs applies the same filter and pagination as ListItems.
func (s *Storage) ListItemIDs(filter ItemFilter, limit, offset int) ([]int64, error) {
	query, args := itemListQuery("i.id", filter, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetItemsByIDs returns visible items keyed by ID. Missing or unreadable rows
// are omitted, matching the reader endpoint's individual lookups.
func (s *Storage) GetItemsByIDs(ids []int64) (map[int64]*Item, error) {
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	// Keep each query bounded while fitting a full default FreshRSS page.
	const batchSize = 1000
	items := make(map[int64]*Item, len(uniqueIDs))
	for batch := range slices.Chunk(uniqueIDs, batchSize) {
		if err := s.getItemBatch(batch, items); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Storage) getItemBatch(ids []int64, items map[int64]*Item) error {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.Repeat("?, ", len(ids)-1) + "?"
	// #nosec G202 -- placeholders contain only generated question marks; IDs are bound in args.
	rows, err := s.db.Query(`SELECT `+itemDetailColumns+` FROM items WHERE hidden = 0 AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		item := new(Item)
		if err := scanItemDetail(rows, item); err != nil {
			continue
		}
		items[item.ID] = item
	}
	return rows.Err()
}
