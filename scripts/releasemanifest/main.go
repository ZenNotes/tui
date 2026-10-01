// Command releasemanifest writes and signs terminal-release.json, the release
// asset ZenNotes desktop reads to update the CLI it manages. The release
// workflow runs it after GoReleaser; CONTRIBUTING.md describes the signing key.
//
//	go run ./scripts/releasemanifest keygen -out <path>
//	go run ./scripts/releasemanifest build -dist dist -tag vX.Y.Z -commit <sha> -require-signature
//	go run ./scripts/releasemanifest verify -manifest <path> -sig <path> -public-key-file <path>
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const usage = `usage: releasemanifest <command> [flags]

  keygen  -out <path> [-key-id zn-release-1]
  build   -dist <dir> -tag vX.Y.Z -commit <40 hex> [-out <path>] [-sig <path>]
          [-key-env ZN_RELEASE_SIGNING_KEY] [-key-id zn-release-1] [-require-signature]
  verify  -manifest <path> -sig <path> -public-key-file <path>`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "releasemanifest: "+err.Error())
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	parse := func() error {
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return fmt.Errorf("%s: unexpected argument %q", args[0], fs.Arg(0))
		}
		return nil
	}
	switch args[0] {
	case "keygen":
		out := fs.String("out", "", "file that receives the base64 seed (created with mode 0600)")
		keyID := fs.String("key-id", defaultKeyID, "key id printed with the public key")
		if err := parse(); err != nil {
			return err
		}
		if *out == "" {
			return errors.New("keygen: -out is required")
		}
		return keygen(*out, *keyID, stdout)
	case "build":
		c := buildConfig{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
		fs.StringVar(&c.Dist, "dist", "", "GoReleaser dist directory")
		fs.StringVar(&c.Tag, "tag", "", "release tag, vX.Y.Z")
		fs.StringVar(&c.Commit, "commit", "", "40-character source commit")
		fs.StringVar(&c.Out, "out", "", "manifest path (default <dist>/terminal-release.json)")
		fs.StringVar(&c.Sig, "sig", "", "signature path (default <dist>/terminal-release.json.sig)")
		fs.StringVar(&c.KeyEnv, "key-env", "ZN_RELEASE_SIGNING_KEY", "environment variable holding the base64 Ed25519 seed")
		fs.StringVar(&c.KeyID, "key-id", defaultKeyID, "key id recorded in the signature")
		fs.BoolVar(&c.RequireSignature, "require-signature", false, "fail when no signing key is set")
		if err := parse(); err != nil {
			return err
		}
		if c.Dist == "" || c.Tag == "" || c.Commit == "" {
			return errors.New("build: -dist, -tag and -commit are required")
		}
		if c.Out == "" {
			c.Out = filepath.Join(c.Dist, "terminal-release.json")
		}
		if c.Sig == "" {
			c.Sig = filepath.Join(c.Dist, "terminal-release.json.sig")
		}
		return build(c, stdout, stderr)
	case "verify":
		manifest := fs.String("manifest", "", "terminal-release.json")
		sig := fs.String("sig", "", "terminal-release.json.sig")
		pub := fs.String("public-key-file", "", "<key id>.pub holding the base64 public key")
		if err := parse(); err != nil {
			return err
		}
		if *manifest == "" || *sig == "" || *pub == "" {
			return errors.New("verify: -manifest, -sig and -public-key-file are required")
		}
		if err := verify(*manifest, *sig, *pub); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "verified %s with %s\n", *manifest, *pub)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}
