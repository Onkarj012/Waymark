package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPullBindingUsesCapturedCredentialSnapshot(t *testing.T) {
	for _, change := range []string{"replace-config", "remove-config", "environment-token"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
			t.Setenv("WAYMARK_TOKEN", "")
			t.Setenv("WAYMARK_URL", "")
			var bearerRequests atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Authorization") != "" {
					bearerRequests.Add(1)
				}
				if request.URL.Path != "/.well-known/waymark" {
					writer.WriteHeader(http.StatusUnauthorized)
					return
				}
				var err error
				switch change {
				case "replace-config":
					err = saveConfig(config{URL: server.URL, Token: "instance-b-token"})
				case "remove-config":
					err = os.Remove(configPath())
				case "environment-token":
					err = os.Setenv("WAYMARK_TOKEN", "new-explicit-token")
				}
				if err != nil {
					t.Error(err)
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				writer.Header().Set("Content-Type", "application/json")
				json.NewEncoder(writer).Encode(discoveryResponse{ControlURL: server.URL, ContentURL: "https://pages.example.test"})
			}))
			defer server.Close()
			if err := saveConfig(config{URL: "https://instance-a.example.test", Token: "instance-a-token"}); err != nil {
				t.Fatal(err)
			}
			_, err := fetchSource(server.URL, "AbCdEf123456")
			if err == nil || !strings.Contains(err.Error(), "different control origin") || bearerRequests.Load() != 0 {
				t.Fatalf("credential escaped its captured origin after %s: error=%v, bearer requests=%d", change, err, bearerRequests.Load())
			}
		})
	}
}

func TestPullCapturedEnvironmentTokenDoesNotFallBackAfterDiscovery(t *testing.T) {
	server, _, requests := sourceFixture(t, nil)
	if err := saveConfig(config{URL: "https://other.example.test", Token: "unrelated-saved-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_TOKEN", "saved-token")
	original := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/.well-known/waymark" {
			if err := os.Unsetenv("WAYMARK_TOKEN"); err != nil {
				t.Error(err)
			}
		}
		original.ServeHTTP(writer, request)
	})
	if _, err := fetchSource(server.URL, "AbCdEf123456"); err != nil || *requests != 1 {
		t.Fatalf("captured environment token was not preserved: %v", err)
	}
}
