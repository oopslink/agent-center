package sse

import (
	"context"
	"io"
	"net/http"
)

// httpGet wraps a quick GET (test helper).
func httpGet(url string) (*http.Response, error) {
	return http.Get(url)
}

// httpGetStream opens a streaming GET with optional Last-Event-ID header.
// Caller closes the returned ReadCloser.
func httpGetStream(ctx context.Context, url, lastEventID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
