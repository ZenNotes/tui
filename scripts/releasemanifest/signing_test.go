package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func signedPair(t *testing.T, key ed25519.PrivateKey, keyID string) (dir, manifest, sig string) {
	t.Helper()
	dir = t.TempDir()
	manifest, sig = filepath.Join(dir, "terminal-release.json"), filepath.Join(dir, "terminal-release.json.sig")
	data, err := encodeJSON(Manifest{SchemaVersion: 1, Release: Release{Repository: repository, Protocol: 1, Version: "0.6.2", Commit: strings.Repeat("a", 40), Artifacts: map[string]Artifact{}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeJSON(sign(key, keyID, data))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(manifest, data, 0o644)
	os.WriteFile(sig, encoded, 0o644)
	return dir, manifest, sig
}

func TestSignThenVerify(t *testing.T) {
	key, _ := testKey(t, 1)
	other, _ := testKey(t, 2)
	dir, manifest, sig := signedPair(t, key, "zn-release-1")
	pub := writePublicKey(t, dir, "zn-release-1.pub", key)
	if err := verify(manifest, sig, pub); err != nil {
		t.Fatal(err)
	}

	t.Run("tampered manifest", func(t *testing.T) {
		data, _ := os.ReadFile(manifest)
		tampered := filepath.Join(t.TempDir(), "terminal-release.json")
		os.WriteFile(tampered, bytes.Replace(data, []byte(`"0.6.2"`), []byte(`"0.6.3"`), 1), 0o644)
		if err := verify(tampered, sig, pub); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("tampered manifest verified: %v", err)
		}
	})
	t.Run("wrong key id", func(t *testing.T) {
		renamed := writePublicKey(t, t.TempDir(), "zn-release-2.pub", key)
		if err := verify(manifest, sig, renamed); err == nil || !strings.Contains(err.Error(), `"zn-release-2"`) {
			t.Fatalf("signature accepted for another key id: %v", err)
		}
	})
	t.Run("wrong key", func(t *testing.T) {
		wrong := writePublicKey(t, t.TempDir(), "zn-release-1.pub", other)
		if err := verify(manifest, sig, wrong); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("signature accepted for another key: %v", err)
		}
	})
	t.Run("algorithm", func(t *testing.T) {
		raw, _ := os.ReadFile(sig)
		changed := filepath.Join(t.TempDir(), "terminal-release.json.sig")
		os.WriteFile(changed, bytes.Replace(raw, []byte(`"ed25519"`), []byte(`"rsa"`), 1), 0o644)
		if err := verify(manifest, changed, pub); err == nil || !strings.Contains(err.Error(), "algorithm") {
			t.Fatalf("non-ed25519 signature accepted: %v", err)
		}
	})
	t.Run("malformed public key", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "zn-release-1.pub")
		os.WriteFile(bad, []byte(base64.StdEncoding.EncodeToString(make([]byte, 16))), 0o644)
		if err := verify(manifest, sig, bad); err == nil || !strings.Contains(err.Error(), "public key") {
			t.Fatalf("short public key accepted: %v", err)
		}
	})
	t.Run("command exits with an error", func(t *testing.T) {
		wrong := writePublicKey(t, t.TempDir(), "zn-release-1.pub", other)
		if err := run([]string{"verify", "-manifest", manifest, "-sig", sig, "-public-key-file", wrong}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatal("verify command succeeded with the wrong key")
		}
	})
}

func TestKeygen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signing.key")
	var stdout bytes.Buffer
	if err := run([]string{"keygen", "-out", path}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\n") || strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("key file must be one base64 line: %q", raw)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(string(raw), "\n"))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("key file does not hold a seed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("key file mode: %v %v", info.Mode(), err)
		}
	}
	pub := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	if want := "keyId: zn-release-1\npublicKey: " + pub + "\n"; stdout.String() != want {
		t.Fatalf("stdout %q, want %q", stdout.String(), want)
	}
	if strings.Contains(stdout.String(), strings.TrimSpace(string(raw))) {
		t.Fatal("keygen printed the seed")
	}

	t.Setenv(testKeyEnv, string(raw))
	key, err := signingKey(testKeyEnv)
	if err != nil || !bytes.Equal(key.Seed(), seed) {
		t.Fatalf("build cannot read the keygen output: %v", err)
	}

	stdout.Reset()
	if err := run([]string{"keygen", "-out", path, "-key-id", "zn-release-2"}, &stdout, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("keygen overwrote an existing key: %v", err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, raw) || stdout.Len() != 0 {
		t.Fatal("a refused keygen changed the key file or printed a key")
	}
	if err := run([]string{"keygen", "-out", filepath.Join(t.TempDir(), "k"), "-key-id", "../escape"}, &stdout, &bytes.Buffer{}); err == nil {
		t.Fatal("key id that is not a file name accepted")
	}
}
