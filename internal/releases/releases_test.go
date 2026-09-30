package releases

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func archive(t *testing.T, name, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tarw := tar.NewWriter(gz)
	if err := tarw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tarw.Write([]byte(body))
	tarw.Close()
	gz.Close()
	return b.Bytes()
}

func TestVerifiedReleaseStaging(t *testing.T) {
	for _, tc := range []struct {
		name, member string
		corrupt      bool
		wantError    string
	}{{"valid", "zn", false, ""}, {"tampered", "zn", true, "checksum"}, {"escape", "../zn", false, "binary"}} {
		t.Run(tc.name, func(t *testing.T) {
			body := archive(t, tc.member, "verified executable")
			hash := sha256.Sum256(body)
			asset := Asset{Name: "zn_0.5.0_linux_amd64.tar.gz", Size: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(hash[:])}
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/releases/") {
					asset.URL = srv.URL + "/archive"
					json.NewEncoder(w).Encode(Release{Tag: "v0.5.0", Assets: []Asset{asset}})
					return
				}
				if tc.corrupt {
					w.Write(bytes.Repeat([]byte{'x'}, len(body)))
				} else {
					w.Write(body)
				}
			}))
			defer srv.Close()
			client := &Client{HTTP: srv.Client(), APIBase: srv.URL}
			rel, err := client.Lookup(context.Background(), CLI, "0.5.0")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			path, err := client.Stage(context.Background(), CLI, rel, "linux", "amd64", dir)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("wanted %s error, got %v", tc.wantError, err)
				}
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatalf("failed staging left files: %v", entries)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "verified executable" {
				t.Fatalf("candidate: %s %v", got, err)
			}
		})
	}
}

func TestVersionSelection(t *testing.T) {
	for _, invalid := range []string{"../main", "v1", "1.2.3/path", "latest?draft=true", "-1.2.3"} {
		if _, err := NormalizeVersion(invalid); err == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
	if !Newer("0.10.0", "0.9.9") || Newer("0.5.0", "v0.5.0") || Newer("0.4.1", "0.5.0") {
		t.Fatal("version ordering is not semantic")
	}
}

func TestServerChecksumFileAndCancelledDownload(t *testing.T) {
	body := []byte("server executable")
	digest := sha256.Sum256(body)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sums":
			fmt.Fprintf(w, "%x  zennotes-server-linux-amd64\n", digest)
		case "/binary":
			w.Write(body)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := &Client{HTTP: srv.Client(), APIBase: srv.URL}
	rel := Release{Tag: "v2.56.0", Assets: []Asset{
		{Name: "zennotes-server-linux-amd64", Size: int64(len(body)), URL: srv.URL + "/binary"},
		{Name: "SHA256SUMS", URL: srv.URL + "/sums"},
	}}
	dir := t.TempDir()
	file, err := client.Stage(context.Background(), Server, rel, "linux", "amd64", dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(got, body) {
		t.Fatal("wrong server executable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Stage(ctx, Server, rel, "linux", "amd64", dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("cancellation left temporary files: %v", entries)
	}
}

func TestReleaseRejectsUnexpectedMetadataAndOversizeAssets(t *testing.T) {
	client := NewClient()
	r := Release{Tag: "v0.5.0", Assets: []Asset{{Name: "zn_0.5.0_linux_amd64.tar.gz", Size: maxBinaryBytes + 1}}}
	if _, err := client.Stage(context.Background(), CLI, r, "linux", "amd64", t.TempDir()); err == nil {
		t.Fatal("oversized asset accepted")
	}
	if _, err := client.assetURL(CLI, Release{Tag: "../../escape"}, Asset{Name: "zn"}); err == nil {
		t.Fatal("unsafe release tag accepted")
	}
	address, err := client.assetURL(CLI, r, Asset{Name: "checksums.txt", URL: "https://untrusted.example/binary"})
	if err != nil || address != "https://github.com/ZenNotes/tui/releases/download/v0.5.0/checksums.txt" {
		t.Fatalf("download not pinned to official release: %s %v", address, err)
	}
}

func TestInterruptedDownloadLeavesNoCandidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	go func() { <-started; cancel() }()
	digest := sha256.Sum256([]byte("not the full content"))
	r := Release{Tag: "v2.56.0", Assets: []Asset{{Name: "zennotes-server-linux-amd64", Size: 100, Digest: fmt.Sprintf("sha256:%x", digest), URL: srv.URL}}}
	dir := t.TempDir()
	c := &Client{HTTP: srv.Client(), APIBase: srv.URL}
	if _, err := c.Stage(ctx, Server, r, "linux", "amd64", dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("partial candidate retained: %v", entries)
	}
}

func TestWindowsArchiveExtractionRejectsSymlinksAndDuplicates(t *testing.T) {
	for _, mode := range []string{"valid", "symlink", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			var b bytes.Buffer
			zw := zip.NewWriter(&b)
			count := 1
			if mode == "duplicate" {
				count = 2
			}
			for n := 0; n < count; n++ {
				h := &zip.FileHeader{Name: "zn.exe", Method: zip.Deflate}
				h.SetMode(0o755)
				if mode == "symlink" {
					h.SetMode(os.ModeSymlink | 0o777)
				}
				w, err := zw.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("executable")); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			archive := t.TempDir() + "/release.zip"
			if err := os.WriteFile(archive, b.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			path, err := extractBinary(archive, "windows", dir)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %v", mode, err)
			}
			if err == nil {
				raw, _ := os.ReadFile(path)
				if string(raw) != "executable" {
					t.Fatal("wrong binary extracted")
				}
			} else {
				files, _ := os.ReadDir(dir)
				if len(files) != 0 {
					t.Fatal("failed extraction left a candidate")
				}
			}
		})
	}
}
