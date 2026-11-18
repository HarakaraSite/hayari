package storage

import "database/sql"

type Folder struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	IsExpanded bool   `json:"is_expanded"`
}

func (s *Storage) ListFolders() ([]Folder, error) {
	rows, err := s.db.Query("SELECT id, title, is_expanded FROM folders ORDER BY title")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Title, &f.IsExpanded); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func (s *Storage) GetFolder(id int64) (*Folder, error) {
	f := &Folder{}
	err := s.db.QueryRow("SELECT id, title, is_expanded FROM folders WHERE id = ?", id).
		Scan(&f.ID, &f.Title, &f.IsExpanded)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Storage) GetFolderByTitle(title string) (*Folder, error) {
	f := &Folder{}
	err := s.db.QueryRow("SELECT id, title, is_expanded FROM folders WHERE title = ?", title).
		Scan(&f.ID, &f.Title, &f.IsExpanded)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Storage) GetUniqueFolderIDByTitle(title string) (int64, error) {
	var id int64
	err := s.db.QueryRow("SELECT MIN(id) FROM folders WHERE title = ? HAVING COUNT(*) = 1", title).Scan(&id)
	return id, err
}

func (s *Storage) GetOrCreateFolder(title string) (*Folder, error) {
	if _, err := s.db.Exec(`
		INSERT INTO folders (title)
		SELECT ? WHERE NOT EXISTS (SELECT 1 FROM folders WHERE title = ?)`, title, title); err != nil {
		return nil, err
	}
	return s.GetFolderByTitle(title)
}

func (s *Storage) CreateFolder(title string) (*Folder, error) {
	res, err := s.db.Exec("INSERT INTO folders (title) VALUES (?)", title)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Folder{ID: id, Title: title}, nil
}

func (s *Storage) UpdateFolder(id int64, title string) error {
	_, err := s.db.Exec("UPDATE folders SET title = ? WHERE id = ?", title, id)
	return err
}

func (s *Storage) UpdateFolderExpanded(id int64, isExpanded bool) error {
	_, err := s.db.Exec("UPDATE folders SET is_expanded = ? WHERE id = ?", isExpanded, id)
	return err
}

func (s *Storage) DeleteFolder(id int64) error {
	_, err := s.db.Exec("DELETE FROM folders WHERE id = ?", id)
	return err
}

// RenameFolderByTitle identifies the folder by its Reader API label and avoids
// assigning the same label to another folder.
func (s *Storage) RenameFolderByTitle(oldTitle, title string) error {
	result, err := s.db.Exec(`
		UPDATE folders AS folder SET title = ?
		WHERE id = (SELECT MIN(id) FROM folders WHERE title = ? HAVING COUNT(*) = 1)
		AND NOT EXISTS (SELECT 1 FROM folders WHERE title = ? AND id <> folder.id)`, title, oldTitle, title)
	return requireFolderChange(result, err)
}

// DeleteFolderByTitle removes the label. The foreign key moves subscriptions
// into the uncategorized list while keeping their articles.
func (s *Storage) DeleteFolderByTitle(title string) error {
	result, err := s.db.Exec(`
		DELETE FROM folders
		WHERE id = (SELECT MIN(id) FROM folders WHERE title = ? HAVING COUNT(*) = 1)`, title)
	return requireFolderChange(result, err)
}

func requireFolderChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}
