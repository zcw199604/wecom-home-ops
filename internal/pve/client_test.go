package pve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetVersion_UsesAuthorizationAndApi2JSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/version" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api2/json/version")
		}
		if got := r.Header.Get("Authorization"); got != "PVEAPIToken=x" {
			t.Fatalf("Authorization = %q, want %q", got, "PVEAPIToken=x")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"version": "8.1",
				"release": "8.1",
			},
		})
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(ClientConfig{
		BaseURL:  srv.URL,
		APIToken: "PVEAPIToken=x",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	v, err := c.GetVersion(context.Background())
	if err != nil {
		t.Fatalf("GetVersion() error: %v", err)
	}
	if v.Version != "8.1" {
		t.Fatalf("Version = %q, want %q", v.Version, "8.1")
	}
}

