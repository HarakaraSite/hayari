package storage

import (
	"slices"
	"strings"
)

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
