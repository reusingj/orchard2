package store

import (
	"encoding/json"
	"strings"
)

// SavedCommand is a reusable catalog command or custom MDM command.
type SavedCommand struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Command   string         `json:"command"`
	Params    map[string]any `json:"params"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
}

func scanSavedCommand(r scanner) (*SavedCommand, error) {
	c := &SavedCommand{}
	var params string
	if err := r.Scan(&c.ID, &c.Name, &c.Kind, &c.Command, &params, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal([]byte(params), &c.Params); err != nil {
		return nil, err
	}
	if c.Params == nil {
		c.Params = map[string]any{}
	}
	return c, nil
}

const savedCommandCols = `id, name, kind, command, params, created_at, updated_at`

// ListSavedCommands returns saved commands ordered by name.
func (s *Store) ListSavedCommands() ([]*SavedCommand, error) {
	rows, err := s.db.Query(`SELECT ` + savedCommandCols + ` FROM saved_commands ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SavedCommand{}
	for rows.Next() {
		c, err := scanSavedCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetSavedCommand loads one saved command.
func (s *Store) GetSavedCommand(id int64) (*SavedCommand, error) {
	return scanSavedCommand(s.db.QueryRow(`SELECT `+savedCommandCols+` FROM saved_commands WHERE id=?`, id))
}

// CreateSavedCommand inserts a saved command.
func (s *Store) CreateSavedCommand(c *SavedCommand) error {
	now := Now()
	params, err := json.Marshal(c.Params)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`INSERT INTO saved_commands(name, kind, command, params, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		strings.TrimSpace(c.Name), c.Kind, c.Command, string(params), now, now)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	c.Name = strings.TrimSpace(c.Name)
	c.CreatedAt, c.UpdatedAt = now, now
	return nil
}

// UpdateSavedCommand replaces a saved command.
func (s *Store) UpdateSavedCommand(c *SavedCommand) error {
	params, err := json.Marshal(c.Params)
	if err != nil {
		return err
	}
	c.Name = strings.TrimSpace(c.Name)
	c.UpdatedAt = Now()
	res, err := s.db.Exec(`UPDATE saved_commands SET name=?, kind=?, command=?, params=?, updated_at=? WHERE id=?`,
		c.Name, c.Kind, c.Command, string(params), c.UpdatedAt, c.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSavedCommand removes a saved command.
func (s *Store) DeleteSavedCommand(id int64) error {
	res, err := s.db.Exec(`DELETE FROM saved_commands WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
