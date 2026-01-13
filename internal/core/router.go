// Package core 提供企业微信会话路由、状态机与可插拔服务分发等核心能力。
package core

// router.go 负责企业微信消息入口的鉴权、会话管理与多服务 Provider 分发。
import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type RouterDeps struct {
	WeCom         WeComSender
	AllowedUserID map[string]struct{}
	Providers     []ServiceProvider
	State         *StateStore
}

type Router struct {
	WeCom         WeComSender
	AllowedUserID map[string]struct{}

	state        *StateStore
	providerList []ServiceProvider
	providers    map[string]ServiceProvider
	keywordIndex map[string]string
}

func NewRouter(deps RouterDeps) *Router {
	state := deps.State
	if state == nil {
		state = NewStateStore(30 * time.Minute)
	}

	providers := make(map[string]ServiceProvider)
	var list []ServiceProvider
	for _, p := range deps.Providers {
		if p == nil || strings.TrimSpace(p.Key()) == "" {
			continue
		}
		if _, exists := providers[p.Key()]; exists {
			continue
		}
		providers[p.Key()] = p
		list = append(list, p)
	}

	keywordIndex := make(map[string]string)
	for _, p := range list {
		for _, k := range p.EntryKeywords() {
			kk := normalizeKeyword(k)
			if kk == "" {
				continue
			}
			keywordIndex[kk] = p.Key()
		}
	}

	return &Router{
		WeCom:         deps.WeCom,
		AllowedUserID: deps.AllowedUserID,
		state:         state,
		providerList:  list,
		providers:     providers,
		keywordIndex:  keywordIndex,
	}
}

func (r *Router) HandleMessage(ctx context.Context, msg wecom.IncomingMessage) error {
	userID := strings.TrimSpace(msg.FromUserName)
	if userID == "" {
		return nil
	}
	if _, ok := r.AllowedUserID[userID]; !ok {
		_ = r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "无权限：该账号未加入白名单。",
		})
		return nil
	}

	switch msg.MsgType {
	case "text":
		return r.handleText(ctx, userID, strings.TrimSpace(msg.Content))
	case "event":
		return r.handleEvent(ctx, userID, msg)
	default:
		_ = r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "暂不支持的消息类型。",
		})
		return nil
	}
}

func (r *Router) handleText(ctx context.Context, userID string, content string) error {
	if content == "" {
		return nil
	}

	normalized := normalizeKeyword(content)
	if isMenuKeyword(normalized) {
		r.state.Clear(userID)
		return r.sendServiceMenu(ctx, userID)
	}

	if providerKey, ok := r.keywordIndex[normalized]; ok {
		r.state.Clear(userID)
		r.state.Set(userID, ConversationState{ServiceKey: providerKey})
		return r.enterProvider(ctx, userID, providerKey)
	}

	state, ok := r.state.Get(userID)
	if ok && strings.TrimSpace(state.ServiceKey) != "" {
		if p, ok := r.providers[state.ServiceKey]; ok {
			handled, err := p.HandleText(ctx, userID, content)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
		}
	}

	return r.WeCom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: "请输入“菜单”打开操作菜单。",
	})
}

func (r *Router) handleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) error {
	if msg.Event == "enter_agent" {
		r.state.Clear(userID)
		return r.sendServiceMenu(ctx, userID)
	}

	if msg.Event != "template_card_event" {
		return nil
	}

	key := strings.TrimSpace(msg.EventKey)
	if key == "" {
		return nil
	}

	switch key {
	case wecom.EventKeyConfirm:
		state, ok := r.state.Get(userID)
		if !ok || state.Step != StepAwaitingConfirm || strings.TrimSpace(state.ServiceKey) == "" {
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "会话已过期，请输入“菜单”重新开始。",
			})
		}

		p, ok := r.providers[state.ServiceKey]
		if !ok {
			r.state.Clear(userID)
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "服务不可用，请输入“菜单”重新开始。",
			})
		}

		handled, err := p.HandleConfirm(ctx, userID)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "当前无可确认的操作，请输入“菜单”重新开始。",
		})

	case wecom.EventKeyCancel:
		r.state.Clear(userID)
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "已取消。",
		})
	}

	if strings.HasPrefix(key, wecom.EventKeyServiceSelectPrefix) {
		serviceKey := strings.TrimPrefix(key, wecom.EventKeyServiceSelectPrefix)
		r.state.Clear(userID)
		r.state.Set(userID, ConversationState{ServiceKey: serviceKey})
		return r.enterProvider(ctx, userID, serviceKey)
	}

	if serviceKey := r.providerKeyFromEventKey(key); serviceKey != "" {
		p := r.providers[serviceKey]
		handled, err := p.HandleEvent(ctx, userID, msg)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}

	if state, ok := r.state.Get(userID); ok && strings.TrimSpace(state.ServiceKey) != "" {
		if p, ok := r.providers[state.ServiceKey]; ok {
			handled, err := p.HandleEvent(ctx, userID, msg)
			if err != nil {
				return err
			}
			if handled {
				return nil
			}
		}
	}

	return nil
}

func (r *Router) enterProvider(ctx context.Context, userID, key string) error {
	p, ok := r.providers[key]
	if !ok {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "服务不可用，请输入“菜单”重新开始。",
		})
	}
	return p.OnEnter(ctx, userID)
}

func (r *Router) providerKeyFromEventKey(eventKey string) string {
	part, _, ok := strings.Cut(eventKey, ".")
	if !ok || part == "" {
		return ""
	}
	if _, exists := r.providers[part]; !exists {
		return ""
	}
	return part
}

func (r *Router) sendServiceMenu(ctx context.Context, userID string) error {
	var opts []wecom.ServiceOption
	for _, p := range r.providerList {
		if p == nil {
			continue
		}
		if strings.TrimSpace(p.Key()) == "" || strings.TrimSpace(p.DisplayName()) == "" {
			continue
		}
		opts = append(opts, wecom.ServiceOption{Key: p.Key(), Name: p.DisplayName()})
	}

	if len(opts) == 0 {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "未配置可用服务。",
		})
	}

	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Key < opts[j].Key })

	return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewServiceSelectCard(opts),
	})
}

func normalizeKeyword(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func isMenuKeyword(normalized string) bool {
	switch normalized {
	case "help", "menu", "菜单":
		return true
	default:
		return false
	}
}
