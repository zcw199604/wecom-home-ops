package qinglong

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

func (r *recordWeCom) LastText() (wecom.TextMessage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.texts) == 0 {
		return wecom.TextMessage{}, false
	}
	return r.texts[len(r.texts)-1], true
}

func (r *recordWeCom) LastCard() (wecom.TemplateCardMessage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cards) == 0 {
		return wecom.TemplateCardMessage{}, false
	}
	return r.cards[len(r.cards)-1], true
}

func TestProvider_OnEnter_NoInstances(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
	})

	if err := p.OnEnter(context.Background(), "u"); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	msg, ok := rec.LastText()
	if !ok || !strings.Contains(msg.Content, "未配置青龙实例") {
		t.Fatalf("want missing instances text, got: %#v", msg)
	}
}

func TestProvider_OnEnter_MultiInstance_SelectAndMenu(t *testing.T) {
	t.Parallel()

	var tokenHits int32
	var listHits int32
	var getHits int32
	var runHits int32
	var logHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			atomic.AddInt32(&tokenHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return

		case r.URL.Path == "/open/crons" && r.Method == http.MethodGet:
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer AT" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&listHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"data": []map[string]interface{}{
						{"id": 1, "name": "a"},
						{"id": 2, "name": "b"},
						{"id": 3, "name": "c"},
						{"id": 4, "name": "d"},
						{"id": 5, "name": "e"},
					},
					"total": 5,
				},
			})
			return

		case strings.HasPrefix(r.URL.Path, "/open/crons/") && r.Method == http.MethodGet:
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer AT" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/log") {
				atomic.AddInt32(&logHits, 1)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"code": 200,
					"data": "hello log",
				})
				return
			}
			atomic.AddInt32(&getHits, 1)
			idStr := strings.TrimPrefix(r.URL.Path, "/open/crons/")
			id, _ := strconv.Atoi(idStr)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"id":         id,
					"name":       "job",
					"command":    "cmd",
					"schedule":   "0 0 * * *",
					"isDisabled": 0,
					"status":     0,
				},
			})
			return

		case r.URL.Path == "/open/crons/run" && r.Method == http.MethodPut:
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer AT" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&runHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": true,
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	clientA, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient(A) error: %v", err)
	}
	clientB, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient(B) error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "a", Name: "A", Client: clientA},
			{ID: "b", Name: "B", Client: clientB},
		},
	})

	ctx := context.Background()
	userID := "u"

	// 1) 进入：多实例应返回实例选择卡片
	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	cardMsg, ok := rec.LastCard()
	if !ok {
		t.Fatalf("want instance select card")
	}
	mainTitle, _ := cardMsg.Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "青龙(QL)" {
		t.Fatalf("card title = %q, want %q", title, "青龙(QL)")
	}

	// 2) 选择实例 a：进入动作菜单
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongInstanceSelectPrefix + "a"}); err != nil || !ok {
		t.Fatalf("HandleEvent(instance select) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	cardMsg, ok = rec.LastCard()
	if !ok {
		t.Fatalf("want action card")
	}
	mainTitle, _ = cardMsg.Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "青龙(QL) 任务管理" {
		t.Fatalf("card title = %q, want %q", title, "青龙(QL) 任务管理")
	}

	// 3) 任务列表：应返回最多4个任务按钮 + 1个动作菜单按钮
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionList}); err != nil || !ok {
		t.Fatalf("HandleEvent(action list) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	cardMsg, ok = rec.LastCard()
	if !ok {
		t.Fatalf("want cron list card")
	}
	buttons, _ := cardMsg.Card["button_list"].([]map[string]interface{})
	if len(buttons) != 5 {
		t.Fatalf("button_list len = %d, want 5 (4 jobs + menu)", len(buttons))
	}

	// 4) 选择任务：进入任务操作菜单
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronSelectPrefix + "1"}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron select) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	cardMsg, ok = rec.LastCard()
	if !ok {
		t.Fatalf("want cron action card")
	}
	mainTitle, _ = cardMsg.Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); !strings.Contains(title, "任务操作") {
		t.Fatalf("cron action title = %q, want contains %q", title, "任务操作")
	}

	// 5) 查看日志：返回文本
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronLog}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron log) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok := rec.LastText()
	if !ok || !strings.Contains(txt.Content, "最近日志") {
		t.Fatalf("want log text, got: %#v", txt)
	}

	// 6) 执行：运行→确认→HandleConfirm 调用 run 接口
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronRun}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron run) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm() ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok = rec.LastText()
	if !ok || !strings.Contains(txt.Content, "执行成功") {
		t.Fatalf("want success text, got: %#v", txt)
	}

	if atomic.LoadInt32(&tokenHits) != 1 {
		t.Fatalf("token hits = %d, want 1", tokenHits)
	}
	if atomic.LoadInt32(&listHits) != 1 {
		t.Fatalf("list hits = %d, want 1", listHits)
	}
	if atomic.LoadInt32(&getHits) != 1 {
		t.Fatalf("get hits = %d, want 1", getHits)
	}
	if atomic.LoadInt32(&logHits) != 1 {
		t.Fatalf("log hits = %d, want 1", logHits)
	}
	if atomic.LoadInt32(&runHits) != 1 {
		t.Fatalf("run hits = %d, want 1", runHits)
	}
}

func TestProvider_SearchAndByIDInputs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return
		case r.URL.Path == "/open/crons" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"data": []map[string]interface{}{
						{"id": 1, "name": "a"},
					},
					"total": 1,
				},
			})
			return
		case strings.HasPrefix(r.URL.Path, "/open/crons/") && r.Method == http.MethodGet:
			idStr := strings.TrimPrefix(r.URL.Path, "/open/crons/")
			id, _ := strconv.Atoi(idStr)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"id":   id,
					"name": "job",
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
	})

	ctx := context.Background()
	userID := "u"

	// 单实例 OnEnter：直接进入动作菜单并选择实例
	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}

	// 搜索：进入待输入关键词
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionSearch}); err != nil || !ok {
		t.Fatalf("HandleEvent(search) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, ""); err != nil || !ok {
		t.Fatalf("HandleText(empty kw) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok := rec.LastText()
	if !ok || !strings.Contains(txt.Content, "关键词不能为空") {
		t.Fatalf("want empty keyword msg, got: %#v", txt)
	}
	if ok, err := p.HandleText(ctx, userID, "a"); err != nil || !ok {
		t.Fatalf("HandleText(kw) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if _, ok := rec.LastCard(); !ok {
		t.Fatalf("want search result card")
	}

	// 按ID操作：进入待输入任务ID
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionByID}); err != nil || !ok {
		t.Fatalf("HandleEvent(by id) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "bad"); err != nil || !ok {
		t.Fatalf("HandleText(bad id) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok = rec.LastText()
	if !ok || !strings.Contains(txt.Content, "任务ID不合法") {
		t.Fatalf("want invalid id msg, got: %#v", txt)
	}
	if ok, err := p.HandleText(ctx, userID, "1"); err != nil || !ok {
		t.Fatalf("HandleText(id=1) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if _, ok := rec.LastCard(); !ok {
		t.Fatalf("want cron action card by id")
	}
}

func TestProvider_ListEmpty_ShowsText(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return
		case r.URL.Path == "/open/crons" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"data":  []map[string]interface{}{},
					"total": 0,
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
	})

	ctx := context.Background()
	userID := "u"

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionList}); err != nil || !ok {
		t.Fatalf("HandleEvent(list) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok := rec.LastText()
	if !ok || !strings.Contains(txt.Content, "未找到任务") {
		t.Fatalf("want empty list text, got: %#v", txt)
	}
}

func TestProvider_SearchNoResult_ShowsText(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return
		case r.URL.Path == "/open/crons" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"data":  []map[string]interface{}{},
					"total": 0,
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
	})

	ctx := context.Background()
	userID := "u"

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionSearch}); err != nil || !ok {
		t.Fatalf("HandleEvent(search) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "nope"); err != nil || !ok {
		t.Fatalf("HandleText(keyword) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok := rec.LastText()
	if !ok || !strings.Contains(txt.Content, "未找到任务") {
		t.Fatalf("want no result text, got: %#v", txt)
	}
}

func TestProvider_LogEmptyAndTruncated(t *testing.T) {
	t.Parallel()

	var logHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return

		case strings.HasPrefix(r.URL.Path, "/open/crons/") && r.Method == http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "/log") {
				atomic.AddInt32(&logHits, 1)
				if atomic.LoadInt32(&logHits) == 1 {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"code": 200,
						"data": "",
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"code": 200,
					"data": strings.Repeat("a", 1300),
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"id":   1,
					"name": "job",
				},
			})
			return

		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
	})

	ctx := context.Background()
	userID := "u"

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronSelectPrefix + "1"}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron select) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	// 1) 空日志
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronLog}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron log empty) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok := rec.LastText()
	if !ok || !strings.Contains(txt.Content, "日志为空") {
		t.Fatalf("want empty log text, got: %#v", txt)
	}

	// 2) 超长日志截断
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronLog}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron log long) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	txt, ok = rec.LastText()
	if !ok || !strings.Contains(txt.Content, "…(前略)") {
		t.Fatalf("want truncated prefix, got: %#v", txt)
	}
}

func TestProvider_EnableDisable_ConfirmFlow(t *testing.T) {
	t.Parallel()

	var enableHits int32
	var disableHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return

		case strings.HasPrefix(r.URL.Path, "/open/crons/") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"id":   1,
					"name": "job",
				},
			})
			return

		case r.URL.Path == "/open/crons/enable" && r.Method == http.MethodPut:
			atomic.AddInt32(&enableHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": true})
			return

		case r.URL.Path == "/open/crons/disable" && r.Method == http.MethodPut:
			atomic.AddInt32(&disableHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": true})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		ClientID:     "id",
		ClientSecret: "sec",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
	})

	ctx := context.Background()
	userID := "u"

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronSelectPrefix + "1"}); err != nil || !ok {
		t.Fatalf("HandleEvent(cron select) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	// Enable
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronEnable}); err != nil || !ok {
		t.Fatalf("HandleEvent(enable) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(enable) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	// Disable
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongCronDisable}); err != nil || !ok {
		t.Fatalf("HandleEvent(disable) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(disable) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	if atomic.LoadInt32(&enableHits) != 1 {
		t.Fatalf("enable hits = %d, want 1", enableHits)
	}
	if atomic.LoadInt32(&disableHits) != 1 {
		t.Fatalf("disable hits = %d, want 1", disableHits)
	}
}

func TestProvider_MenuAndSwitchInstance_Navigation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 200,
				"data": map[string]interface{}{
					"token":      "AT",
					"token_type": "Bearer",
					"expiration": time.Now().Add(1 * time.Hour).Unix(),
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	t.Cleanup(srv.Close)

	clientA, err := NewClient(ClientConfig{BaseURL: srv.URL, ClientID: "id", ClientSecret: "sec"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient(A) error: %v", err)
	}
	clientB, err := NewClient(ClientConfig{BaseURL: srv.URL, ClientID: "id", ClientSecret: "sec"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient(B) error: %v", err)
	}

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: rec,
		State: store,
		Instances: []Instance{
			{ID: "a", Name: "A", Client: clientA},
			{ID: "b", Name: "B", Client: clientB},
		},
	})

	ctx := context.Background()
	userID := "u"

	// 1) 进入：实例选择卡片
	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}
	cardMsg, ok := rec.LastCard()
	if !ok {
		t.Fatalf("want instance select card")
	}
	mainTitle, _ := cardMsg.Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "青龙(QL)" {
		t.Fatalf("card title = %q, want %q", title, "青龙(QL)")
	}

	// 2) 选择实例 a
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongInstanceSelectPrefix + "a"}); err != nil || !ok {
		t.Fatalf("HandleEvent(select a) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if st, ok := store.Get(userID); !ok || st.InstanceID != "a" {
		t.Fatalf("state = %#v ok=%v, want InstanceID=a", st, ok)
	}

	// 3) 进入搜索待输入，然后用“菜单”回到动作菜单应清理 Step
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionSearch}); err != nil || !ok {
		t.Fatalf("HandleEvent(search) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if st, ok := store.Get(userID); !ok || st.Step != core.StepAwaitingQinglongSearchKeyword {
		t.Fatalf("state = %#v ok=%v, want StepAwaitingQinglongSearchKeyword", st, ok)
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongMenu}); err != nil || !ok {
		t.Fatalf("HandleEvent(menu) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if st, ok := store.Get(userID); !ok || st.Step != "" {
		t.Fatalf("state = %#v ok=%v, want Step cleared", st, ok)
	}

	// 4) 切换实例：回到实例选择，InstanceID 应清空
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyQinglongActionSwitchInstance}); err != nil || !ok {
		t.Fatalf("HandleEvent(switch) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if st, ok := store.Get(userID); !ok || st.InstanceID != "" {
		t.Fatalf("state = %#v ok=%v, want InstanceID empty", st, ok)
	}
	cardMsg, ok = rec.LastCard()
	if !ok {
		t.Fatalf("want instance select card after switch")
	}
	mainTitle, _ = cardMsg.Card["main_title"].(map[string]interface{})
	if title, _ := mainTitle["title"].(string); title != "青龙(QL)" {
		t.Fatalf("card title = %q, want %q", title, "青龙(QL)")
	}
}
