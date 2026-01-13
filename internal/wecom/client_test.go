package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_SendText_UsesCachedAccessToken(t *testing.T) {
	t.Parallel()

	var getTokenHits int32
	var sendHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			atomic.AddInt32(&getTokenHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "AT",
				"expires_in":   7200,
			})
			return
		case "/message/send":
			atomic.AddInt32(&sendHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "ok",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		APIBaseURL: srv.URL,
		CorpID:     "ww",
		AgentID:    1,
		Secret:     "sec",
	}, srv.Client())

	ctx := context.Background()
	if err := c.SendText(ctx, TextMessage{ToUser: "u", Content: "a"}); err != nil {
		t.Fatalf("SendText(1) error: %v", err)
	}
	if err := c.SendText(ctx, TextMessage{ToUser: "u", Content: "b"}); err != nil {
		t.Fatalf("SendText(2) error: %v", err)
	}

	if atomic.LoadInt32(&getTokenHits) != 1 {
		t.Fatalf("gettoken hits = %d, want 1", getTokenHits)
	}
	if atomic.LoadInt32(&sendHits) != 2 {
		t.Fatalf("message/send hits = %d, want 2", sendHits)
	}
}

func TestClient_SendText_ConcurrentTokenRefresh(t *testing.T) {
	t.Parallel()

	var getTokenHits int32
	var sendHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			atomic.AddInt32(&getTokenHits, 1)
			time.Sleep(20 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "AT",
				"expires_in":   7200,
			})
			return
		case "/message/send":
			atomic.AddInt32(&sendHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "ok",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		APIBaseURL: srv.URL,
		CorpID:     "ww",
		AgentID:    1,
		Secret:     "sec",
	}, srv.Client())

	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			if err := c.SendText(ctx, TextMessage{ToUser: "u", Content: "msg-" + string(rune('a'+i))}); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("SendText() error: %v", err)
		}
	}

	if atomic.LoadInt32(&getTokenHits) != 1 {
		t.Fatalf("gettoken hits = %d, want 1", getTokenHits)
	}
	if atomic.LoadInt32(&sendHits) != n {
		t.Fatalf("message/send hits = %d, want %d", sendHits, n)
	}
}

func TestClient_UpdateTemplateCardButton_RequestShape(t *testing.T) {
	t.Parallel()

	var getTokenHits int32
	var updateHits int32
	validateErr := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			atomic.AddInt32(&getTokenHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "AT",
				"expires_in":   7200,
			})
			return
		case "/message/update_template_card":
			atomic.AddInt32(&updateHits, 1)
			if got := r.URL.Query().Get("access_token"); got != "AT" {
				select {
				case validateErr <- fmt.Errorf("access_token = %q, want %q", got, "AT"):
				default:
				}
			}
			var payload map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				select {
				case validateErr <- fmt.Errorf("decode payload error: %w", err):
				default:
				}
			} else {
				if got, ok := payload["agentid"].(float64); !ok || int(got) != 1 {
					select {
					case validateErr <- fmt.Errorf("agentid = %v, want 1", payload["agentid"]):
					default:
					}
				}
				if got, ok := payload["response_code"].(string); !ok || got != "RC" {
					select {
					case validateErr <- fmt.Errorf("response_code = %v, want %q", payload["response_code"], "RC"):
					default:
					}
				}
				button, ok := payload["button"].(map[string]interface{})
				if !ok {
					select {
					case validateErr <- fmt.Errorf("button missing"):
					default:
					}
				} else {
					if got, ok := button["replace_name"].(string); !ok || got != "已处理" {
						select {
						case validateErr <- fmt.Errorf("button.replace_name = %v, want %q", button["replace_name"], "已处理"):
						default:
						}
					}
				}
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "ok",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		APIBaseURL: srv.URL,
		CorpID:     "ww",
		AgentID:    1,
		Secret:     "sec",
	}, srv.Client())

	if err := c.UpdateTemplateCardButton(context.Background(), "RC", "已处理"); err != nil {
		t.Fatalf("UpdateTemplateCardButton() error: %v", err)
	}
	select {
	case err := <-validateErr:
		if err != nil {
			t.Fatalf("validate request error: %v", err)
		}
	default:
	}

	if atomic.LoadInt32(&getTokenHits) != 1 {
		t.Fatalf("gettoken hits = %d, want 1", getTokenHits)
	}
	if atomic.LoadInt32(&updateHits) != 1 {
		t.Fatalf("update hits = %d, want 1", updateHits)
	}
}

func TestClient_CreateMenu_RequestShape(t *testing.T) {
	t.Parallel()

	var getTokenHits int32
	var createHits int32
	validateErr := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			atomic.AddInt32(&getTokenHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "AT",
				"expires_in":   7200,
			})
			return
		case "/menu/create":
			atomic.AddInt32(&createHits, 1)
			if got := r.URL.Query().Get("access_token"); got != "AT" {
				select {
				case validateErr <- fmt.Errorf("access_token = %q, want %q", got, "AT"):
				default:
				}
			}
			if got := r.URL.Query().Get("agentid"); got != "1" {
				select {
				case validateErr <- fmt.Errorf("agentid = %q, want %q", got, "1"):
				default:
				}
			}
			var payload map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				select {
				case validateErr <- fmt.Errorf("decode payload error: %w", err):
				default:
				}
			} else {
				buttons, ok := payload["button"].([]interface{})
				if !ok || len(buttons) == 0 {
					select {
					case validateErr <- fmt.Errorf("button missing or empty"):
					default:
					}
				}
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"errmsg":  "ok",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{
		APIBaseURL: srv.URL,
		CorpID:     "ww",
		AgentID:    1,
		Secret:     "sec",
	}, srv.Client())

	if err := c.CreateMenu(context.Background(), DefaultMenu()); err != nil {
		t.Fatalf("CreateMenu() error: %v", err)
	}
	select {
	case err := <-validateErr:
		if err != nil {
			t.Fatalf("validate request error: %v", err)
		}
	default:
	}

	if atomic.LoadInt32(&getTokenHits) != 1 {
		t.Fatalf("gettoken hits = %d, want 1", getTokenHits)
	}
	if atomic.LoadInt32(&createHits) != 1 {
		t.Fatalf("menu/create hits = %d, want 1", createHits)
	}
}
