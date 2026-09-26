package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestLatestStableAndBetaPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, stable, betas, want string
		status                    int
	}{
		{"stable", `{"tag_name":"v1.0.0"}`, "", "v1.0.0", 200},
		{"betas", `{}`, `[{"tag_name":"v0.1.0-beta.2","prerelease":true},{"tag_name":"v0.1.0-beta.10","prerelease":true},{"tag_name":"v2.0.0-beta.1","prerelease":true,"draft":true},{"tag_name":"v3.0.0-rc.1","prerelease":true}]`, "v0.1.0-beta.10", 404},
		{"invalid-stable", `{"tag_name":"v1.0.0-beta.1"}`, "", "", 200},
		{"rate-limit", `{}`, "", "", 403},
		{"no-releases", `{}`, `[]`, "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.URL.Host != "api.github.com" {
					t.Fatal("update check sent credentials or used another host")
				}
				if strings.HasSuffix(r.URL.Path, "/latest") {
					return response(tc.status, tc.stable), nil
				}
				if tc.status != 404 {
					t.Fatal("fell back to betas after a non-404 failure")
				}
				return response(200, tc.betas), nil
			}))
			got, err := c.Latest(context.Background())
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
}

func TestBetaPaginationAndCancellation(t *testing.T) {
	c := New(transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/latest") {
			return response(404, "{}"), nil
		}
		if r.URL.Query().Get("page") == "1" {
			return response(200, "["+strings.Repeat(`{"tag_name":"v0.1.0-beta.1","prerelease":true},`, 99)+`{"tag_name":"v0.1.0-beta.1","prerelease":true}]`), nil
		}
		return response(200, `[{"tag_name":"v0.2.0-beta.1","prerelease":true}]`), nil
	}))
	if tag, err := c.Latest(context.Background()); err != nil || tag != "v0.2.0-beta.1" {
		t.Fatalf("pagination: %s %v", tag, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = New(transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() }))
	if _, err := c.Latest(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if err := c.download(ctx, repository, &bytes.Buffer{}, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost download cancellation: %v", err)
	}
}

func fixtureBinary(t *testing.T, dir, version string) []byte {
	t.Helper()
	source := filepath.Join(dir, "main.go")
	code := `package main
import("fmt";"runtime";"os")
var release string
func main(){if os.Getenv("DEPLEXO_TEST_NOISY")=="1"{for{fmt.Print("too much output")}};fmt.Printf("{\"version\":%q,\"os\":%q,\"arch\":%q}",release,runtime.GOOS,runtime.GOARCH)}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture.exe")
	cmd := exec.Command("go", "build", "-ldflags=-X main.release="+version, "-o", path, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func packageFixture(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	name, ext := "deplexo", ".tar.gz"
	if runtime.GOOS == "windows" {
		name, ext = "deplexo.exe", ".zip"
		z := zip.NewWriter(&buf)
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0755}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes(), "deplexo_v0.2.0_" + runtime.GOOS + "_" + runtime.GOARCH + ext
}

func TestInstallVerifiesBeforeReplacement(t *testing.T) {
	fixtureDir := t.TempDir()
	old := fixtureBinary(t, fixtureDir, "v0.1.0")
	next := fixtureBinary(t, fixtureDir, "v0.2.0")
	archive, name := packageFixture(t, next)
	for _, scenario := range []string{"success", "checksum", "duplicate-checksum", "locked", "symlink-lock", "directory-moved", "stale-process"} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "bin")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "deplexo.exe")
			if err := os.WriteFile(target, old, 0755); err != nil {
				t.Fatal(err)
			}
			if scenario == "locked" {
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = root.Close() }()
				lock, err := acquireLock(root)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
			}
			if scenario == "symlink-lock" {
				if err := os.Symlink(target, filepath.Join(dir, ".deplexo-update.lock")); err != nil {
					t.Skip("symlinks unavailable")
				}
			}
			checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), name)
			if scenario == "checksum" {
				checksum = strings.Repeat("0", 64) + "  " + name + "\n"
			}
			if scenario == "duplicate-checksum" {
				checksum += checksum
			}
			moved := false
			c := New(transportFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
					if scenario == "directory-moved" && !moved {
						if err := os.Rename(dir, filepath.Join(base, "moved")); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(dir, 0700); err != nil {
							t.Fatal(err)
						}
						moved = true
					}
					return response(200, checksum), nil
				}
				return response(200, string(archive)), nil
			}))
			current := "v0.1.0"
			if scenario == "stale-process" {
				current = "v0.0.9"
			}
			err := c.Install(context.Background(), "v0.2.0", current, target)
			want := old
			if scenario == "success" || scenario == "directory-moved" {
				want = next
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe upgrade succeeded")
			}
			if moved {
				target = filepath.Join(base, "moved", "deplexo.exe")
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("wrote through replaced directory path")
				}
			}
			got, readErr := os.ReadFile(target)
			if readErr != nil || !bytes.Equal(got, want) {
				t.Fatalf("incorrect target after %v: %v", err, readErr)
			}
		})
	}
	path := filepath.Join(fixtureDir, "noisy.exe")
	if err := os.WriteFile(path, next, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPLEXO_TEST_NOISY", "1")
	if err := verifyBinary(context.Background(), path, "v0.2.0"); err == nil {
		t.Fatal("unbounded identity output accepted")
	}
}

func TestRejectMalformedArchives(t *testing.T) {
	for _, scenario := range []string{"duplicate", "symlink", "oversized", "truncated", "nested"} {
		t.Run(scenario, func(t *testing.T) {
			var packed bytes.Buffer
			gz := gzip.NewWriter(&packed)
			tw := tar.NewWriter(gz)
			header := tar.Header{Name: "deplexo", Size: 1, Mode: 0755}
			switch scenario {
			case "symlink":
				header.Typeflag = tar.TypeSymlink
				header.Linkname = "/outside"
				header.Size = 0
			case "oversized":
				header.Size = maxPackage + 1
			case "truncated":
				header.Size = 10
			case "nested":
				header.Name = "nested/deplexo"
			}
			if err := tw.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
			if header.Size > 0 {
				if _, err := tw.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "duplicate" {
				if err := tw.WriteHeader(&header); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			// Oversized and truncated cases intentionally omit the declared contents.
			_ = tw.Close()
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bad.tar.gz")
			if err := os.WriteFile(path, packed.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := extract(path, "deplexo", &bytes.Buffer{}); err == nil {
				t.Fatal("malformed archive accepted")
			}
		})
	}
}
