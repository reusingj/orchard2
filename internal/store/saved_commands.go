package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// SavedCommand is a reusable catalog command or custom MDM command.
type SavedCommand struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	Command           string         `json:"command"`
	Params            map[string]any `json:"params"`
	QuicksendGroupIDs []int64        `json:"quicksend_group_ids"`
	CreatedAt         int64          `json:"created_at"`
	UpdatedAt         int64          `json:"updated_at"`
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
	c.QuicksendGroupIDs = []int64{}
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, c := range out {
		if err := s.loadSavedCommandQuicksendGroups(c); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetSavedCommand loads one saved command.
func (s *Store) GetSavedCommand(id int64) (*SavedCommand, error) {
	c, err := scanSavedCommand(s.db.QueryRow(`SELECT `+savedCommandCols+` FROM saved_commands WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if err := s.loadSavedCommandQuicksendGroups(c); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateSavedCommand inserts a saved command.
func (s *Store) CreateSavedCommand(c *SavedCommand) error {
	now := Now()
	params, err := json.Marshal(c.Params)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO saved_commands(name, kind, command, params, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		strings.TrimSpace(c.Name), c.Kind, c.Command, string(params), now, now)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	if err := replaceSavedCommandQuicksendGroups(tx, c.ID, c.QuicksendGroupIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.QuicksendGroupIDs == nil {
		c.QuicksendGroupIDs = []int64{}
	}
	c.CreatedAt, c.UpdatedAt = now, now
	return nil
}

// UpdateSavedCommand replaces a saved command.
func (s *Store) UpdateSavedCommand(c *SavedCommand) error {
	params, err := json.Marshal(c.Params)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c.Name = strings.TrimSpace(c.Name)
	c.UpdatedAt = Now()
	res, err := tx.Exec(`UPDATE saved_commands SET name=?, kind=?, command=?, params=?, updated_at=? WHERE id=?`,
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
	if err := replaceSavedCommandQuicksendGroups(tx, c.ID, c.QuicksendGroupIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) loadSavedCommandQuicksendGroups(c *SavedCommand) error {
	rows, err := s.db.Query(`SELECT group_id FROM saved_command_quicksend_groups WHERE saved_command_id=? ORDER BY group_id`, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	c.QuicksendGroupIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		c.QuicksendGroupIDs = append(c.QuicksendGroupIDs, id)
	}
	return rows.Err()
}

func replaceSavedCommandQuicksendGroups(tx *sql.Tx, commandID int64, groupIDs []int64) error {
	if _, err := tx.Exec(`DELETE FROM saved_command_quicksend_groups WHERE saved_command_id=?`, commandID); err != nil {
		return err
	}
	seen := make(map[int64]bool, len(groupIDs))
	for _, groupID := range groupIDs {
		if seen[groupID] {
			continue
		}
		seen[groupID] = true
		if _, err := tx.Exec(`INSERT INTO saved_command_quicksend_groups(saved_command_id, group_id) VALUES(?,?)`, commandID, groupID); err != nil {
			return err
		}
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
