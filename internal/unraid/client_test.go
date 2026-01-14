package unraid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClient_RestartStopForceUpdate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"id":     "docker:abc",
								"names":  []string{"app"},
								"state":  "running",
								"status": "Up",
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "mutation Stop"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"stop": map[string]interface{}{
							"id":     "docker:abc",
							"state":  "exited",
							"status": "Exited",
						},
					},
				},
			})
			return

		case strings.Contains(q, "mutation Start"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"start": map[string]interface{}{
							"id":     "docker:abc",
							"state":  "running",
							"status": "Up",
						},
					},
				},
			})
			return

		case strings.Contains(q, "mutation ForceUpdate"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"update": map[string]interface{}{
							"__typename": "DockerContainer",
						},
					},
				},
			})
			return

		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "unexpected query"},
				},
			})
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
		Origin:   "o",
	}, srv.Client())

	ctx := context.Background()

	if err := c.RestartContainerByName(ctx, "app"); err != nil {
		t.Fatalf("RestartContainerByName() error: %v", err)
	}
	if err := c.StopContainerByName(ctx, "app"); err != nil {
		t.Fatalf("StopContainerByName() error: %v", err)
	}
	if err := c.ForceUpdateContainerByName(ctx, "app"); err != nil {
		t.Fatalf("ForceUpdateContainerByName() error: %v", err)
	}
}

func TestClient_ForceUpdate_FallbackMutationName(t *testing.T) {
	t.Parallel()

	var updateCalls int32
	var updateContainerCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"id":     "docker:abc",
								"names":  []string{"app"},
								"state":  "running",
								"status": "Up",
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "mutation ForceUpdate") && strings.Contains(q, "update("):
			atomic.AddInt32(&updateCalls, 1)
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "Cannot query field \"update\" on type \"DockerMutations\"."},
				},
			})
			return

		case strings.Contains(q, "mutation ForceUpdate") && strings.Contains(q, "updateContainer("):
			atomic.AddInt32(&updateContainerCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"updateContainer": map[string]interface{}{
							"__typename": "DockerContainer",
						},
					},
				},
			})
			return
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "unexpected query"},
				},
			})
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint:            srv.URL,
		APIKey:              "k",
		Origin:              "o",
		ForceUpdateMutation: "update",
	}, srv.Client())

	if err := c.ForceUpdateContainerByName(context.Background(), "app"); err != nil {
		t.Fatalf("ForceUpdateContainerByName() error: %v", err)
	}

	if atomic.LoadInt32(&updateCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", updateCalls)
	}
	if atomic.LoadInt32(&updateContainerCalls) != 1 {
		t.Fatalf("updateContainer calls = %d, want 1", updateContainerCalls)
	}
}

func TestClient_ForceUpdate_FallbackMutationName_DoubleEscaped(t *testing.T) {
	t.Parallel()

	var updateCalls int32
	var updateContainerCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"id":     "docker:abc",
								"names":  []string{"app"},
								"state":  "running",
								"status": "Up",
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "mutation ForceUpdate") && strings.Contains(q, "updateContainer("):
			atomic.AddInt32(&updateContainerCalls, 1)
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": `Cannot query field \"updateContainer\" on type \"DockerMutations\".`},
				},
			})
			return

		case strings.Contains(q, "mutation ForceUpdate") && strings.Contains(q, "update("):
			atomic.AddInt32(&updateCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"update": map[string]interface{}{
							"__typename": "DockerContainer",
						},
					},
				},
			})
			return

		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "unexpected query"},
				},
			})
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
		Origin:   "o",
	}, srv.Client())

	if err := c.ForceUpdateContainerByName(context.Background(), "app"); err != nil {
		t.Fatalf("ForceUpdateContainerByName() error: %v", err)
	}

	if atomic.LoadInt32(&updateContainerCalls) != 1 {
		t.Fatalf("updateContainer calls = %d, want 1", updateContainerCalls)
	}
	if atomic.LoadInt32(&updateCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", updateCalls)
	}
}

func TestClient_GetContainerStatusStatsLogs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"id":     "docker:abc",
								"names":  []string{"app"},
								"state":  "running",
								"status": "Up 3 hours (healthy)",
								"logs":   "line1\nline2\nline3\nline4",
								"stats": map[string]interface{}{
									"cpuPercent": "1.23%",
									"memUsage":   "128MiB",
									"memLimit":   "2GiB",
									"netIO":      "1.1MB / 2.2MB",
									"blockIO":    "0B / 0B",
									"pids":       "12",
								},
							},
						},
					},
				},
			})
			return

		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "unexpected query"},
				},
			})
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
		Origin:   "o",
	}, srv.Client())

	ctx := context.Background()

	st, err := c.GetContainerStatusByName(ctx, "app")
	if err != nil {
		t.Fatalf("GetContainerStatusByName() error: %v", err)
	}
	if st.Uptime == "" {
		t.Fatalf("GetContainerStatusByName() uptime empty")
	}

	stats, err := c.GetContainerStatsByName(ctx, "app")
	if err != nil {
		t.Fatalf("GetContainerStatsByName() error: %v", err)
	}
	if stats.Stats == nil {
		t.Fatalf("GetContainerStatsByName() stats nil")
	}

	logs, err := c.GetContainerLogsByName(ctx, "app", 2)
	if err != nil {
		t.Fatalf("GetContainerLogsByName() error: %v", err)
	}
	if !strings.Contains(logs.Logs, "line") {
		t.Fatalf("GetContainerLogsByName() logs unexpected: %q", logs.Logs)
	}
	if strings.Contains(logs.Logs, "line1") {
		t.Fatalf("GetContainerLogsByName() want tail 2 lines, got: %q", logs.Logs)
	}
}

func TestClient_GetContainerLogs_Unsupported(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers") && strings.Contains(q, "logs"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "Cannot query field \"logs\" on type \"DockerContainer\"."},
				},
			})
			return

		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "unexpected query"},
				},
			})
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
		Origin:   "o",
	}, srv.Client())

	ctx := context.Background()
	if _, err := c.GetContainerLogsByName(ctx, "app", 50); err == nil {
		t.Fatalf("GetContainerLogsByName() error = nil, want not nil")
	}
}

func TestClient_ConfigOverride_LogsPayload(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query
		if !strings.Contains(q, "containerLogs") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{{"message": "unexpected query"}},
			})
			return
		}
		if strings.Contains(q, "tail:") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{{"message": "unexpected argument"}},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"docker": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"id":     "docker:abc",
							"names":  []string{"app"},
							"state":  "running",
							"status": "Up",
							"containerLogs": map[string]interface{}{
								"content": "line1\nline2\nline3",
							},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	disableTail := ""
	c := NewClient(ClientConfig{
		Endpoint:         srv.URL,
		APIKey:           "k",
		Origin:           "o",
		LogsField:        "containerLogs",
		LogsTailArg:      &disableTail,
		LogsPayloadField: "content",
	}, srv.Client())

	ctx := context.Background()
	logs, err := c.GetContainerLogsByName(ctx, "app", 2)
	if err != nil {
		t.Fatalf("GetContainerLogsByName() error: %v", err)
	}
	if strings.Contains(logs.Logs, "line1") {
		t.Fatalf("GetContainerLogsByName() want tail 2 lines, got: %q", logs.Logs)
	}
}

func TestClient_ConfigOverride_StatsScalar(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		q := req.Query
		if !strings.Contains(q, "metrics") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{{"message": "unexpected query"}},
			})
			return
		}
		if strings.Contains(q, "metrics {") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{{"message": "metrics is scalar"}},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"docker": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"id":      "docker:abc",
							"names":   []string{"app"},
							"state":   "running",
							"status":  "Up",
							"metrics": map[string]interface{}{"cpu": "1%"},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Origin:      "o",
		StatsField:  "metrics",
		StatsFields: []string{},
	}, srv.Client())

	ctx := context.Background()
	stats, err := c.GetContainerStatsByName(ctx, "app")
	if err != nil {
		t.Fatalf("GetContainerStatsByName() error: %v", err)
	}
	if stats.Stats == nil {
		t.Fatalf("GetContainerStatsByName() stats nil")
	}
}
