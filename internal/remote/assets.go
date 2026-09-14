package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ZenNotes/tui/internal/vault"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
)

func (c *Client) ImportAsset(ctx context.Context, note, name string, body io.Reader) (vault.ImportedAsset, error) {
	var data bytes.Buffer
	writer := multipart.NewWriter(&data)
	if err := writer.WriteField("notePath", note); err != nil {
		return vault.ImportedAsset{}, err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(name))
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	n, err := io.Copy(part, io.LimitReader(body, (64<<20)+1))
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	if n > 64<<20 {
		return vault.ImportedAsset{}, fmt.Errorf("asset exceeds 64 MiB")
	}
	if err = writer.Close(); err != nil {
		return vault.ImportedAsset{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/assets/upload", &data)
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return vault.ImportedAsset{}, &RequestError{Status: resp.StatusCode, Message: requestErrorMessage(c.BaseURL, "/api/assets/upload", resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(raw)))}
	}
	var out vault.ImportedAsset
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return out, err
}
