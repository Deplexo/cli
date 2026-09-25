package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectContractAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	id := "11111111-1111-4111-8111-111111111111"
	if err := Link(dir, id); err != nil {
		t.Fatal(err)
	}
	p, err := LoadProject(dir)
	if err != nil || p.App != id {
		t.Fatalf("project %+v %v", p, err)
	}
	if err := Link(dir, "22222222-2222-4222-8222-222222222222"); err == nil {
		t.Fatal("overwrote association")
	}
	if err := Unlink(dir); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{"version":1,"app":"` + id + `","origin":"https://evil.example"}`, `{"version":1,"app":"` + id + `","profile":"other"}`, `{"version":2,"app":"` + id + `"}`, `{"version":1,"app":"invalid"}`, `{"version":1,"app":"` + id + `"} {}`} {
		if err := os.WriteFile(filepath.Join(dir, ".deplexo.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadProject(dir); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
}
