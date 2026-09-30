package releases

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxBinaryBytes int64 = 256 << 20

// Stage returns a verified executable in dir. The caller owns that file on
// success; every temporary file is removed on failure, including cancellation.
func (c *Client) Stage(ctx context.Context, product Product, release Release, goos, arch, dir string) (path string, err error) {
	version, err := NormalizeVersion(release.Tag)
	if err != nil {
		return "", err
	}
	name, err := assetName(product, version, goos, arch)
	if err != nil {
		return "", err
	}
	asset, err := release.asset(name)
	if err != nil {
		return "", err
	}
	if asset.Size <= 0 || asset.Size > maxBinaryBytes {
		return "", fmt.Errorf("invalid release asset size: %d", asset.Size)
	}
	want, err := c.checksum(ctx, product, release, asset)
	if err != nil {
		return "", err
	}
	address, err := c.assetURL(product, release, asset)
	if err != nil {
		return "", err
	}
	body, err := c.get(ctx, address, asset.Size, false)
	if err != nil {
		return "", err
	}
	defer body.Close()
	f, err := os.CreateTemp(dir, ".release-*")
	if err != nil {
		return "", err
	}
	defer func() {
		f.Close()
		if path != f.Name() || err != nil {
			os.Remove(f.Name())
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), body)
	if err != nil {
		return "", err
	}
	if n != asset.Size || hex.EncodeToString(hash.Sum(nil)) != want {
		return "", fmt.Errorf("checksum or size mismatch for %s; installation was not changed", asset.Name)
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if product == Server {
		if err := os.Chmod(f.Name(), 0o755); err != nil {
			return "", err
		}
		return f.Name(), nil
	}
	return extractBinary(f.Name(), goos, dir)
}

func (c *Client) checksum(ctx context.Context, product Product, release Release, asset Asset) (string, error) {
	if asset.Digest != "" {
		if strings.HasPrefix(asset.Digest, "sha256:") && validHash(strings.TrimPrefix(asset.Digest, "sha256:")) {
			return strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:")), nil
		}
		return "", fmt.Errorf("unsupported release checksum for %s", asset.Name)
	}
	name := "checksums.txt"
	if product == Server {
		name = "SHA256SUMS"
	}
	sums, err := release.asset(name)
	if err != nil {
		return "", fmt.Errorf("release has no SHA-256 verification: %w", err)
	}
	address, err := c.assetURL(product, release, sums)
	if err != nil {
		return "", err
	}
	body, err := c.get(ctx, address, 1<<20, false)
	if err != nil {
		return "", err
	}
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil || len(raw) > 1<<20 {
		return "", fmt.Errorf("could not read bounded checksum file")
	}
	if sums.Digest != "" {
		h := sha256.Sum256(raw)
		if sums.Digest != "sha256:"+hex.EncodeToString(h[:]) {
			return "", fmt.Errorf("checksum file digest mismatch")
		}
	}
	found := ""
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset.Name {
			if !validHash(fields[0]) || found != "" {
				return "", fmt.Errorf("invalid or duplicate checksum for %s", asset.Name)
			}
			found = strings.ToLower(fields[0])
		}
	}
	if found == "" {
		return "", fmt.Errorf("missing checksum for %s", asset.Name)
	}
	return found, nil
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

func extractBinary(archive, goos, dir string) (path string, err error) {
	name, pattern := "zn", ".zn-candidate-*"
	if goos == "windows" {
		name, pattern = "zn.exe", ".zn-candidate-*.exe"
	}
	out, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(out.Name())
		}
	}()
	found := false
	copyBinary := func(r io.Reader, size int64) error {
		if found || size <= 0 || size > maxBinaryBytes {
			return fmt.Errorf("invalid or duplicate binary in release archive")
		}
		found = true
		n, err := io.Copy(out, io.LimitReader(r, size+1))
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("incomplete binary in release archive")
		}
		return nil
	}
	if goos == "windows" {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			if !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(maxBinaryBytes) {
				return "", fmt.Errorf("invalid binary in release archive")
			}
			r, err := f.Open()
			if err != nil {
				return "", err
			}
			err = copyBinary(r, int64(f.UncompressedSize64))
			r.Close()
			if err != nil {
				return "", err
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return "", err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		tr := tar.NewReader(io.LimitReader(gz, maxBinaryBytes+4<<20))
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if header.Name == name {
				if header.Typeflag != tar.TypeReg {
					return "", fmt.Errorf("release binary must be a regular file")
				}
				if err := copyBinary(tr, header.Size); err != nil {
					return "", err
				}
			}
		}
	}
	if !found {
		return "", fmt.Errorf("release archive does not contain the %s binary", name)
	}
	if err := out.Chmod(0o755); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}
	return out.Name(), out.Close()
}
