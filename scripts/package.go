//go:build ignore

// Run with go run scripts/package.go to build release archives and check native packages.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	buildversion "github.com/Deplexo/cli/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	targetOS := flag.String("os", runtime.GOOS, "Target OS")
	arch := flag.String("arch", runtime.GOARCH, "Target architecture")
	version := flag.String("version", "dev", "CLI version")
	commit := flag.String("commit", "unknown", "Source commit")
	out := flag.String("out", "dist", "Output directory")
	checkRelease := flag.Bool("check-release", false, "Check the tag, release manifest and source commit without building")
	verifyPath := flag.String("verify", "", "Verify an existing native archive without rebuilding")
	flag.Parse()
	if *checkRelease {
		return checkReleaseTag(*version)
	}
	if !regexp.MustCompile(`^(linux|darwin|windows)$`).MatchString(*targetOS) || !regexp.MustCompile(`^(amd64|arm64)$`).MatchString(*arch) {
		return errors.New("supported targets are linux, darwin, or windows with amd64 or arm64")
	}
	if (*version != "dev" && !buildversion.ValidTag(*version)) || !regexp.MustCompile(`^(unknown|[0-9a-f]{7,40})$`).MatchString(*commit) {
		return errors.New("invalid version or commit")
	}
	if *version != "dev" && !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(*commit) {
		return errors.New("release archives require the full source commit")
	}
	if *verifyPath != "" {
		binary := "deplexo"
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		return verify(*verifyPath, binary, *version, *commit)
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(*out, ".package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	binary := "deplexo"
	if *targetOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X main.version="+*version+" -X main.commit="+*commit, "-o", filepath.Join(work, binary), "./cmd/deplexo")
	command.Env = append(os.Environ(), "GOOS="+*targetOS, "GOARCH="+*arch, "CGO_ENABLED=0")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return err
	}
	notices, err := licenses()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "THIRD_PARTY_NOTICES.txt"), notices, 0644); err != nil {
		return err
	}
	files := map[string]string{binary: filepath.Join(work, binary), "README.md": "README.md", "LICENSE": "LICENSE", "THIRD_PARTY_NOTICES.txt": filepath.Join(work, "THIRD_PARTY_NOTICES.txt")}
	name := "deplexo_" + *version + "_" + *targetOS + "_" + *arch
	if *targetOS == "windows" {
		name += ".zip"
	} else {
		name += ".tar.gz"
	}
	archivePath := filepath.Join(*out, name)
	if err := archive(archivePath, files, binary); err != nil {
		return err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, f)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	checksum := hex.EncodeToString(hash.Sum(nil)) + "  " + name + "\n"
	if err := os.WriteFile(archivePath+".sha256", []byte(checksum), 0644); err != nil {
		return err
	}
	if *targetOS == runtime.GOOS && *arch == runtime.GOARCH {
		if err := verify(archivePath, binary, *version, *commit); err != nil {
			return err
		}
	}
	fmt.Println(archivePath)
	return nil
}

func licenses() ([]byte, error) {
	data, err := exec.Command("go", "list", "-m", "-json", "all").Output()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	for {
		var module struct {
			Path, Version, Dir string
			Main               bool
		}
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if module.Main {
			continue
		}
		var content []byte
		for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
			if b, err := os.ReadFile(filepath.Join(module.Dir, name)); err == nil {
				content = b
				break
			}
		}
		if len(content) == 0 {
			return nil, fmt.Errorf("license file missing for %s", module.Path)
		}
		fmt.Fprintf(&output, "%s %s\n\n%s\n\n", module.Path, module.Version, content)
	}
	return output.Bytes(), nil
}

func archive(path string, files map[string]string, binary string) (result error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	names := []string{binary, "LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt"}
	if strings.HasSuffix(path, ".zip") {
		writer := zip.NewWriter(f)
		for _, name := range names {
			data, err := os.ReadFile(files[name])
			if err != nil {
				return err
			}
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0644)
			if name == binary {
				header.SetMode(0755)
			}
			entry, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			if _, err := entry.Write(data); err != nil {
				return err
			}
		}
		return writer.Close()
	}
	gz := gzip.NewWriter(f)
	writer := tar.NewWriter(gz)
	for _, name := range names {
		source, err := os.Open(files[name])
		if err != nil {
			return err
		}
		info, err := source.Stat()
		if err != nil {
			source.Close()
			return err
		}
		mode := int64(0644)
		if name == binary {
			mode = 0755
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: info.Size(), ModTime: time.Unix(0, 0)}); err != nil {
			source.Close()
			return err
		}
		_, err = io.Copy(writer, source)
		closeErr := source.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return errors.Join(writer.Close(), gz.Close())
}

func verify(path, binary, version, commit string) error {
	directory, err := os.MkdirTemp("", "deplexo-package-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	destination := filepath.Join(directory, binary)
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	var copyErr error
	if strings.HasSuffix(path, ".zip") {
		reader, err := zip.OpenReader(path)
		if err != nil {
			f.Close()
			return err
		}
		defer reader.Close()
		found := false
		for _, entry := range reader.File {
			if entry.Name == binary {
				found = true
				source, err := entry.Open()
				if err != nil {
					f.Close()
					return err
				}
				_, copyErr = io.Copy(f, source)
				source.Close()
				break
			}
		}
		if !found {
			copyErr = errors.New("package does not contain the executable")
		}
	} else {
		source, err := os.Open(path)
		if err != nil {
			f.Close()
			return err
		}
		defer source.Close()
		gz, err := gzip.NewReader(source)
		if err != nil {
			f.Close()
			return err
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if err != nil {
				copyErr = err
				break
			}
			if header.Name == binary {
				_, copyErr = io.Copy(f, reader)
				break
			}
		}
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	data, err := exec.Command(destination, "version", "--json").Output()
	if err != nil {
		return fmt.Errorf("packaged executable failed: %w", err)
	}
	var result struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
	if json.Unmarshal(data, &result) != nil || result.Version != version || result.Commit != commit || result.OS != runtime.GOOS || result.Arch != runtime.GOARCH {
		return errors.New("packaged executable reports a different build identity")
	}
	if err := exec.Command(destination, "--help").Run(); err != nil {
		return fmt.Errorf("packaged help failed: %w", err)
	}
	return nil
}

func checkReleaseTag(tag string) error {
	if !buildversion.ValidTag(tag) {
		return fmt.Errorf("provide a release tag such as v0.1.0 or v0.1.0-rc.1")
	}
	data, err := os.ReadFile(".release-please-manifest.json")
	if err != nil {
		return err
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if "v"+manifest["."] != tag {
		return fmt.Errorf("release tag does not match the version in the release manifest")
	}
	for _, args := range [][]string{{"diff", "--quiet"}, {"diff", "--cached", "--quiet"}, {"merge-base", "--is-ancestor", "HEAD", "origin/main"}} {
		if err := exec.Command("git", args...).Run(); err != nil {
			return fmt.Errorf("release requires a clean tracked tree at a commit on origin/main")
		}
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	ref, err := exec.Command("git", "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}").Output()
	if err != nil || strings.TrimSpace(string(head)) != strings.TrimSpace(string(ref)) {
		return fmt.Errorf("release tag must point to the checked-out commit")
	}
	fmt.Println(tag)
	return nil
}
