package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	TitleTranslationPending    = "pending"
	TitleTranslationProcessing = "processing"
	TitleTranslationTranslated = "translated"
	TitleTranslationSkipped    = "skipped"
	TitleTranslationFailed     = "failed"
)

// ClaimTitleTranslations reserves at most limit eligible unread articles in one transaction.
func (s *Storage) ClaimTitleTranslations(feedID int64, limit int) (string, []Item, error) {
	if limit < 1 || limit > 50 {
		return "", nil, fmt.Errorf("invalid title translation limit: %d", limit)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM items WHERE feed_id = ? AND title_translation_state = ?`, feedID, TitleTranslationProcessing).Scan(&active); err != nil {
		return "", nil, err
	}
	if active != 0 {
		return "", nil, nil
	}
	rows, err := tx.Query(`SELECT id, feed_id, guid, title, translated_title, title_translation_state, link, date, content, author, status, starred, image, hidden FROM items WHERE feed_id = ? AND status = 'unread' AND hidden = 0 AND title_translation_state IN (?, ?) ORDER BY date DESC, id DESC LIMIT ?`, feedID, TitleTranslationPending, TitleTranslationFailed, limit)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.TranslatedTitle, &item.TitleTranslationState, &item.Link, &item.Date, &item.Content, &item.Author, &item.Status, &item.Starred, &item.Image, &item.Hidden); err != nil {
			return "", nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(items) == 0 {
		return "", nil, nil
	}
	claim := uuid.NewString()
	ids := make([]string, len(items))
	args := []interface{}{TitleTranslationProcessing, claim}
	for i, item := range items {
		ids[i] = "?"
		args = append(args, item.ID)
	}
	if _, err := tx.Exec(`UPDATE items SET title_translation_state = ?, title_translation_claim = ? WHERE id IN (`+strings.Join(ids, ",")+`)`, args...); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	return claim, items, nil
}
func (s *Storage) TranslationItemStillEligible(id int64, claim string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE id = ? AND title_translation_claim = ? AND title_translation_state = ? AND status = 'unread' AND hidden = 0`, id, claim, TitleTranslationProcessing).Scan(&n)
	return n == 1, err
}
func (s *Storage) SetTitleTranslationResult(id int64, claim, state string, title *string) error {
	if state != TitleTranslationTranslated && state != TitleTranslationSkipped && state != TitleTranslationFailed && state != TitleTranslationPending {
		return fmt.Errorf("invalid title translation state: %q", state)
	}
	if state == TitleTranslationTranslated && (title == nil || *title == "") {
		return fmt.Errorf("translated title required")
	}
	if state != TitleTranslationTranslated {
		title = nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var original string
	err = tx.QueryRow(`SELECT title FROM items WHERE id = ? AND title_translation_claim = ? AND title_translation_state = ?`, id, claim, TitleTranslationProcessing).Scan(&original)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE items SET title_translation_state = ?, title_translation_claim = NULL, translated_title = ? WHERE id = ?`, state, title, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM search WHERE rowid = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO search(rowid, title, translated_title) VALUES (?, ?, COALESCE(?, ''))`, id, original, title); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Storage) ReleaseTitleTranslationClaim(claim string) error {
	_, err := s.db.Exec(`UPDATE items SET title_translation_state = ?, title_translation_claim = NULL WHERE title_translation_claim = ? AND title_translation_state = ?`, TitleTranslationPending, claim, TitleTranslationProcessing)
	return err
}
func (s *Storage) ReleaseProcessingTitleTranslations() error {
	_, err := s.db.Exec(`UPDATE items SET title_translation_state = ?, title_translation_claim = NULL WHERE title_translation_state = ?`, TitleTranslationPending, TitleTranslationProcessing)
	return err
}
