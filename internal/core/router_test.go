// Router Provider 分发单元测试。
package core

import (
	"context"
	"testing"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type recordWeCom struct {
	texts []wecom.TextMessage
	cards []wecom.TemplateCardMessage
}

func (r *recordWeCom) SendText(_ context.Context, msg wecom.TextMessage) error {
	r.texts = append(r.texts, msg)
	return nil
}

func (r *recordWeCom) SendTemplateCard(_ context.Context, msg wecom.TemplateCardMessage) error {
	r.cards = append(r.cards, msg)
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
