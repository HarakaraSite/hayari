package storage

type Settings struct {
	RefreshInterval string `json:"refresh_interval"`
	Theme           string `json:"theme"`
	FontSize        string `json:"font_size"`
}

func (s *Storage) GetSetting(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT val FROM settings WHERE key = ?", key).Scan(&val)
	return val, err
}

func (s *Storage) SetSetting(key, val string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings (key, val) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET val = excluded.val`, key, val)
	return err
}

func (s *Storage) GetAllSettings() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, val FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		result[key] = val
	}
	return result, rows.Err()
}
