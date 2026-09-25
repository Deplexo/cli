package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Deplexo/cli/internal/api"
)

type Settings struct {
	Version int    `json:"version"`
	Origin  string `json:"origin"`
	Profile string `json:"profile"`
}
type Project struct {
	Version int    `json:"version"`
	App     string `json:"app"`
}

func read(path string, result any) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return errors.New("configuration must be a regular file, not a link or device")
	}
	// Nonblocking open prevents a concurrent FIFO replacement from hanging.
	// Root confinement also prevents replacement links escaping the directory.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return errors.New("configuration changed while opening it; try again")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(data) > 16384 {
		return errors.New("configuration file exceeds the size limit or could not be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(result); err != nil {
		return errors.New("configuration file is invalid; check its fields and format version")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("configuration file must contain a single JSON object")
	}
	return nil
}

func LoadSettings(directory string) (Settings, error) {
	settings := Settings{Version: 1}
	err := read(filepath.Join(directory, "settings.json"), &settings)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if settings.Version != 1 {
		return settings, errors.New("unsupported settings format version")
	}
	return settings, nil
}

func LoadProject(directory string) (Project, error) {
	var project Project
	if err := read(filepath.Join(directory, ".deplexo.json"), &project); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project, errors.New("select an app with --app or run `deplexo link --app <uuid>`")
		}
		return project, err
	}
	if project.Version != 1 || !api.ValidID(project.App) {
		return project, errors.New(".deplexo.json must contain version 1 and an app UUID")
	}
	return project, nil
}

func Link(directory, id string) error {
	if !api.ValidID(id) {
		return errors.New("app ID must be a UUID")
	}
	data, _ := json.MarshalIndent(Project{Version: 1, App: id}, "", "  ")
	// Exclusive creation preserves an existing project association, including symlinks.
	f, err := os.OpenFile(filepath.Join(directory, ".deplexo.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return errors.New(".deplexo.json already exists; run `deplexo unlink` before linking another app")
	}
	if err != nil {
		return errors.New("could not create .deplexo.json")
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("could not save .deplexo.json; check the file before retrying")
	}
	return nil
}

func Unlink(directory string) error {
	path := filepath.Join(directory, ".deplexo.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New(".deplexo.json must be a regular file")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("could not remove .deplexo.json")
	}
	return nil
}
