package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Deplexo/cli/internal/version"
)

const maxPackage = 64 << 20

func (c *Client) download(ctx context.Context, url string, out io.Writer, limit int64) error {
	resp, err := c.get(ctx, url)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("could not download the update; check your connection")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return errors.New("this release could not be downloaded for your platform")
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil || n > limit {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("update download failed or exceeded the size limit")
	}
	return nil
}

// Install replaces a regular executable after checking its archive and identity.
func (c *Client) Install(ctx context.Context, tag, current, target string) error {
	if !version.Newer(tag, current) || !filepath.IsAbs(target) {
		return errors.New("invalid update version or executable path")
	}
	directory, name := filepath.Dir(target), filepath.Base(target)
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("the installed executable is not a regular file; update it with its original installer")
	}
	lock, err := acquireLock(root)
	if err != nil {
		return errors.New("cannot lock this installation; another upgrade may be running, or the directory is not writable")
	}
	defer func() { _ = lock.Close() }()
	work, err := os.MkdirTemp("", "deplexo-upgrade-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	currentCopy := filepath.Join(work, "current.exe")
	old, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return err
	}
	oldInfo, statErr := old.Stat()
	if statErr != nil || !os.SameFile(before, oldInfo) {
		_ = old.Close()
		return errors.New("installed executable changed; retry the upgrade")
	}
	copyFile, err := os.OpenFile(currentCopy, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		_ = old.Close()
		return err
	}
	oldHash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(copyFile, oldHash), io.LimitReader(old, maxPackage+1))
	_ = old.Close()
	closeCopyErr := copyFile.Close()
	if copyErr != nil || closeCopyErr != nil || n > maxPackage {
		return errors.New("could not verify the installed executable")
	}
	if err := verifyBinary(ctx, currentCopy, current); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("this installed copy changed or cannot be verified; restart the CLI before upgrading")
	}
	ext, binary := ".tar.gz", "deplexo"
	if runtime.GOOS == "windows" {
		ext, binary = ".zip", "deplexo.exe"
	}
	archive := "deplexo_" + tag + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
	base := repository + "/releases/download/" + tag + "/"
	var checksums strings.Builder
	if err := c.download(ctx, base+"SHA256SUMS", &checksums, 1<<20); err != nil {
		return err
	}
	expected, count := "", 0
	for _, line := range strings.Split(checksums.String(), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == archive {
			expected, count = parts[0], count+1
		}
	}
	if _, err := hex.DecodeString(expected); err != nil || len(expected) != 64 || count != 1 {
		return errors.New("release checksum is missing or ambiguous")
	}
	packagePath := filepath.Join(work, archive)
	packed, err := os.OpenFile(packagePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	err = c.download(ctx, base+archive, io.MultiWriter(packed, hash), maxPackage)
	closeErr := packed.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected) {
		return errors.New("update checksum does not match; the installed binary was not changed")
	}
	verifiedPath := filepath.Join(work, binary)
	verified, err := os.OpenFile(verifiedPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0700)
	if err != nil {
		return err
	}
	err = extract(packagePath, binary, verified)
	verifiedCloseErr := verified.Close()
	if err != nil {
		return err
	}
	if verifiedCloseErr != nil {
		return verifiedCloseErr
	}
	if err := verifyBinary(ctx, verifiedPath, tag); err != nil {
		return err
	}
	stageName := ".deplexo-upgrade-" + rand.Text() + ".exe"
	stage, err := root.OpenFile(stageName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot write to the install directory; use its original installer or package manager")
	}
	defer func() { _ = root.Remove(stageName) }()
	source, err := os.Open(verifiedPath)
	if err != nil {
		_ = stage.Close()
		return err
	}
	_, err = io.Copy(stage, source)
	_ = source.Close()
	if err == nil {
		err = stage.Chmod(before.Mode().Perm())
	}
	if err == nil {
		err = stage.Sync()
	}
	closeErr = stage.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(before, after) {
		return errors.New("the installed executable changed during the upgrade; try again")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return replace(root, stageName, name, hex.EncodeToString(oldHash.Sum(nil)))
}

func extract(path, binary string, out io.Writer) error {
	found := 0
	copyBinary := func(source io.Reader, size int64) error {
		found++
		if found != 1 || size <= 0 || size > maxPackage {
			return errors.New("release archive must contain exactly one nonempty executable within the size limit")
		}
		n, err := io.Copy(out, io.LimitReader(source, size+1))
		if err != nil || n != size {
			return errors.New("release executable is incomplete")
		}
		return nil
	}
	if strings.HasSuffix(path, ".zip") {
		z, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer func() { _ = z.Close() }()
		for _, entry := range z.File {
			if entry.Name == binary {
				if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxPackage {
					return errors.New("release executable is not a regular file or exceeds the size limit")
				}
				r, err := entry.Open()
				if err != nil {
					return err
				}
				err = copyBinary(r, int64(entry.UncompressedSize64))
				_ = r.Close()
				if err != nil {
					return err
				}
			}
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		r := tar.NewReader(io.LimitReader(gz, maxPackage+1))
		for {
			entry, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if entry.Name == binary {
				if !entry.FileInfo().Mode().IsRegular() {
					return errors.New("release executable is not a regular file")
				}
				if err := copyBinary(r, entry.Size); err != nil {
					return err
				}
			}
		}
	}
	if found != 1 {
		return errors.New("release archive is missing the executable")
	}
	return nil
}

func verifyBinary(ctx context.Context, path, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data := limitedOutput{remaining: 4096}
	cmd := exec.CommandContext(ctx, path, "version", "--json")
	cmd.Stdout = &data
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("the downloaded executable cannot run; the installed binary was not changed")
	}
	var identity struct{ Version, OS, Arch string }
	if json.Unmarshal([]byte(data.String()), &identity) != nil || identity.Version != tag || identity.OS != runtime.GOOS || identity.Arch != runtime.GOARCH {
		return fmt.Errorf("downloaded executable does not match %s for this platform", tag)
	}
	return nil
}

type limitedOutput struct {
	strings.Builder
	remaining int
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errors.New("executable identity output exceeds the size limit")
	}
	w.remaining -= len(p)
	return w.Builder.Write(p)
}
