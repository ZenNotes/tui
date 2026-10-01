package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultKeyID = "zn-release-1"

// Desktop looks a signature's key up by id among the public keys it ships, and
// verify ties the id to a `<key id>.pub` file name, so ids stay file-name safe.
var keyIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

type Signature struct {
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func validKeyID(id string) error {
	if !keyIDPattern.MatchString(id) {
		return fmt.Errorf("key id %q must be letters, digits, dots, dashes or underscores", id)
	}
	return nil
}

// keygen keeps the seed out of stdout so that terminal logs and CI output can
// only ever show the public half.
func keygen(path, keyID string, stdout io.Writer) (err error) {
	if err := validKeyID(keyID); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists; refusing to overwrite a signing key", path)
	}
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.WriteString(f, base64.StdEncoding.EncodeToString(priv.Seed())+"\n"); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "keyId: %s\npublicKey: %s\n", keyID, base64.StdEncoding.EncodeToString(pub))
	return err
}

// signingKey returns nil without error when the variable is unset or empty;
// the caller decides whether an unsigned manifest is acceptable. Errors never
// quote the value, because it is a secret.
func signingKey(env string) (ed25519.PrivateKey, error) {
	value := strings.TrimSpace(os.Getenv(env))
	if env == "" || value == "" {
		return nil, nil
	}
	seed, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("$%s must be the base64 encoding of a %d-byte Ed25519 seed, as written by keygen", env, ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func sign(key ed25519.PrivateKey, keyID string, manifest []byte) Signature {
	return Signature{
		KeyID:     keyID,
		Algorithm: "ed25519",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest)),
	}
}

func verify(manifestPath, sigPath, publicKeyPath string) error {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	var s Signature
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("%s is not a signature file: %w", sigPath, err)
	}
	if s.Algorithm != "ed25519" {
		return fmt.Errorf("%s uses algorithm %q; only ed25519 is accepted", sigPath, s.Algorithm)
	}
	keyID := strings.TrimSuffix(filepath.Base(publicKeyPath), filepath.Ext(publicKeyPath))
	if s.KeyID != keyID {
		return fmt.Errorf("%s was signed with key %q, but %s holds key %q", sigPath, s.KeyID, publicKeyPath, keyID)
	}
	encoded, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return err
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%s must hold the base64 encoding of a %d-byte Ed25519 public key", publicKeyPath, ed25519.PublicKeySize)
	}
	signature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%s does not hold a %d-byte Ed25519 signature", sigPath, ed25519.SignatureSize)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifest, signature) {
		return fmt.Errorf("signature in %s does not match %s for key %s", sigPath, manifestPath, keyID)
	}
	return nil
}
