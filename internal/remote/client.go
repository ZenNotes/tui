// Package remote talks to a self-hosted ZenNotes server over its HTTP API,
// the same routes the desktop and web clients use.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

var schemeRe = regexp.MustCompile(`(?i)^https?://`)

// NormalizeBaseURL turns `localhost:7878` into `http://localhost:7878` and
// drops any trailing slash.
func NormalizeBaseURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if !schemeRe.MatchString(trimmed) {
		trimmed = "http://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

// RequestError is a non-2xx answer, with the status attached so a 404
// (absent file) can be told from a real failure.
type RequestError struct {
	Status  int
	Message string
}

func (e *RequestError) Error() string { return e.Message }

// StatusOf is the HTTP status behind an error, or 0 when it carries none.
func StatusOf(err error) int {
	var re *RequestError
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

// ConnectionErrorMessage phrases a transport-level failure. On macOS a
// server on the local network needs the Local Network permission, and
// without it the attempt looks exactly like a server that is down.
func ConnectionErrorMessage(baseURL string, err error) string {
	detail := ""
	if err != nil && err.Error() != "" {
		detail = " Could not reach the server: " + err.Error() + "."
	}
	hint := ""
	if runtime.GOOS == "darwin" {
		hint = " If the server is on your local network, check that ZenNotes is allowed under System Settings → Privacy & Security → Local Network."
	}
	return fmt.Sprintf("Could not connect to the ZenNotes server at %s. Make sure the server is running and the URL is correct.%s%s", baseURL, detail, hint)
}

func requestErrorMessage(baseURL, path string, status int, statusText, text string) string {
	if status == 401 {
		return fmt.Sprintf("The ZenNotes server rejected the connection. Check the auth token for %s and try again.", baseURL)
	}
	msg := fmt.Sprintf("Remote server request failed (%d %s) for %s", status, statusText, path)
	if text != "" {
		msg += ": " + text
	}
	return msg
}

// Client is a fetch-only client for one server.
type Client struct {
	BaseURL   string
	AuthToken string
	http      *http.Client
}

// NewClient binds a client to a server and an optional token.
func NewClient(baseURL, authToken string) *Client {
	return &Client{
		BaseURL:   NormalizeBaseURL(baseURL),
		AuthToken: authToken,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New(ConnectionErrorMessage(c.BaseURL, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return &RequestError{
			Status:  resp.StatusCode,
			Message: requestErrorMessage(c.BaseURL, path, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(text))),
		}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// GetRaw performs a GET and returns the body untouched.
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New(ConnectionErrorMessage(c.BaseURL, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, &RequestError{Status: resp.StatusCode, Message: requestErrorMessage(c.BaseURL, path, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(text)))}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// ReadComments loads a note's comment sidecar from the server.
func (c *Client) ReadComments(ctx context.Context, rel string) ([]vault.NoteComment, error) {
	var out []vault.NoteComment
	if err := c.Get(ctx, "/api/comments/read?path="+url.QueryEscape(rel), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.NoteComment{}
	}
	return out, nil
}

// WriteComments replaces a note's comments on the server.
func (c *Client) WriteComments(ctx context.Context, rel string, comments []vault.NoteComment) ([]vault.NoteComment, error) {
	var out []vault.NoteComment
	if err := c.Post(ctx, "/api/comments/write", map[string]any{"path": rel, "comments": comments}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.NoteComment{}
	}
	return out, nil
}

// ReadAsset fetches an attachment's bytes.
func (c *Client) ReadAsset(ctx context.Context, rel string) ([]byte, error) {
	return c.GetRaw(ctx, "/api/assets/raw?path="+url.QueryEscape(rel))
}

// Get performs a JSON GET.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post performs a JSON POST; a nil body sends `{}`.
func (c *Client) Post(ctx context.Context, path string, body any, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	return c.do(ctx, http.MethodPost, path, body, out)
}

func withLinks(notes []vault.NoteMeta) []vault.NoteMeta {
	for i := range notes {
		if notes[i].Link == "" {
			notes[i].Link = vault.BuildOpenNoteDeepLink(notes[i].Path)
		}
		if notes[i].Tags == nil {
			notes[i].Tags = []string{}
		}
		if notes[i].Wikilinks == nil {
			notes[i].Wikilinks = []string{}
		}
	}
	return notes
}

func withTaskLinks(tasks []vault.Task) []vault.Task {
	for i := range tasks {
		if tasks[i].Link == "" {
			tasks[i].Link = vault.BuildOpenNoteDeepLink(tasks[i].SourcePath)
		}
		if tasks[i].Tags == nil {
			tasks[i].Tags = []string{}
		}
	}
	return tasks
}

// --- reads ---

// GetCurrentVault is the vault the server is serving, or nil.
func (c *Client) GetCurrentVault(ctx context.Context) (*vault.VaultInfo, error) {
	var info *vault.VaultInfo
	if err := c.Get(ctx, "/api/vault", &info); err != nil {
		return nil, err
	}
	return info, nil
}

// GetVaultSettings is the server's vault.json as it reports it.
func (c *Client) GetVaultSettings(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.Get(ctx, "/api/vault/settings", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetVaultSettings writes the whole settings object back.
func (c *Client) SetVaultSettings(ctx context.Context, settings map[string]any) (map[string]any, error) {
	var out map[string]any
	if err := c.Post(ctx, "/api/vault/settings", settings, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Capabilities is what the server says it supports.
func (c *Client) Capabilities(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.Get(ctx, "/api/capabilities", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListNotes(ctx context.Context) ([]vault.NoteMeta, error) {
	var out []vault.NoteMeta
	if err := c.Get(ctx, "/api/notes", &out); err != nil {
		return nil, err
	}
	return withLinks(out), nil
}

func (c *Client) ListFolders(ctx context.Context) ([]vault.FolderEntry, error) {
	var out []vault.FolderEntry
	if err := c.Get(ctx, "/api/folders", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.FolderEntry{}
	}
	return out, nil
}

func (c *Client) ListAssets(ctx context.Context) ([]vault.AssetMeta, error) {
	var out []vault.AssetMeta
	if err := c.Get(ctx, "/api/assets", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.AssetMeta{}
	}
	return out, nil
}

func (c *Client) ReadNote(ctx context.Context, rel string) (vault.NoteContent, error) {
	var out vault.NoteContent
	if err := c.Get(ctx, "/api/notes/read?path="+url.QueryEscape(rel), &out); err != nil {
		return vault.NoteContent{}, err
	}
	out.NoteMeta = withLinks([]vault.NoteMeta{out.NoteMeta})[0]
	return out, nil
}

func (c *Client) SearchText(ctx context.Context, query string) ([]vault.TextSearchMatch, error) {
	params := url.Values{"q": {query}, "backend": {"auto"}}
	var out []vault.TextSearchMatch
	if err := c.Get(ctx, "/api/search/text?"+params.Encode(), &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Link == "" {
			out[i].Link = vault.BuildOpenNoteDeepLink(out[i].Path)
		}
	}
	if out == nil {
		out = []vault.TextSearchMatch{}
	}
	return out, nil
}

func (c *Client) ScanTasks(ctx context.Context, includeExcluded bool) ([]vault.Task, error) {
	path := "/api/tasks"
	if includeExcluded {
		path += "?includeExcluded=1"
	}
	var out []vault.Task
	if err := c.Get(ctx, path, &out); err != nil {
		return nil, err
	}
	return withTaskLinks(out), nil
}

func (c *Client) ScanTasksForPath(ctx context.Context, rel string, includeExcluded bool) ([]vault.Task, error) {
	path := "/api/tasks/for?path=" + url.QueryEscape(rel)
	if includeExcluded {
		path += "&includeExcluded=1"
	}
	var out []vault.Task
	if err := c.Get(ctx, path, &out); err != nil {
		return nil, err
	}
	return withTaskLinks(out), nil
}

// --- writes ---

func (c *Client) noteResult(ctx context.Context, path string, body any) (vault.NoteMeta, error) {
	var out vault.NoteMeta
	if err := c.Post(ctx, path, body, &out); err != nil {
		return vault.NoteMeta{}, err
	}
	return withLinks([]vault.NoteMeta{out})[0], nil
}

func (c *Client) WriteNote(ctx context.Context, rel, body string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/write", map[string]any{"path": rel, "body": body})
}

func (c *Client) CreateNote(ctx context.Context, folder vault.NoteFolder, title, subpath string) (vault.NoteMeta, error) {
	payload := map[string]any{"folder": folder, "subpath": subpath}
	if title != "" {
		payload["title"] = title
	}
	return c.noteResult(ctx, "/api/notes/create", payload)
}

func (c *Client) RenameNote(ctx context.Context, rel, nextTitle string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/rename", map[string]any{"path": rel, "title": nextTitle})
}

func (c *Client) MoveNote(ctx context.Context, rel string, folder vault.NoteFolder, subpath string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/move", map[string]any{"path": rel, "targetFolder": folder, "targetSubpath": subpath})
}

func (c *Client) ArchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/archive", map[string]any{"path": rel})
}

func (c *Client) UnarchiveNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/unarchive", map[string]any{"path": rel})
}

func (c *Client) MoveToTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/trash", map[string]any{"path": rel})
}

func (c *Client) RestoreFromTrash(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/restore", map[string]any{"path": rel})
}

func (c *Client) DuplicateNote(ctx context.Context, rel string) (vault.NoteMeta, error) {
	return c.noteResult(ctx, "/api/notes/duplicate", map[string]any{"path": rel})
}

func (c *Client) DeleteNote(ctx context.Context, rel string) error {
	return c.Post(ctx, "/api/notes/delete", map[string]any{"path": rel}, nil)
}

func (c *Client) EmptyTrash(ctx context.Context) error {
	return c.Post(ctx, "/api/notes/empty-trash", nil, nil)
}

func (c *Client) CreateFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	return c.Post(ctx, "/api/folders/create", map[string]any{"folder": folder, "subpath": subpath}, nil)
}

func (c *Client) RenameFolder(ctx context.Context, folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error) {
	var out struct {
		Subpath string `json:"subpath"`
	}
	if err := c.Post(ctx, "/api/folders/rename", map[string]any{"folder": folder, "oldSubpath": oldSubpath, "newSubpath": newSubpath}, &out); err != nil {
		return "", err
	}
	return out.Subpath, nil
}

func (c *Client) DeleteFolder(ctx context.Context, folder vault.NoteFolder, subpath string) error {
	return c.Post(ctx, "/api/folders/delete", map[string]any{"folder": folder, "subpath": subpath}, nil)
}

// --- templates ---

func (c *Client) ListTemplates(ctx context.Context) ([]vault.CustomTemplateFile, error) {
	var out []vault.CustomTemplateFile
	if err := c.Get(ctx, "/api/templates", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []vault.CustomTemplateFile{}
	}
	return out, nil
}

func (c *Client) ReadTemplate(ctx context.Context, sourcePath string) (string, error) {
	var out vault.CustomTemplateFile
	if err := c.Get(ctx, "/api/templates/read?path="+url.QueryEscape(sourcePath), &out); err != nil {
		return "", err
	}
	return out.Raw, nil
}

func (c *Client) WriteTemplate(ctx context.Context, input vault.WriteTemplateInput) (vault.CustomTemplateFile, error) {
	var out vault.CustomTemplateFile
	if err := c.Post(ctx, "/api/templates/write", input, &out); err != nil {
		return vault.CustomTemplateFile{}, err
	}
	return out, nil
}

func (c *Client) DeleteTemplate(ctx context.Context, sourcePath string) error {
	return c.Post(ctx, "/api/templates/delete", map[string]any{"sourcePath": sourcePath}, nil)
}
