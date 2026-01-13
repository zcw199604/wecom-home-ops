package unraid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
	var tails []int
	var stopHits int
	var startHits int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q := req.Query

		switch {
		case strings.Contains(q, "docker { containers"):
			if idx := strings.Index(q, "tail:"); idx >= 0 {
				rest := q[idx+len("tail:"):]
				rest = strings.TrimSpace(rest)
				var digits strings.Builder
				for _, ch := range rest {
					if ch < '0' || ch > '9' {
						break
					}
					_ = digits.WriteByte(byte(ch))
				}
				if digits.Len() > 0 {
					n, _ := strconv.Atoi(digits.String())
					mu.Lock()
					tails = append(tails, n)
					mu.Unlock()
				}
			}
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

		case strings.Contains(q, "mutation Start"):
			mu.Lock()
			startHits++
			mu.Unlock()
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

	rec := &recordWeCom{}
	store := core.NewStateStore(1 * time.Minute)
	t.Cleanup(store.Close)

	client := NewClient(ClientConfig{
		Endpoint: srv.URL,
		APIKey:   "k",
		Origin:   "o",
	}, srv.Client())

	p := NewProvider(ProviderDeps{
		WeCom:  rec,
		Client: client,
		State:  store,
	})

	ctx := context.Background()
	userID := "u"

	// 1) 查看状态：提示输入→回显运行时长
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewStatus}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_status) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(status) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	texts := rec.Texts()
	if len(texts) == 0 || !strings.Contains(texts[len(texts)-1].Content, "运行时长: 3 hours") {
		t.Fatalf("status reply missing uptime, got: %#v", texts)
	}

	// 2) 资源概览
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewStats}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_stats) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(stats) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	texts = rec.Texts()
	if len(texts) == 0 || !strings.Contains(texts[len(texts)-1].Content, "【资源概览】") {
		t.Fatalf("stats overview reply missing, got: %#v", texts[len(texts)-1].Content)
	}

	// 3) 资源详情
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewStatsDetail}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_stats_detail) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(stats_detail) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	texts = rec.Texts()
	if len(texts) == 0 || !strings.Contains(texts[len(texts)-1].Content, "【资源详情】") {
		t.Fatalf("stats detail reply missing, got: %#v", texts[len(texts)-1].Content)
	}

	// 4) 查看日志：默认 tail=50
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(logs default) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	// 5) 查看日志：指定 tail=2，应截断最新两行并标记已截取
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs2) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app 2"); err != nil || !ok {
		t.Fatalf("HandleText(logs 2) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	texts = rec.Texts()
	if last := texts[len(texts)-1].Content; !strings.Contains(last, "tail 2") || !strings.Contains(last, "line3") || strings.Contains(last, "line1") {
		t.Fatalf("logs tail reply unexpected: %q", last)
	}

	// 6) 查看日志：超过上限应 clamp 到 200
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs3) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app 999"); err != nil || !ok {
		t.Fatalf("HandleText(logs 999) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	mu.Lock()
	gotTails := append([]int(nil), tails...)
	gotStop := stopHits
	gotStart := startHits
	mu.Unlock()

	if len(gotTails) < 3 || gotTails[len(gotTails)-3] != 50 || gotTails[len(gotTails)-2] != 2 || gotTails[len(gotTails)-1] != 200 {
		t.Fatalf("tails = %#v, want last [50 2 200]", gotTails)
	}

	// 7) 操作类动作：重启需要确认并执行 stop+start
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidRestart}); err != nil || !ok {
		t.Fatalf("HandleEvent(restart) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(restart input) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(restart) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	mu.Lock()
	gotStop = stopHits
	gotStart = startHits
	mu.Unlock()
	if gotStop != 1 || gotStart != 1 {
		t.Fatalf("stop/start hits = %d/%d, want 1/1", gotStop, gotStart)
	}
}

func TestProvider_InvalidLogTailInput(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "unexpected"}},
		})
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

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app abc"); err != nil || !ok {
		t.Fatalf("HandleText(logs abc) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	texts := rec.Texts()
	if len(texts) == 0 || !strings.Contains(texts[len(texts)-1].Content, "日志行数不合法") {
		t.Fatalf("want invalid tail message, got: %#v", texts)
	}
}

func TestProvider_LogTailClampMin(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var tails []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q := req.Query
		if strings.Contains(q, "docker { containers") {
			if idx := strings.Index(q, "tail:"); idx >= 0 {
				rest := q[idx+len("tail:"):]
				rest = strings.TrimSpace(rest)
				var digits strings.Builder
				for _, ch := range rest {
					if ch < '0' || ch > '9' {
						break
					}
					_ = digits.WriteByte(byte(ch))
				}
				if digits.Len() > 0 {
					n, _ := strconv.Atoi(digits.String())
					mu.Lock()
					tails = append(tails, n)
					mu.Unlock()
				}
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
								"logs":   "line1\nline2\nline3",
							},
						},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "unexpected"}},
		})
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

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app 0"); err != nil || !ok {
		t.Fatalf("HandleText(logs 0) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewLogs}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_logs2) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app -1"); err != nil || !ok {
		t.Fatalf("HandleText(logs -1) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	mu.Lock()
	got := append([]int(nil), tails...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("tails = %#v, want [1 1]", got)
	}
}

func TestProvider_ViewStatus_ContainerNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(req.Query, "docker { containers") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"docker": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"id":     "docker:abc",
								"names":  []string{"other"},
								"state":  "running",
								"status": "Up",
							},
						},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "unexpected"}},
		})
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

	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidViewStatus}); err != nil || !ok {
		t.Fatalf("HandleEvent(view_status) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "missing"); err != nil || !ok {
		t.Fatalf("HandleText(missing) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	texts := rec.Texts()
	if len(texts) == 0 || !strings.Contains(texts[len(texts)-1].Content, "未找到容器") {
		t.Fatalf("want not found error, got: %#v", texts)
	}
}

func TestFormatContainerStatsOverview_FallbackToRaw(t *testing.T) {
	t.Parallel()

	out := formatContainerStatsOverview(ContainerStats{
		Name:  "app",
		Stats: map[string]interface{}{"unknown": "x"},
	})
	if !strings.Contains(out, "未识别到常见字段") {
		t.Fatalf("want fallback text, got: %q", out)
	}
	if !strings.Contains(out, "\"unknown\"") {
		t.Fatalf("want raw json in output, got: %q", out)
	}
}

func TestTruncateForWecom_UTF8Safe(t *testing.T) {
	t.Parallel()

	in := strings.Repeat("你", 2000)
	out := truncateForWecom(in)
	if !utf8.ValidString(out) {
		t.Fatalf("truncateForWecom output is not valid utf8")
	}
	if len(out) > maxWecomTextBytes {
		t.Fatalf("output bytes = %d, want <= %d", len(out), maxWecomTextBytes)
	}
	if !strings.HasSuffix(out, wecomTruncSuffix) {
		t.Fatalf("want suffix %q", wecomTruncSuffix)
	}
}

func TestProvider_StopAndForceUpdate_ConfirmFlow(t *testing.T) {
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

	// Stop
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidStop}); err != nil || !ok {
		t.Fatalf("HandleEvent(stop) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(stop input) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleConfirm(ctx, userID); err != nil || !ok {
		t.Fatalf("HandleConfirm(stop) ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	// ForceUpdate
	if ok, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyUnraidForceUpdate}); err != nil || !ok {
		t.Fatalf("HandleEvent(force_update) ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if ok, err := p.HandleText(ctx, userID, "app"); err != nil || !ok {
		t.Fatalf("HandleText(force_update input) ok=%v err=%v, want ok=true err=nil", ok, err)
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

func TestProvider_ClickMenuView_FallsBackToTextMenu(t *testing.T) {
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

	if got := len(rec.Texts()); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
	if got := len(rec.Cards()); got != 0 {
		t.Fatalf("template card count = %d, want 0", got)
	}

	st, ok := store.Get(userID)
	if !ok || st.ServiceKey != "unraid" || st.Step != core.StepAwaitingUnraidViewAction {
		t.Fatalf("state = %#v ok=%v, want ServiceKey=unraid Step=awaiting_unraid_view_action", st, ok)
	}
}
