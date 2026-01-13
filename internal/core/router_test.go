// Router Provider 分发单元测试。
package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type recordWeCom struct {
	texts []wecom.TextMessage
	cards []wecom.TemplateCardMessage
}

type recordWeComUpdater struct {
	recordWeCom
	updates []templateCardUpdate
}

type recordWeComMenu struct {
	recordWeCom
	menus []wecom.Menu
}

type templateCardUpdate struct {
	ResponseCode string
	ReplaceName  string
}

func (r *recordWeCom) SendText(_ context.Context, msg wecom.TextMessage) error {
	r.texts = append(r.texts, msg)
	return nil
}

func (r *recordWeCom) SendTemplateCard(_ context.Context, msg wecom.TemplateCardMessage) error {
	r.cards = append(r.cards, msg)
	return nil
}

func (r *recordWeComUpdater) UpdateTemplateCardButton(_ context.Context, responseCode string, replaceName string) error {
	r.updates = append(r.updates, templateCardUpdate{ResponseCode: responseCode, ReplaceName: replaceName})
	return nil
}

func (r *recordWeComMenu) CreateMenu(_ context.Context, menu wecom.Menu) error {
	r.menus = append(r.menus, menu)
	return nil
}

type fakeProvider struct {
	key      string
	name     string
	keywords []string

	onEnter   int
	onText    int
	onEvent   int
	onConfirm int

	textHandled    bool
	eventHandled   bool
	confirmHandled bool
}

func (p *fakeProvider) Key() string             { return p.key }
func (p *fakeProvider) DisplayName() string     { return p.name }
func (p *fakeProvider) EntryKeywords() []string { return p.keywords }

func (p *fakeProvider) OnEnter(_ context.Context, _ string) error {
	p.onEnter++
	return nil
}

func (p *fakeProvider) HandleText(_ context.Context, _ string, _ string) (bool, error) {
	p.onText++
	return p.textHandled, nil
}

func (p *fakeProvider) HandleEvent(_ context.Context, _ string, _ wecom.IncomingMessage) (bool, error) {
	p.onEvent++
	return p.eventHandled, nil
}

func (p *fakeProvider) HandleConfirm(_ context.Context, _ string) (bool, error) {
	p.onConfirm++
	return p.confirmHandled, nil
}

func TestRouter_Menu_SendsServiceSelectCard(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{
			&fakeProvider{key: "unraid", name: "Unraid 容器"},
			&fakeProvider{key: "qinglong", name: "青龙(QL)"},
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "text",
		Content:      "菜单",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.cards); got != 1 {
		t.Fatalf("template card count = %d, want 1", got)
	}
	card := rec.cards[0].Card
	mainTitle, ok := card["main_title"].(map[string]interface{})
	if !ok {
		t.Fatalf("main_title missing")
	}
	title, _ := mainTitle["title"].(string)
	if title != "操作菜单" {
		t.Fatalf("card title = %q, want %q", title, "操作菜单")
	}
}

func TestRouter_ClickCoreMenu_SendsServiceSelectCard(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{
			&fakeProvider{key: "unraid", name: "Unraid 容器"},
			&fakeProvider{key: "qinglong", name: "青龙(QL)"},
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "event",
		Event:        "CLICK",
		EventKey:     wecom.EventKeyCoreMenu,
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.cards); got != 1 {
		t.Fatalf("template card count = %d, want 1", got)
	}
}

func TestRouter_DirectKeyword_EntersProvider(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	unraid := &fakeProvider{key: "unraid", name: "Unraid 容器", keywords: []string{"容器"}}

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{
			unraid,
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "text",
		Content:      "容器",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if unraid.onEnter != 1 {
		t.Fatalf("unraid OnEnter hits = %d, want 1", unraid.onEnter)
	}
}

func TestRouter_Help_SendsTextHelp(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "text",
		Content:      "help",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.texts); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
	if got := len(rec.cards); got != 0 {
		t.Fatalf("template card count = %d, want 0", got)
	}
	if !strings.Contains(rec.texts[0].Content, "可用命令") {
		t.Fatalf("help reply = %q, want contains %q", rec.texts[0].Content, "可用命令")
	}
}

func TestRouter_SyncMenu_CallsCreateMenu(t *testing.T) {
	t.Parallel()

	rec := &recordWeComMenu{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "text",
		Content:      "同步菜单",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.menus); got != 1 {
		t.Fatalf("CreateMenu hits = %d, want 1", got)
	}
	if got := len(rec.texts); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
}

func TestRouter_EventPrefix_DispatchesToProvider(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	ql := &fakeProvider{key: "qinglong", name: "青龙(QL)", eventHandled: true}

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{
			ql,
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "event",
		Event:        "template_card_event",
		EventKey:     "qinglong.any.event",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}
	if ql.onEvent != 1 {
		t.Fatalf("provider HandleEvent hits = %d, want 1", ql.onEvent)
	}
}

func TestRouter_Confirm_DispatchesByStateServiceKey(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"
	state := NewStateStore(1 * time.Minute)

	unraid := &fakeProvider{key: "unraid", name: "Unraid 容器", confirmHandled: true}
	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{
			unraid,
		},
		State: state,
	})

	state.Set(userID, ConversationState{
		ServiceKey: "unraid",
		Step:       StepAwaitingConfirm,
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "event",
		Event:        "template_card_event",
		EventKey:     wecom.EventKeyConfirm,
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}
	if unraid.onConfirm != 1 {
		t.Fatalf("provider HandleConfirm hits = %d, want 1", unraid.onConfirm)
	}
}

func TestRouter_TemplateCardEvent_UpdatesButtonByResponseCode(t *testing.T) {
	t.Parallel()

	rec := &recordWeComUpdater{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		Providers: []ServiceProvider{},
		State:     NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		FromUserName: userID,
		MsgType:      "event",
		Event:        "template_card_event",
		EventKey:     wecom.EventKeyCancel,
		ResponseCode: "RC",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.updates); got != 1 {
		t.Fatalf("update calls = %d, want 1", got)
	}
	if rec.updates[0].ResponseCode != "RC" {
		t.Fatalf("response_code = %q, want %q", rec.updates[0].ResponseCode, "RC")
	}
	if rec.updates[0].ReplaceName != "已取消" {
		t.Fatalf("replace_name = %q, want %q", rec.updates[0].ReplaceName, "已取消")
	}
}

func TestRouter_SelfTestPing_AutoReplies(t *testing.T) {
	t.Parallel()

	rec := &recordWeCom{}
	userID := "u"

	r := NewRouter(RouterDeps{
		WeCom: rec,
		AllowedUserID: map[string]struct{}{
			userID: {},
		},
		State: NewStateStore(1 * time.Minute),
	})

	if err := r.HandleMessage(context.Background(), wecom.IncomingMessage{
		ToUserName:   "corp",
		FromUserName: userID,
		MsgType:      "text",
		Content:      "ping",
		MsgID:        "123",
	}); err != nil {
		t.Fatalf("HandleMessage() error: %v", err)
	}

	if got := len(rec.texts); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
	if got := rec.texts[0].ToUser; got != userID {
		t.Fatalf("ToUser = %q, want %q", got, userID)
	}
	if !strings.HasPrefix(rec.texts[0].Content, "pong\nserver_time: ") {
		t.Fatalf("reply = %q, want prefix %q", rec.texts[0].Content, "pong\\nserver_time: ")
	}
	if strings.Contains(rec.texts[0].Content, "\nto: ") {
		t.Fatalf("reply = %q, want no to field", rec.texts[0].Content)
	}
	if !strings.Contains(rec.texts[0].Content, "\nmsg_id: 123") {
		t.Fatalf("reply = %q, want msg_id 123", rec.texts[0].Content)
	}
	if got := len(rec.cards); got != 0 {
		t.Fatalf("template card count = %d, want 0", got)
	}
}
