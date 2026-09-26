package update

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsReplacementRollbackAndRecordedCleanup(t *testing.T) {
	for _, failure := range []bool{false, true} {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		target, stage := "deplexo.exe", "stage.exe"
		if err := os.WriteFile(filepath.Join(dir, target), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		if !failure {
			if err := os.WriteFile(filepath.Join(dir, stage), []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err = replace(root, stage, target, fmt.Sprintf("%x", sha256.Sum256([]byte("old"))))
		want := "new"
		if failure {
			want = "old"
		}
		if (err != nil) != failure {
			t.Fatalf("replacement: %v", err)
		}
		data, err := root.ReadFile(target)
		if err != nil || string(data) != want {
			t.Fatalf("rollback lost executable: %q %v", data, err)
		}
		_ = root.Close()
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	backup := ".deplexo.exe-previous-recorded.exe"
	receipt := ".deplexo.exe-backup.json"
	for _, changed := range []bool{true, false} {
		data := []byte("old")
		if changed {
			data = []byte("different")
		}
		if err := os.WriteFile(filepath.Join(dir, backup), data, 0600); err != nil {
			t.Fatal(err)
		}
		record, _ := json.Marshal(backupRecord{Name: backup, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("old")))})
		if err := os.WriteFile(filepath.Join(dir, receipt), record, 0600); err != nil {
			t.Fatal(err)
		}
		err := cleanupBackup(root, "deplexo.exe")
		if (err != nil) != changed {
			t.Fatalf("cleanup changed=%v: %v", changed, err)
		}
		_, statErr := root.Lstat(backup)
		if changed && statErr != nil {
			t.Fatal("changed backup was deleted")
		}
		if !changed && !os.IsNotExist(statErr) {
			t.Fatal("recorded backup was not cleaned")
		}
	}
}
