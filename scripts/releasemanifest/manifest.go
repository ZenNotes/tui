package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	repository     = "ZenNotes/tui"
	maxBinaryBytes = 256 << 20
)

var (
	versionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+-]{0,99}$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

// The keys are the desktop's `${process.platform}-${process.arch}` names. Desktop
// only ships on these four, so a release that lacks one of them is incomplete.
var targets = []struct{ key, goos, goarch string }{
	{"darwin-arm64", "darwin", "arm64"},
	{"darwin-x64", "darwin", "amd64"},
	{"linux-arm64", "linux", "arm64"},
	{"linux-x64", "linux", "amd64"},
}

// Manifest is the schema of the desktop repo's apps/desktop/terminal-release.json,
// so a desktop release can adopt a verified asset as its bundled pin unchanged.
type Manifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	Release       Release `json:"release"`
}

type Release struct {
	Repository string `json:"repository"`
	Protocol   int    `json:"protocol"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	// encoding/json writes map keys in sorted order, which is also the order
	// of the desktop pin, so the bytes match JSON.stringify of the same value.
	Artifacts map[string]Artifact `json:"artifacts"`
}

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type buildConfig struct {
	Dist, Tag, Commit, Out, Sig, KeyEnv, KeyID string
	RequireSignature                           bool
	// The probe runs the archive built for the machine running this command.
	GOOS, GOARCH string
}

func build(c buildConfig, stdout, stderr io.Writer) error {
	version, ok := strings.CutPrefix(c.Tag, "v")
	if !ok || !versionPattern.MatchString(version) {
		return fmt.Errorf("tag %q must be v followed by a release version such as v0.6.2", c.Tag)
	}
	if !commitPattern.MatchString(c.Commit) {
		return fmt.Errorf("commit %q must be a 40-character hexadecimal SHA", c.Commit)
	}
	if err := validKeyID(c.KeyID); err != nil {
		return err
	}
	// The key is checked before anything is written so a malformed secret
	// cannot leave an unsigned manifest behind for the upload step.
	key, err := signingKey(c.KeyEnv)
	if err != nil {
		return err
	}
	if key == nil && c.RequireSignature {
		return fmt.Errorf("-require-signature is set but $%s holds no signing key", c.KeyEnv)
	}
	sums, err := readChecksums(filepath.Join(c.Dist, "checksums.txt"))
	if err != nil {
		return err
	}
	m := Manifest{SchemaVersion: 1, Release: Release{
		Repository: repository,
		Version:    version,
		Commit:     strings.ToLower(c.Commit),
		Artifacts:  map[string]Artifact{},
	}}
	host := ""
	for _, t := range targets {
		name := fmt.Sprintf("zn_%s_%s_%s.tar.gz", version, t.goos, t.goarch)
		want, ok := sums[name]
		if !ok {
			return fmt.Errorf("checksums.txt does not list %s", name)
		}
		got, err := fileSHA256(filepath.Join(c.Dist, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s is missing from %s", name, c.Dist)
			}
			return err
		}
		if got != want {
			return fmt.Errorf("%s does not match its checksums.txt entry (file %s, listed %s)", name, got, want)
		}
		m.Release.Artifacts[t.key] = Artifact{
			URL:    fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repository, c.Tag, name),
			SHA256: got,
		}
		if t.goos == c.GOOS && t.goarch == c.GOARCH {
			host = filepath.Join(c.Dist, name)
		}
	}
	if host == "" {
		return fmt.Errorf("no release archive runs on this machine (%s/%s); build the manifest on darwin or linux, amd64 or arm64", c.GOOS, c.GOARCH)
	}
	protocol, err := probeArchive(host, version)
	if err != nil {
		return err
	}
	m.Release.Protocol = protocol
	data, err := encodeJSON(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.Out, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (zn %s, protocol %d)\n", c.Out, version, protocol)
	if key == nil {
		// A signature left over from an earlier run would no longer match.
		if err := os.Remove(c.Sig); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintf(stderr, "warning: $%s is not set; %s was not signed and ZenNotes desktop will not install it\n", c.KeyEnv, c.Out)
		return nil
	}
	sig, err := encodeJSON(sign(key, c.KeyID, data))
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.Sig, sig, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "signed %s with %s\n", c.Sig, c.KeyID)
	return nil
}

// encodeJSON matches JavaScript's JSON.stringify(value, null, 2) + "\n" for
// the ASCII-only values the manifest and signature hold.
func encodeJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// readChecksums parses GoReleaser's checksums.txt. Any line it does not
// recognise is an error, so a format change cannot silently drop an archive.
func readChecksums(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseChecksums(raw)
}

func parseChecksums(raw []byte) (map[string]string, error) {
	sums := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !validHash(fields[0]) {
			return nil, fmt.Errorf("checksums.txt line %d is not `<sha256>  <filename>`", i+1)
		}
		if _, dup := sums[fields[1]]; dup {
			return nil, fmt.Errorf("checksums.txt lists %s more than once", fields[1])
		}
		sums[fields[1]] = strings.ToLower(fields[0])
	}
	return sums, nil
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// probeArchive asks the archived executable which integration protocol it
// speaks. Desktop refuses releases whose protocol it does not support, so the
// value must come from the binary itself rather than from this tool.
func probeArchive(archive, version string) (int, error) {
	dir, err := os.MkdirTemp("", "releasemanifest-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "zn")
	if err := extractZn(archive, binary); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--desktop-integration")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("zn --desktop-integration from %s failed: %w %s", filepath.Base(archive), err, strings.TrimSpace(errOut.String()))
	}
	var info struct {
		// Raw so a quoted or fractional protocol is rejected rather than coerced.
		Protocol json.RawMessage `json:"protocol"`
		Version  string          `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		return 0, fmt.Errorf("zn --desktop-integration printed invalid JSON: %w", err)
	}
	protocol, err := strconv.Atoi(string(info.Protocol))
	if err != nil || protocol < 1 {
		return 0, fmt.Errorf("zn --desktop-integration reported protocol %s; want an integer of at least 1", info.Protocol)
	}
	if info.Version != version {
		return 0, fmt.Errorf("zn --desktop-integration reported version %q, but the tag is for %s", info.Version, version)
	}
	return protocol, nil
}

// extractZn copies the archive's top-level zn entry to path. Member names are
// only compared, never used as paths, so a hostile archive cannot write
// anywhere else.
func extractZn(archive, path string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(archive), err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(archive), err)
		}
		if h.Name != "zn" {
			continue
		}
		if found || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxBinaryBytes {
			return fmt.Errorf("%s must contain exactly one regular zn file", filepath.Base(archive))
		}
		found = true
		if err := writeExecutable(path, tr, h.Size); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("%s does not contain a zn executable", filepath.Base(archive))
	}
	return nil
}

func writeExecutable(path string, r io.Reader, size int64) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("zn is truncated in the release archive")
	}
	if err == nil {
		// The umask may have narrowed the creation mode.
		err = out.Chmod(0o700)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
