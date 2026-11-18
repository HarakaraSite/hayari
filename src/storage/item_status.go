package storage

import (
	"fmt"
	"slices"
	"strings"
)

// UpdateItemsState sets the requested fields for all matching IDs, including
// hidden items. An empty status or nil starred leaves that field unchanged.
func (s *Storage) UpdateItemsState(ids []int64, status string, starred *bool) error {
	if len(ids) == 0 || (status == "" && starred == nil) {
		return nil
	}

	var assignments, differences []string
	var values []any
	if status != "" {
		if status != "read" && status != "unread" {
			return fmt.Errorf("invalid item status: %q", status)
		}
		assignments = append(assignments, "status = ?")
		differences = append(differences, "status IS NOT ?")
		values = append(values, status)
	}
	if starred != nil {
		assignments = append(assignments, "starred = ?")
		differences = append(differences, "starred IS NOT ?")
		values = append(values, *starred)
	}
	query := `UPDATE items SET ` + strings.Join(assignments, ", ") + ` WHERE (` +
		strings.Join(differences, " OR ") + `) AND id IN (`

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const batchSize = 1000
	args := make([]any, 0, 2*len(values)+min(len(ids), batchSize))
	args = append(args, values...) // SET
	args = append(args, values...) // WHERE
	for batch := range slices.Chunk(ids, batchSize) {
		batchArgs := args
		for _, id := range batch {
			batchArgs = append(batchArgs, id)
		}
		placeholders := strings.Repeat("?, ", len(batch)-1) + "?"
		// #nosec G202 -- query contains fixed SQL fragments and generated placeholders; all values are bound.
		if _, err := tx.Exec(query+placeholders+")", batchArgs...); err != nil {
			return err
		}
	}
	return tx.Commit()
}
