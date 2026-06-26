package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func serviceURL(base, path string) string {
	pathPart := path
	queryPart := ""
	if before, after, ok := strings.Cut(path, "?"); ok {
		pathPart = before
		queryPart = after
	}
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		parsed, err := url.Parse(base)
		if err == nil {
			parsed.Path = strings.TrimRight(parsed.Path, "/") + pathPart
			parsed.RawQuery = queryPart
			return parsed.String()
		}
	}
	return "https://" + base + path
}

func getJSON(ctx context.Context, client HTTPDoer, rawURL string, username string, password string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s returned HTTP %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func statusCode(ctx context.Context, client HTTPDoer, method string, rawURL string, username string, password string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return 0, err
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func postJSON(ctx context.Context, client HTTPDoer, rawURL string, username string, password string, body any, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("POST %s returned HTTP %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
