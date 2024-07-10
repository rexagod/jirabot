package pkg

import (
	"fmt"
	"net/http"
)

// JIRATransporter implements the http.RoundTripper interface.
type JIRATransporter struct {
	Transport http.RoundTripper
	Token     string
}

// RoundTrip executes a single HTTP transaction.
func (t *JIRATransporter) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.Token)

	response, err := t.Transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP transaction: %w", err)
	}

	return response, nil
}

// GHTransporter implements the http.RoundTripper interface.
type GHTransporter struct {
	Transport http.RoundTripper
	Token     string
}

// RoundTrip executes a single HTTP transaction.
func (t *GHTransporter) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Accept", "application/vnd.github+json")

	response, err := t.Transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP transaction: %w", err)
	}

	return response, nil
}
