package main

import (
	"errors"
	"net/http"
	"time"
)

type applicationTransport struct{}

func (applicationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("User-Agent", "Waymark/"+version+" (self-hosted publishing CLI)")
	return http.DefaultTransport.RoundTrip(request)
}

func safeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: applicationTransport{},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (credential resolvedCredential) checkOrigin(server string) error {
	if credential.source == "env" {
		return nil
	}
	saved, err := normalizeServerURL(credential.boundOrigin)
	if err != nil || saved != server {
		return errors.New("saved token belongs to a different control origin; run `waymark login --server <instance-url>` for this instance")
	}
	return nil
}
