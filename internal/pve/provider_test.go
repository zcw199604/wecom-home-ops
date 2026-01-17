package pve

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

func TestProvider_VMRebootFlowByVMID(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var rebootHits int
	var taskHits int

	const upid = "UPID:node1:00000000:00000000:00000000:qmreboot:100:root@pam:"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api2/json/cluster/resources":
			if r.URL.Query().Get("type") != "vm" {
				t.Fatalf("type query = %q, want %q", r.URL.Query().Get("type"), "vm")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"type": "qemu",
						"vmid": 100,
						"name": "vm100",
						"node": "node1",
					},
				},
			})
			return

		case r.Method == http.MethodPost && r.URL.Path == "/api2/json/nodes/node1/qemu/100/status/reboot":
			if got := r.Header.Get("Authorization"); got != "PVEAPIToken=x" {
				t.Fatalf("Authorization = %q, want %q", got, "PVEAPIToken=x")
			}
			mu.Lock()
			rebootHits++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": upid,
			})
			return

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api2/json/nodes/node1/tasks/") && strings.HasSuffix(r.URL.Path, "/status"):
			mu.Lock()
			taskHits++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"status":     "stopped",
					"exitstatus": "OK",
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
		BaseURL:  srv.URL,
		APIToken: "PVEAPIToken=x",
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	wc := &recordWeCom{}
	store := core.NewStateStore(5 * time.Minute)
	t.Cleanup(store.Close)

	p := NewProvider(ProviderDeps{
		WeCom: wc,
		State: store,
		Instances: []Instance{
			{ID: "home", Name: "Home", Client: client},
		},
		AlertConfig: AlertConfig{Enabled: false},
	})

	userID := "u"
	ctx := context.Background()

	if err := p.OnEnter(ctx, userID); err != nil {
		t.Fatalf("OnEnter() error: %v", err)
	}

	// 进入 VM 菜单
	if handled, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyPVEActionVMMenu}); err != nil || !handled {
		t.Fatalf("HandleEvent(VMMenu) handled=%v err=%v, want handled=true err=nil", handled, err)
	}

	// 选择重启
	if handled, err := p.HandleEvent(ctx, userID, wecom.IncomingMessage{EventKey: wecom.EventKeyPVEVMReboot}); err != nil || !handled {
		t.Fatalf("HandleEvent(Reboot) handled=%v err=%v, want handled=true err=nil", handled, err)
	}

	// 输入 VMID
	if handled, err := p.HandleText(ctx, userID, "100"); err != nil || !handled {
		t.Fatalf("HandleText(VMID) handled=%v err=%v, want handled=true err=nil", handled, err)
	}

	// 确认执行
	if handled, err := p.HandleConfirm(ctx, userID); err != nil || !handled {
		t.Fatalf("HandleConfirm() handled=%v err=%v, want handled=true err=nil", handled, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if rebootHits != 1 {
		t.Fatalf("reboot hits = %d, want 1", rebootHits)
	}
	if taskHits == 0 {
		t.Fatalf("task status hits = %d, want >0", taskHits)
	}
	if len(wc.Texts()) == 0 {
		t.Fatalf("texts empty, want messages")
	}
	if len(wc.Cards()) == 0 {
		t.Fatalf("cards empty, want messages")
	}
}

