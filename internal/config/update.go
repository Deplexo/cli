package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type UpdateState struct {
	CheckedAt time.Time `json:"checked_at"`
	RemindAt  time.Time `json:"remind_at"`
	Latest    string    `json:"latest"`
}

func LoadUpdateState(directory string) (UpdateState, error) {
	var state UpdateState
	err := read(filepath.Join(directory, "update.json"), &state)
	return state, err
}

func SaveUpdateState(directory string, state UpdateState) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".update-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	err = json.NewEncoder(f).Encode(state)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(directory, "update.json"))
}
