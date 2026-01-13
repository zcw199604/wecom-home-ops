package core

import (
	"context"
	"errors"
	"strings"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type TemplateCardMode string

const (
	TemplateCardModeTemplateCard TemplateCardMode = "template_card"
	TemplateCardModeBoth         TemplateCardMode = "both"
	TemplateCardModeText         TemplateCardMode = "text"
)

type TemplateCardSenderDeps struct {
	Base  WeComSender
	State *StateStore
	Mode  TemplateCardMode
}

// TemplateCardSender 为模板卡片提供“文本兜底 + 序号选择”能力。
// 典型用途：企业微信客户端不支持展示模板卡片时（官方注明微工作台不支持，且存在客户端版本门槛），仍可通过文本完成交互。
type TemplateCardSender struct {
	base  WeComSender
	state *StateStore
	mode  TemplateCardMode
}

func NewTemplateCardSender(deps TemplateCardSenderDeps) *TemplateCardSender {
	return &TemplateCardSender{
		base:  deps.Base,
		state: deps.State,
		mode:  deps.Mode,
	}
}

func (s *TemplateCardSender) SendText(ctx context.Context, msg wecom.TextMessage) error {
	if s.base == nil {
		return errors.New("wecom sender: base 为空")
	}
	s.clearPendingButtons(msg.ToUser)
	return s.base.SendText(ctx, msg)
}

func (s *TemplateCardSender) SendTemplateCard(ctx context.Context, msg wecom.TemplateCardMessage) error {
	if s.base == nil {
		return errors.New("wecom sender: base 为空")
	}

	mode := TemplateCardMode(strings.ToLower(strings.TrimSpace(string(s.mode))))
	if mode == "" {
		mode = TemplateCardModeTemplateCard
	}

	s.clearPendingButtons(msg.ToUser)

	if mode == TemplateCardModeTemplateCard {
		return s.base.SendTemplateCard(ctx, msg)
	}

	text, buttons, ok := wecom.RenderButtonInteractionTextMenu(msg.Card)
	if ok {
		s.setPendingButtons(msg.ToUser, buttons)
	}

	switch mode {
	case TemplateCardModeBoth:
		if err := s.base.SendTemplateCard(ctx, msg); err != nil {
			return err
		}
		if ok {
			return s.base.SendText(ctx, wecom.TextMessage{ToUser: msg.ToUser, Content: text})
		}
		return nil
	case TemplateCardModeText:
		if ok {
			return s.base.SendText(ctx, wecom.TextMessage{ToUser: msg.ToUser, Content: text})
		}
		return s.base.SendText(ctx, wecom.TextMessage{ToUser: msg.ToUser, Content: "（模板卡片已切换为文本模式，但当前卡片无法渲染为文本菜单）"})
	default:
		return s.base.SendTemplateCard(ctx, msg)
	}
}

func (s *TemplateCardSender) clearPendingButtons(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" || s.state == nil {
		return
	}
	state, ok := s.state.Get(userID)
	if !ok || len(state.PendingButtons) == 0 {
		return
	}
	state.PendingButtons = nil
	s.state.Set(userID, state)
}

func (s *TemplateCardSender) setPendingButtons(userID string, buttons []wecom.TemplateCardButton) {
	userID = strings.TrimSpace(userID)
	if userID == "" || s.state == nil || len(buttons) == 0 {
		return
	}
	state, _ := s.state.Get(userID)
	state.PendingButtons = buttons
	s.state.Set(userID, state)
}

func (s *TemplateCardSender) UpdateTemplateCardButton(ctx context.Context, responseCode string, replaceName string) error {
	updater, ok := s.base.(interface {
		UpdateTemplateCardButton(ctx context.Context, responseCode string, replaceName string) error
	})
	if !ok {
		return errors.New("UpdateTemplateCardButton not supported")
	}
	return updater.UpdateTemplateCardButton(ctx, responseCode, replaceName)
}

func (s *TemplateCardSender) CreateMenu(ctx context.Context, menu wecom.Menu) error {
	creator, ok := s.base.(interface {
		CreateMenu(ctx context.Context, menu wecom.Menu) error
	})
	if !ok {
		return errors.New("CreateMenu not supported")
	}
	return creator.CreateMenu(ctx, menu)
}
