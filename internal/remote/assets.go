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
	resp, err := c.send(req, "/api/assets/upload")
	if err != nil {
		return vault.ImportedAsset{}, err
	}
	defer resp.Body.Close()
	var out vault.ImportedAsset
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return out, err
}
