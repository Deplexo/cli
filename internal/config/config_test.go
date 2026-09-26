package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

func TestRejectNonregularConfig(t *testing.T) {
	if path := os.Getenv("DEPLEXO_CONFIG_FIFO_TEST"); path != "" {
		if _, err := LoadProject(path); err == nil {
			t.Fatal("accepted FIFO configuration")
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".deplexo.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(dir); err == nil {
		t.Fatal("accepted directory configuration")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := exec.Command("mkfifo", path).Run(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRejectNonregularConfig$")
	cmd.Env = append(os.Environ(), "DEPLEXO_CONFIG_FIFO_TEST="+dir)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO read blocked or failed: %v %s", err, data)
	}
}
