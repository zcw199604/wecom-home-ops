// Package core 提供企业微信会话路由、状态机与可插拔服务分发等核心能力。
package core

// router.go 负责企业微信消息入口的鉴权、会话管理与多服务 Provider 分发。
import (
	"context"
	"log/slog"
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

type templateCardUpdater interface {
	UpdateTemplateCardButton(ctx context.Context, responseCode string, replaceName string) error
}

type menuCreator interface {
	CreateMenu(ctx context.Context, menu wecom.Menu) error
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
		if err := r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "无权限：该账号未加入白名单。",
		}); err != nil {
			slog.Error("wecom 发送无权限提示失败",
				"error", err,
				"user_id", userID,
			)
		}
		return nil
	}

	if msg.MsgType == "text" {
		if isSelfTestKeyword(normalizeCommandKeyword(msg.Content)) {
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: buildSelfTestReply(msg),
			})
		}
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

	keyword := normalizeCommandKeyword(content)

	if state, ok := r.state.Get(userID); ok && strings.TrimSpace(state.ServiceKey) != "" && state.Step == StepAwaitingConfirm {
		if isConfirmKeyword(keyword) {
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
		}
		if isCancelKeyword(keyword) {
			r.state.Clear(userID)
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "已取消。",
			})
		}
	}

	if isHelpKeyword(keyword) {
		return r.sendHelp(ctx, userID)
	}
	if isMenuSyncKeyword(keyword) {
		return r.syncWeComMenu(ctx, userID)
	}

	if isMenuKeyword(keyword) {
		r.state.Clear(userID)
		return r.sendServiceMenu(ctx, userID)
	}

	normalized := normalizeKeyword(content)
	if providerKey, ok := r.keywordIndex[normalized]; ok {
		r.state.Clear(userID)
		r.state.Set(userID, ConversationState{ServiceKey: providerKey})
		return r.enterProvider(ctx, userID, providerKey)
	}
	if strings.HasPrefix(strings.TrimSpace(content), "/") {
		if providerKey, ok := r.keywordIndex[keyword]; ok {
			r.state.Clear(userID)
			r.state.Set(userID, ConversationState{ServiceKey: providerKey})
			return r.enterProvider(ctx, userID, providerKey)
		}
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
		Content: "请输入“菜单”打开操作菜单，或输入“帮助”查看可用命令。",
	})
}

func (r *Router) handleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) error {
	event := strings.ToLower(strings.TrimSpace(msg.Event))
	if event == "enter_agent" {
		r.state.Clear(userID)
		return r.sendServiceMenu(ctx, userID)
	}

	isTemplateCardEvent := event == "template_card_event"
	isClickEvent := event == "click"
	if !isTemplateCardEvent && !isClickEvent {
		return nil
	}

	key := strings.TrimSpace(msg.EventKey)
	if isTemplateCardEvent {
		if updater, ok := r.WeCom.(templateCardUpdater); ok {
			responseCode := strings.TrimSpace(msg.ResponseCode)
			if responseCode != "" {
				if err := updater.UpdateTemplateCardButton(ctx, responseCode, r.templateCardReplaceName(key)); err != nil {
					slog.Error("wecom 更新模板卡片按钮失败",
						"error", err,
						"user_id", userID,
						"event_key", key,
						"response_code_len", len(responseCode),
					)
				}
			}
		}
	}
	if key == "" {
		return nil
	}

	switch key {
	case wecom.EventKeyCoreMenu:
		r.state.Clear(userID)
		return r.sendServiceMenu(ctx, userID)
	case wecom.EventKeyCoreHelp:
		return r.sendHelp(ctx, userID)
	case wecom.EventKeyCoreSelfTest:
		return r.WeCom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: buildSelfTestReply(msg)})
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

	if strings.HasPrefix(key, "unraid.") {
		if _, ok := r.providers["unraid"]; !ok {
			return r.sendServiceUnavailable(ctx, userID, "unraid")
		}
	}
	if strings.HasPrefix(key, "qinglong.") {
		if _, ok := r.providers["qinglong"]; !ok {
			return r.sendServiceUnavailable(ctx, userID, "qinglong")
		}
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

	if isClickEvent {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "未识别的菜单操作，请发送“同步菜单”更新，或输入“帮助”。",
		})
	}
	return nil
}

func (r *Router) templateCardReplaceName(eventKey string) string {
	switch eventKey {
	case wecom.EventKeyConfirm:
		return "已确认"
	case wecom.EventKeyCancel:
		return "已取消"
	default:
		return "已处理"
	}
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

func normalizeCommandKeyword(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	token := strings.TrimSpace(fields[0])
	token = strings.TrimPrefix(token, "/")
	token = strings.TrimPrefix(token, "!")
	return normalizeKeyword(token)
}

func isMenuKeyword(normalized string) bool {
	switch normalized {
	case "menu", "菜单":
		return true
	default:
		return false
	}
}

func isHelpKeyword(normalized string) bool {
	switch normalized {
	case "help", "帮助", "?", "？":
		return true
	default:
		return false
	}
}

func isMenuSyncKeyword(normalized string) bool {
	switch normalized {
	case "同步菜单", "更新菜单", "syncmenu", "sync-menu":
		return true
	default:
		return false
	}
}

func isSelfTestKeyword(normalized string) bool {
	switch normalized {
	case "ping", "自检":
		return true
	default:
		return false
	}
}

func isConfirmKeyword(normalized string) bool {
	switch normalized {
	case "确认", "confirm", "yes", "y":
		return true
	default:
		return false
	}
}

func isCancelKeyword(normalized string) bool {
	switch normalized {
	case "取消", "cancel", "no", "n":
		return true
	default:
		return false
	}
}

func (r *Router) sendServiceUnavailable(ctx context.Context, userID string, serviceKey string) error {
	msg := "服务未启用：" + serviceKey + "。请检查 config.yaml 配置后重启服务。"
	switch serviceKey {
	case "unraid":
		msg = "Unraid 服务未启用：请在 config.yaml 配置 unraid.endpoint 与 unraid.api_key 后重启服务。"
	case "qinglong":
		msg = "青龙(QL) 服务未启用：请在 config.yaml 配置 qinglong.instances 后重启服务。"
	}
	return r.WeCom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: msg})
}

func (r *Router) sendHelp(ctx context.Context, userID string) error {
	services := make([]string, 0, len(r.providerList))
	for _, p := range r.providerList {
		if p == nil {
			continue
		}
		name := strings.TrimSpace(p.DisplayName())
		if name == "" {
			continue
		}
		services = append(services, name)
	}
	sort.Strings(services)

	var b strings.Builder
	b.WriteString("可用命令：")
	b.WriteString("\n- 菜单 /menu：打开操作菜单")
	b.WriteString("\n- 帮助 /help：查看帮助")
	b.WriteString("\n- 自检 /ping：收发自检（pong）")
	b.WriteString("\n- 同步菜单：创建/覆盖企业微信应用自定义菜单（管理员功能）")
	if len(services) > 0 {
		b.WriteString("\n\n已启用服务：")
		for _, s := range services {
			b.WriteString("\n- ")
			b.WriteString(s)
		}
	}
	b.WriteString("\n\n提示：也可以直接点击应用底部自定义菜单触发常用操作。")

	return r.WeCom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: b.String()})
}

func (r *Router) syncWeComMenu(ctx context.Context, userID string) error {
	c, ok := r.WeCom.(menuCreator)
	if !ok {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "当前发送端不支持同步菜单。",
		})
	}
	if err := c.CreateMenu(ctx, wecom.DefaultMenu()); err != nil {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "同步菜单失败：" + err.Error(),
		})
	}
	return r.WeCom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: "已同步应用自定义菜单。可重新进入应用会话查看底部菜单。",
	})
}

func buildSelfTestReply(msg wecom.IncomingMessage) string {
	var b strings.Builder
	b.WriteString("pong")
	b.WriteString("\nserver_time: ")
	b.WriteString(time.Now().Format(time.RFC3339))
	if v := strings.TrimSpace(msg.FromUserName); v != "" {
		b.WriteString("\nfrom: ")
		b.WriteString(v)
	}
	if v := strings.TrimSpace(msg.MsgType); v != "" {
		b.WriteString("\nmsg_type: ")
		b.WriteString(v)
	}
	if v := strings.TrimSpace(msg.MsgID); v != "" {
		b.WriteString("\nmsg_id: ")
		b.WriteString(v)
	}
	return b.String()
}
