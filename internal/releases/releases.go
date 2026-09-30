// Package releases retrieves and verifies official ZenNotes release binaries.
// It only stages candidates; the caller decides when to replace an installation.
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Product string

const (
	CLI    Product = "ZenNotes/tui"
	Server Product = "ZenNotes/znserver"
	apiURL         = "https://api.github.com"
)

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
}

func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Client's base URL and HTTP client are injectable for offline integration tests.
// The CLI always uses NewClient; it does not accept an alternate release source.
type Client struct {
	HTTP    *http.Client
	APIBase string
}

func NewClient() *Client {
	return &Client{APIBase: apiURL, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

func NormalizeVersion(version string) (string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("version must be a stable release such as 0.5.0")
	}
	for _, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' || strings.Trim(p, "0123456789") != "" {
			return "", fmt.Errorf("invalid release version %q", version)
		}
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return "", fmt.Errorf("invalid release version %q", version)
		}
	}
	return v, nil
}

func Newer(candidate, current string) bool {
	next, err := NormalizeVersion(candidate)
	if err != nil {
		return false
	}
	old, err := NormalizeVersion(current)
	if err != nil {
		return true
	}
	a, b := strings.Split(next, "."), strings.Split(old, ".")
	for i := range a {
		x, _ := strconv.ParseUint(a[i], 10, 64)
		y, _ := strconv.ParseUint(b[i], 10, 64)
		if x != y {
			return x > y
		}
	}
	return false
}

func (c *Client) Lookup(ctx context.Context, product Product, version string) (Release, error) {
	var release Release
	if product != CLI && product != Server {
		return release, fmt.Errorf("unknown release product %q", product)
	}
	endpoint := "/latest"
	if version != "" && version != "latest" {
		v, err := NormalizeVersion(version)
		if err != nil {
			return release, err
		}
		version = v
		endpoint = "/tags/v" + v
	}
	body, err := c.get(ctx, strings.TrimRight(c.APIBase, "/")+"/repos/"+string(product)+"/releases"+endpoint, 4<<20, true)
	if err != nil {
		return release, err
	}
	defer body.Close()
	if err := json.NewDecoder(body).Decode(&release); err != nil {
		return release, fmt.Errorf("read release metadata: %w", err)
	}
	if _, err := NormalizeVersion(release.Tag); err != nil || release.Draft || release.Prerelease {
		return release, fmt.Errorf("GitHub did not return a stable published release")
	}
	if version != "" && version != "latest" && version != release.Version() {
		return release, fmt.Errorf("requested version %s but received %s", version, release.Tag)
	}
	return release, nil
}

func (r Release) asset(name string) (Asset, error) {
	var found *Asset
	for _, a := range r.Assets {
		if a.Name == name {
			if found != nil {
				return Asset{}, fmt.Errorf("duplicate release asset %s", name)
			}
			copy := a
			found = &copy
		}
	}
	if found == nil {
		return Asset{}, fmt.Errorf("release %s has no asset %s", r.Tag, name)
	}
	return *found, nil
}

func assetName(product Product, version, goos, arch string) (string, error) {
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("no native release for %s/%s", goos, arch)
	}
	if product == Server {
		name := "zennotes-server-" + goos + "-" + arch
		if goos == "windows" {
			name += ".exe"
		}
		return name, nil
	}
	if product != CLI {
		return "", fmt.Errorf("unknown release product %q", product)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "zn_" + version + "_" + goos + "_" + arch + ext, nil
}

func (c *Client) assetURL(product Product, r Release, a Asset) (string, error) {
	if c.APIBase == apiURL {
		// Construct the official URL instead of trusting arbitrary URLs in metadata.
		if _, err := NormalizeVersion(r.Tag); err != nil || strings.ContainsAny(a.Name, "/\\") {
			return "", fmt.Errorf("invalid release asset metadata")
		}
		return "https://github.com/" + string(product) + "/releases/download/" + r.Tag + "/" + a.Name, nil
	}
	base, _ := url.Parse(c.APIBase)
	u, err := url.Parse(a.URL)
	if err != nil || base == nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil {
		return "", fmt.Errorf("release asset is outside the release source")
	}
	return a.URL, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}

func (c *Client) get(ctx context.Context, address string, limit int64, metadata bool) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ZenNotes-CLI")
	if metadata {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
		if req.URL.Host == "api.github.com" && req.URL.Scheme == "https" {
			if token := os.Getenv("GH_TOKEN"); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
	}
	client := *c.HTTP
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many release redirects")
		}
		if c.APIBase == apiURL {
			host := req.URL.Hostname()
			if req.URL.Scheme != "https" || (host != "github.com" && host != "api.github.com" && !strings.HasSuffix(host, ".githubusercontent.com")) {
				return fmt.Errorf("release redirected outside GitHub")
			}
		}
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download release: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("release download returned HTTP %d (check the version and GitHub rate limit)", res.StatusCode)
	}
	if res.ContentLength > limit {
		res.Body.Close()
		return nil, fmt.Errorf("release download exceeds %d bytes", limit)
	}
	return limitedBody{Reader: io.LimitReader(res.Body, limit+1), Closer: res.Body}, nil
}
