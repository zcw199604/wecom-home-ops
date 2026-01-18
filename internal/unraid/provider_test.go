package unraid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/core"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type recordWeCom struct {
	mu    sync.Mutex
	texts []wecom.TextMessage
	cards []wecom.TemplateCardMessage
}

func (r *recordWeCom) SendText(_ context.Context, msg wecom.TextMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, msg)
	return nil
}

func (r *recordWeCom) SendTemplateCard(_ context.Context, msg wecom.TemplateCardMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cards = append(r.cards, msg)
	return nil
}

func (r *recordWeCom) Texts() []wecom.TextMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]wecom.TextMessage(nil), r.texts...)
}

func (r *recordWeCom) Cards() []wecom.TemplateCardMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]wecom.TemplateCardMessage(nil), r.cards...)
}

func TestProvider_ViewFlowsAndLogTail(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var stopHits int
	var forceHits int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q := req.Query

		switch {
		case strings.Contains(q, "metrics"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"metrics": map[string]interface{}{
						"cpu": map[string]interface{}{
							"percentTotal": 12.34,
							"cpus": []map[string]interface{}{
								{
									"percentTotal":  12.34,
									"percentUser":   1.0,
									"percentSystem": 2.0,
									"percentNice":   0.0,
									"percentIdle":   87.66,
									"percentIrq":    0.0,
									"percentGuest":  0.0,
									"percentSteal":  0.0,
								},
							},
						},
						"memory": map[string]interface{}{
							"total":        "1073741824",
							"used":         "536870912",
							"free":         "268435456",
							"available":    "805306368",
							"percentTotal": 50.0,
						},
					},
				},
			})
			return
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
			mu.Lock()
			stopHits++
			mu.Unlock()
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

		case strings.Contains(q, "mutation ForceUpdate"):
			mu.Lock()
			forceHits++
			mu.Unlock()
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

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	client := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
	}, srv.Client())

	p := NewProvider(ProviderDeps{
		WeCom:  rec,
		Client: client,
		State:  store,
	})

	ctx := context.Background()
	userID := "u"

	_ = "stop"
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidStop}); err != nil || !ok {
		t.Fatalf("HandleEvent(stop) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	cards := rec.Cards()
	if len(cards) == 0 {
		t.Fatalf("want container select card")
	}
	mainTitle, _ := cards[len(cards)-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "选择容器" {
		t.Fatalf("container select title = %q, want %q", title, "选择容器")
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidContainerSelectPrefix + "app"}); err != nil || !ok {
		t.Fatalf("HandleEvent(container select for stop) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(stop) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	_ = "force update"
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidForceUpdate}); err != nil || !ok {
		t.Fatalf("HandleEvent(force_update) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidContainerSelectPrefix + "app"}); err != nil || !ok {
		t.Fatalf("HandleEvent(container select for force_update) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(force_update) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	mu.Lock()
	gotStop := stopHits
	gotForce := forceHits
	mu.Unlock()

	if gotStop != 1 {
		t.Fatalf("stop hits = %d, want 1", gotStop)
	}
	if gotForce != 1 {
		t.Fatalf("force update hits = %d, want 1", gotForce)
	}

	texts := rec.Texts()
	if len(texts) < 2 || !strings.Contains(texts[len(texts)-1].Content, "执行成功") {
		t.Fatalf("want success reply, got: %#v", texts)
	}
}

func TestProvider_MenuNavigation_Cards(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom:  rec,
		Client: nil,
		State:  store,
	})

	ctx := context.Background()
	userID := "u"

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	cardMsg := rec.Cards()
	if len(cardMsg) == 0 {
		t.Fatalf("want entry card")
	}
	mainTitle, _ := cardMsg[len(cardMsg)-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "Unraid 容器" {
		t.Fatalf("entry title = %q, want %q", title, "Unraid 容器")
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidMenuOps}); err != nil || !ok {
		t.Fatalf("HandleEvent(menu ops) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if st, ok := store.Get(userID); !ok || st.ServiceKey != "unraid" {
		t.Fatalf("state = %#v ok=%v, want ServiceKey=unraid", st, ok)
	}
	mainTitle, _ = rec.Cards()[len(rec.Cards())-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "Unraid 容器操作" {
		t.Fatalf("ops title = %q, want %q", title, "Unraid 容器操作")
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidMenuView}); err != nil || !ok {
		t.Fatalf("HandleEvent(menu view) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	mainTitle, _ = rec.Cards()[len(rec.Cards())-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "Unraid 容器查看" {
		t.Fatalf("view title = %q, want %q", title, "Unraid 容器查看")
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidBackToMenu}); err != nil || !ok {
		t.Fatalf("HandleEvent(back) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	mainTitle, _ = rec.Cards()[len(rec.Cards())-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "Unraid 容器" {
		t.Fatalf("back title = %q, want %q", title, "Unraid 容器")
	}
}

func TestProvider_ClickMenuView_SendsTemplateCard(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom:  rec,
		Client: nil,
		State:  store,
	})

	ctx := context.Background()
	userID := "u"

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{
		Event:    "CLICK",
		EventKey: wecom.EventKeyUnraidMenuView,
	}); err != nil || !ok {
		t.Fatalf("HandleEvent(click menu view) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	if got := len(rec.Texts()); got != 0 {
		t.Fatalf("text message count = %d, want 0", got)
	}
	if got := len(rec.Cards()); got != 1 {
		t.Fatalf("template card count = %d, want 1", got)
	}

	st, ok := store.Get(userID)
	if !ok || st.ServiceKey != "unraid" {
		t.Fatalf("state = %#v ok=%v, want ServiceKey=unraid", st, ok)
	}

	mainTitle, _ := rec.Cards()[len(rec.Cards())-1].Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "Unraid 容器查看" {
		t.Fatalf("view title = %q, want %q", title, "Unraid 容器查看")
	}
}
