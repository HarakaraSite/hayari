package storage

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

func (s *Storage) GetOrCreateFolder(title string) (*Folder, error) {
	f, err := s.GetFolderByTitle(title)
	if err == nil {
		return f, nil
	}
	return s.CreateFolder(title)
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
