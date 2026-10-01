package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	testVersion = "0.6.2"
	testCommit  = "4C4038E7A0FF204868729F64C3E372E54FB50307"
	testKeyEnv  = "ZN_RELEASEMANIFEST_TEST_KEY"
	goodProbe   = `{"protocol":1,"version":"0.6.2"}`
)

func tarGz(t *testing.T, path string, members map[string]string) {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"LICENSE", "README.md", "zn"} {
		body, ok := members[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeDist mirrors a GoReleaser dist directory. Only the archive for the test
// machine holds a runnable zn, a shell script that prints probe.
func fakeDist(t *testing.T, probe string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --desktop-integration ]; then\n  printf '%s\\n' '" + probe + "'\n  exit 0\nfi\nexit 2\n"
	var sums strings.Builder
	for _, target := range targets {
		name := fmt.Sprintf("zn_%s_%s_%s.tar.gz", testVersion, target.goos, target.goarch)
		zn := "not runnable on this machine: " + target.key
		if target.goos == runtime.GOOS && target.goarch == runtime.GOARCH {
			zn = script
		}
		tarGz(t, filepath.Join(dir, name), map[string]string{"zn": zn, "README.md": "readme", "LICENSE": "license"})
		hash, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sums, "%s  %s\n", hash, name)
	}
	fmt.Fprintf(&sums, "%s  zn_%s_windows_amd64.zip\n", strings.Repeat("ab", 32), testVersion)
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func requireProbeHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake zn is a shell script")
	}
	for _, target := range targets {
		if target.goos == runtime.GOOS && target.goarch == runtime.GOARCH {
			return
		}
	}
	t.Skipf("no release archive targets %s/%s", runtime.GOOS, runtime.GOARCH)
}

func testKey(t *testing.T, seedByte byte) (ed25519.PrivateKey, string) {
	t.Helper()
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed), base64.StdEncoding.EncodeToString(seed)
}

func writePublicKey(t *testing.T, dir, name string, key ed25519.PrivateKey) string {
	t.Helper()
	path := filepath.Join(dir, name)
	pub := key.Public().(ed25519.PublicKey)
	if err := os.WriteFile(path, []byte("  "+base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildArgs(dist string, extra ...string) []string {
	return append([]string{"build", "-dist", dist, "-tag", "v" + testVersion, "-commit", testCommit, "-key-env", testKeyEnv}, extra...)
}

func TestBuildWritesSignedManifest(t *testing.T) {
	requireProbeHost(t)
	dist := fakeDist(t, goodProbe)
	key, seed := testKey(t, 7)
	t.Setenv(testKeyEnv, seed+"\n")
	var stdout, stderr bytes.Buffer
	if err := run(buildArgs(dist, "-require-signature"), &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(dist, "terminal-release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 1 || m.Release.Repository != "ZenNotes/tui" || m.Release.Protocol != 1 || m.Release.Version != testVersion || m.Release.Commit != strings.ToLower(testCommit) {
		t.Fatalf("manifest header wrong: %+v", m)
	}
	if len(m.Release.Artifacts) != len(targets) {
		t.Fatalf("artifacts: %+v", m.Release.Artifacts)
	}
	for _, target := range targets {
		name := fmt.Sprintf("zn_%s_%s_%s.tar.gz", testVersion, target.goos, target.goarch)
		hash, _ := fileSHA256(filepath.Join(dist, name))
		want := Artifact{URL: "https://github.com/ZenNotes/tui/releases/download/v0.6.2/" + name, SHA256: hash}
		if got := m.Release.Artifacts[target.key]; got != want {
			t.Errorf("%s: got %+v, want %+v", target.key, got, want)
		}
	}
	if again, _ := encodeJSON(m); !bytes.Equal(again, data) {
		t.Fatalf("manifest is not in canonical form:\n%s", data)
	}
	sig, err := os.ReadFile(filepath.Join(dist, "terminal-release.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	wantSig := fmt.Sprintf("{\n  \"keyId\": \"zn-release-1\",\n  \"algorithm\": \"ed25519\",\n  \"signature\": %q\n}\n", base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
	if string(sig) != wantSig {
		t.Fatalf("signature file:\n%s\nwant:\n%s", sig, wantSig)
	}
	pub := writePublicKey(t, t.TempDir(), "zn-release-1.pub", key)
	if err := run([]string{"verify", "-manifest", filepath.Join(dist, "terminal-release.json"), "-sig", filepath.Join(dist, "terminal-release.json.sig"), "-public-key-file", pub}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String()+stderr.String(), strings.TrimSpace(seed)) {
		t.Fatal("output revealed the signing seed")
	}
}

func TestBuildWithoutKey(t *testing.T) {
	requireProbeHost(t)
	dist := fakeDist(t, goodProbe)
	t.Setenv(testKeyEnv, "")
	var stdout, stderr bytes.Buffer
	if err := run(buildArgs(dist, "-require-signature"), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), testKeyEnv) {
		t.Fatalf("required signature without a key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dist, "terminal-release.json")); !os.IsNotExist(err) {
		t.Fatal("manifest written although the signature was required")
	}
	stale := filepath.Join(dist, "terminal-release.json.sig")
	os.WriteFile(stale, []byte("stale"), 0o644)
	if err := run(buildArgs(dist), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dist, "terminal-release.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a signature from an earlier run survived an unsigned build")
	}
	if !strings.Contains(stderr.String(), "warning:") {
		t.Fatalf("no warning for an unsigned manifest: %q", stderr.String())
	}
}

func TestBuildRejectsBadSeed(t *testing.T) {
	dist := fakeDist(t, goodProbe)
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	for _, value := range []string{"not base64 at all!", short, base64.StdEncoding.EncodeToString(make([]byte, 64))} {
		t.Setenv(testKeyEnv, value)
		var stdout, stderr bytes.Buffer
		err := run(buildArgs(dist), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), testKeyEnv) {
			t.Fatalf("%q accepted as a seed: %v", value, err)
		}
		if strings.Contains(err.Error()+stdout.String()+stderr.String(), value) {
			t.Fatal("error revealed the secret value")
		}
		if _, err := os.Stat(filepath.Join(dist, "terminal-release.json")); !os.IsNotExist(err) {
			t.Fatal("manifest written with an invalid key")
		}
	}
}

func TestBuildRejectsChecksumMismatch(t *testing.T) {
	dist := fakeDist(t, goodProbe)
	name := filepath.Join(dist, "zn_0.6.2_linux_arm64.tar.gz")
	tarGz(t, name, map[string]string{"zn": "swapped after checksums.txt was written"})
	err := build(buildConfig{Dist: dist, Tag: "v0.6.2", Commit: testCommit, Out: filepath.Join(dist, "m.json"), KeyID: defaultKeyID, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not match its checksums.txt entry") {
		t.Fatalf("tampered archive accepted: %v", err)
	}
}

func TestBuildRejectsMissingArchive(t *testing.T) {
	config := func(dist string) buildConfig {
		return buildConfig{Dist: dist, Tag: "v0.6.2", Commit: testCommit, Out: filepath.Join(dist, "m.json"), KeyID: defaultKeyID, GOOS: "linux", GOARCH: "amd64"}
	}
	dist := fakeDist(t, goodProbe)
	os.Remove(filepath.Join(dist, "zn_0.6.2_darwin_amd64.tar.gz"))
	if err := build(config(dist), &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "zn_0.6.2_darwin_amd64.tar.gz is missing") {
		t.Fatalf("missing archive accepted: %v", err)
	}
	dist = fakeDist(t, goodProbe)
	sums, _ := os.ReadFile(filepath.Join(dist, "checksums.txt"))
	var kept []string
	for _, line := range strings.Split(string(sums), "\n") {
		if !strings.Contains(line, "darwin_arm64") {
			kept = append(kept, line)
		}
	}
	os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte(strings.Join(kept, "\n")), 0o644)
	if err := build(config(dist), &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "does not list zn_0.6.2_darwin_arm64.tar.gz") {
		t.Fatalf("unlisted archive accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dist, "m.json")); !os.IsNotExist(err) {
		t.Fatal("manifest written for an incomplete release")
	}
}

func TestBuildRejectsHostWithoutArchive(t *testing.T) {
	dist := fakeDist(t, goodProbe)
	err := build(buildConfig{Dist: dist, Tag: "v0.6.2", Commit: testCommit, Out: filepath.Join(dist, "m.json"), KeyID: defaultKeyID, GOOS: "windows", GOARCH: "amd64"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "windows/amd64") {
		t.Fatalf("probe host without an archive: %v", err)
	}
}

func TestBuildRejectsBadProbe(t *testing.T) {
	requireProbeHost(t)
	for name, probe := range map[string]string{
		"version":         `{"protocol":1,"version":"0.6.1"}`,
		"prefixed":        `{"protocol":1,"version":"v0.6.2"}`,
		"quoted protocol": `{"protocol":"1","version":"0.6.2"}`,
		"zero protocol":   `{"protocol":0,"version":"0.6.2"}`,
		"fractional":      `{"protocol":1.5,"version":"0.6.2"}`,
		"not json":        `zn 0.6.2`,
	} {
		t.Run(name, func(t *testing.T) {
			dist := fakeDist(t, probe)
			t.Setenv(testKeyEnv, "")
			err := run(buildArgs(dist), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "--desktop-integration") {
				t.Fatalf("probe %s accepted: %v", probe, err)
			}
			if _, err := os.Stat(filepath.Join(dist, "terminal-release.json")); !os.IsNotExist(err) {
				t.Fatal("manifest written after a failed probe")
			}
		})
	}
}

func TestBuildRejectsInvalidTagAndCommit(t *testing.T) {
	dist := t.TempDir()
	for _, tc := range []struct{ tag, commit string }{
		{"0.6.2", testCommit},
		{"v", testCommit},
		{"v-0.6.2", testCommit},
		{"v0.6.2/../../x", testCommit},
		{"v0.6.2", "4c4038e"},
		{"v0.6.2", strings.Repeat("g", 40)},
	} {
		err := build(buildConfig{Dist: dist, Tag: tc.tag, Commit: tc.commit, KeyID: defaultKeyID, GOOS: "linux", GOARCH: "amd64"}, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil {
			t.Errorf("tag %q commit %q accepted", tc.tag, tc.commit)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	a, b := strings.Repeat("AB", 32), strings.Repeat("cd", 32)
	sums, err := parseChecksums([]byte(a + "  zn_0.6.2_linux_amd64.tar.gz\n\n" + b + "  zn_0.6.2_windows_amd64.zip\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sums["zn_0.6.2_linux_amd64.tar.gz"] != strings.ToLower(a) || sums["zn_0.6.2_windows_amd64.zip"] != b || len(sums) != 2 {
		t.Fatalf("parsed %v", sums)
	}
	for name, raw := range map[string]string{
		"one field":  a + "\n",
		"short hash": "abcd  zn_0.6.2_linux_amd64.tar.gz\n",
		"three":      a + "  zn  extra\n",
		"duplicate":  a + "  zn_0.6.2_linux_amd64.tar.gz\n" + b + "  zn_0.6.2_linux_amd64.tar.gz\n",
	} {
		if _, err := parseChecksums([]byte(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestExtractZnReadsOnlyTheTopLevelEntry(t *testing.T) {
	dir := t.TempDir()
	write := func(names ...string) string {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for _, name := range names {
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: 2, Typeflag: tar.TypeReg})
			tw.Write([]byte("hi"))
		}
		tw.Close()
		gz.Close()
		path := filepath.Join(dir, "a.tar.gz")
		os.WriteFile(path, b.Bytes(), 0o644)
		return path
	}
	out := filepath.Join(dir, "zn")
	if err := extractZn(write("../zn", "./zn", "bin/zn", "README.md"), out); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("archive without a top-level zn accepted: %v", err)
	}
	if err := extractZn(write("zn", "zn"), out); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("archive with two zn entries accepted: %v", err)
	}
	os.Remove(out)
	if err := extractZn(write("README.md", "zn"), out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "hi" {
		t.Fatalf("extracted %q", got)
	}
}

// The expected bytes are apps/desktop/terminal-release.json from the desktop
// repo at its v0.6.1 pin, which JSON.stringify(value, null, 2) + "\n" wrote.
func TestManifestMatchesDesktopPinBytes(t *testing.T) {
	const want = `{
  "schemaVersion": 1,
  "release": {
    "repository": "ZenNotes/tui",
    "protocol": 1,
    "version": "0.6.1",
    "commit": "4c4038e7a0ff204868729f64c3e372e54fb50307",
    "artifacts": {
      "darwin-arm64": {
        "url": "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_darwin_arm64.tar.gz",
        "sha256": "7223eea660a270cf01d2e1015940c131014b8497d0db6b24a0acbb9c546263d7"
      },
      "darwin-x64": {
        "url": "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_darwin_amd64.tar.gz",
        "sha256": "8f3b3190b243d18e84f45a8c4b03eff9c9fa63ea3f815df7a757d674a1f8c652"
      },
      "linux-arm64": {
        "url": "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_linux_arm64.tar.gz",
        "sha256": "f17202e930ac2916cee113d0c7dcacc5bfcbf04db601306a13cc8f757b7c576a"
      },
      "linux-x64": {
        "url": "https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_linux_amd64.tar.gz",
        "sha256": "9b082f58c2db052f44ede345539491f07578745971c847471e5cf1b1786ed256"
      }
    }
  }
}
`
	hashes := map[string]string{
		"darwin-arm64": "7223eea660a270cf01d2e1015940c131014b8497d0db6b24a0acbb9c546263d7",
		"darwin-x64":   "8f3b3190b243d18e84f45a8c4b03eff9c9fa63ea3f815df7a757d674a1f8c652",
		"linux-arm64":  "f17202e930ac2916cee113d0c7dcacc5bfcbf04db601306a13cc8f757b7c576a",
		"linux-x64":    "9b082f58c2db052f44ede345539491f07578745971c847471e5cf1b1786ed256",
	}
	m := Manifest{SchemaVersion: 1, Release: Release{Repository: repository, Protocol: 1, Version: "0.6.1", Commit: "4c4038e7a0ff204868729f64c3e372e54fb50307", Artifacts: map[string]Artifact{}}}
	// Insert in reverse to show the output order does not depend on it.
	for i := len(targets) - 1; i >= 0; i-- {
		target := targets[i]
		m.Release.Artifacts[target.key] = Artifact{
			URL:    fmt.Sprintf("https://github.com/ZenNotes/tui/releases/download/v0.6.1/zn_0.6.1_%s_%s.tar.gz", target.goos, target.goarch),
			SHA256: hashes[target.key],
		}
	}
	got, err := encodeJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("manifest bytes differ from the desktop pin:\n%s", got)
	}
}
