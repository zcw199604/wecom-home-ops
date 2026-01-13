package core

import (
	"context"
	"testing"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

func TestTemplateCardSender_ModeText_SendsTextAndStoresPendingButtons(t *testing.T) {
	t.Parallel()

	base := &recordWeCom{}
	state := NewStateStore(1 * time.Minute)
	sender := NewTemplateCardSender(TemplateCardSenderDeps{
		Base:  base,
		State: state,
		Mode:  TemplateCardModeText,
	})

	userID := "u"
	if err := sender.SendTemplateCard(context.Background(), wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewUnraidViewCard(),
	}); err != nil {
		t.Fatalf("SendTemplateCard() error: %v", err)
	}

	if got := len(base.cards); got != 0 {
		t.Fatalf("template card count = %d, want 0", got)
	}
	if got := len(base.texts); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
	st, ok := state.Get(userID)
	if !ok || len(st.PendingButtons) == 0 {
		t.Fatalf("pending buttons missing")
	}
}

func TestTemplateCardSender_ModeBoth_SendsCardAndText(t *testing.T) {
	t.Parallel()

	base := &recordWeCom{}
	state := NewStateStore(1 * time.Minute)
	sender := NewTemplateCardSender(TemplateCardSenderDeps{
		Base:  base,
		State: state,
		Mode:  TemplateCardModeBoth,
	})

	userID := "u"
	if err := sender.SendTemplateCard(context.Background(), wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewUnraidViewCard(),
	}); err != nil {
		t.Fatalf("SendTemplateCard() error: %v", err)
	}

	if got := len(base.cards); got != 1 {
		t.Fatalf("template card count = %d, want 1", got)
	}
	if got := len(base.texts); got != 1 {
		t.Fatalf("text message count = %d, want 1", got)
	}
}

func TestTemplateCardSender_SendText_ClearsPendingButtons(t *testing.T) {
	t.Parallel()

	base := &recordWeCom{}
	state := NewStateStore(1 * time.Minute)
	sender := NewTemplateCardSender(TemplateCardSenderDeps{
		Base:  base,
		State: state,
		Mode:  TemplateCardModeText,
	})

	userID := "u"
	state.Set(userID, ConversationState{
		PendingButtons: []wecom.TemplateCardButton{{Text: "x", Key: "k"}},
	})

	if err := sender.SendText(context.Background(), wecom.TextMessage{ToUser: userID, Content: "hi"}); err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	st, ok := state.Get(userID)
	if !ok {
		t.Fatalf("state missing")
	}
	if len(st.PendingButtons) != 0 {
		t.Fatalf("pending buttons not cleared")
	}
}
