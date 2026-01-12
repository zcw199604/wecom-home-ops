package unraid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

		case strings.Contains(q, "__schema"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"__schema": map[string]interface{}{
						"mutationType": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "docker",
									"type": map[string]interface{}{
										"name": "DockerMutations",
										"kind": "OBJECT",
									},
								},
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "__type"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"__type": map[string]interface{}{
						"fields": []map[string]interface{}{
							{
								"name": "update",
								"args": []map[string]interface{}{
									{
										"name": "id",
										"type": map[string]interface{}{
											"kind": "NON_NULL",
											"ofType": map[string]interface{}{
												"kind": "SCALAR",
												"name": "PrefixedID",
											},
										},
									},
								},
								"type": map[string]interface{}{
									"kind": "OBJECT",
									"name": "DockerContainer",
								},
							},
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

		case strings.Contains(q, "__schema") && strings.Contains(q, "queryType"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"__schema": map[string]interface{}{
						"queryType": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "docker",
									"type": map[string]interface{}{
										"name": "DockerQueries",
										"kind": "OBJECT",
									},
								},
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "__type"):
			name, _ := req.Variables["name"].(string)
			switch name {
			case "DockerQueries":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "containers",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "LIST",
										"ofType": map[string]interface{}{
											"kind": "OBJECT",
											"name": "DockerContainer",
										},
									},
								},
							},
						},
					},
				})
				return

			case "DockerContainer":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "logs",
									"args": []map[string]interface{}{
										{
											"name": "tail",
											"type": map[string]interface{}{
												"kind": "SCALAR",
												"name": "Int",
											},
										},
									},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "stats",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "OBJECT",
										"name": "DockerContainerStats",
									},
								},
							},
						},
					},
				})
				return

			case "DockerContainerStats":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "cpuPercent",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "memUsage",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "memLimit",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "netIO",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "blockIO",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
								{
									"name": "pids",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "SCALAR",
										"name": "String",
									},
								},
							},
						},
					},
				})
				return

			default:
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{},
						},
					},
				})
				return
			}

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
		case strings.Contains(q, "__schema") && strings.Contains(q, "queryType"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"__schema": map[string]interface{}{
						"queryType": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "docker",
									"type": map[string]interface{}{
										"name": "DockerQueries",
										"kind": "OBJECT",
									},
								},
							},
						},
					},
				},
			})
			return

		case strings.Contains(q, "__type"):
			name, _ := req.Variables["name"].(string)
			switch name {
			case "DockerQueries":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{
								{
									"name": "containers",
									"args": []map[string]interface{}{},
									"type": map[string]interface{}{
										"kind": "LIST",
										"ofType": map[string]interface{}{
											"kind": "OBJECT",
											"name": "DockerContainer",
										},
									},
								},
							},
						},
					},
				})
				return

			case "DockerContainer":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{},
						},
					},
				})
				return

			default:
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"__type": map[string]interface{}{
							"fields": []map[string]interface{}{},
						},
					},
				})
				return
			}

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
