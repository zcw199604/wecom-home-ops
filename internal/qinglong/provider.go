package qinglong

// provider.go 将青龙(QL) OpenAPI 能力适配为可插拔的企业微信交互 Provider（支持多实例）。
import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/core"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type Instance struct {
	ID     string
	Name   string
	Client *Client
}

type ProviderDeps struct {
	WeCom     core.WeComSender
	State     *core.StateStore
	Instances []Instance
}

type Provider struct {
	wecom     core.WeComSender
	state     *core.StateStore
	instances map[string]Instance
	order     []Instance
}

func NewProvider(deps ProviderDeps) *Provider {
	instances := make(map[string]Instance)
	var order []Instance
	for _, ins := range deps.Instances {
		if !isValidInstanceID(ins.ID) || strings.TrimSpace(ins.Name) == "" || ins.Client == nil {
			continue
		}
		if _, exists := instances[ins.ID]; exists {
			continue
		}
		instances[ins.ID] = ins
		order = append(order, ins)
	}

	return &Provider{
		wecom:     deps.WeCom,
		state:     deps.State,
		instances: instances,
		order:     order,
	}
}

func (p *Provider) Key() string { return "qinglong" }

func (p *Provider) DisplayName() string { return "青龙(QL)" }

func (p *Provider) EntryKeywords() []string {
	return []string{"青龙", "ql", "qinglong"}
}

func (p *Provider) OnEnter(ctx context.Context, userID string) error {
	if len(p.order) == 0 {
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "未配置青龙实例，请先在 config.yaml 中配置 qinglong.instances。",
		})
	}

	p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})

	if len(p.order) == 1 {
		ins := p.order[0]
		p.state.Set(userID, core.ConversationState{
			ServiceKey: p.Key(),
			InstanceID: ins.ID,
		})
		return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewQinglongActionCard(ins.Name),
		})
	}

	var opts []wecom.QinglongInstanceOption
	for _, ins := range p.order {
		opts = append(opts, wecom.QinglongInstanceOption{ID: ins.ID, Name: ins.Name})
	}
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewQinglongInstanceSelectCard(opts),
	})
}

func (p *Provider) HandleText(ctx context.Context, userID, content string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "会话已过期，请输入“青龙”或“菜单”重新开始。",
		})
	}

	ins, ok := p.instances[state.InstanceID]
	if !ok {
		if state.InstanceID != "" {
			_ = p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "青龙实例已变更或不可用，请重新选择实例。",
			})
		}
		return true, p.OnEnter(ctx, userID)
	}

	switch state.Step {
	case core.StepAwaitingQinglongSearchKeyword:
		kw := strings.TrimSpace(content)
		if kw == "" {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "关键词不能为空，请重新输入：",
			})
		}
		return true, p.sendCronListBySearch(ctx, userID, ins, kw)

	case core.StepAwaitingQinglongCronID:
		id, err := strconv.Atoi(strings.TrimSpace(content))
		if err != nil || id <= 0 {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "任务ID不合法，请输入数字ID：",
			})
		}
		cron, err := ins.Client.GetCron(ctx, id)
		if err != nil {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: fmt.Sprintf("获取任务失败：%s", err.Error()),
			})
		}
		state.CronID = cron.ID
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewQinglongCronActionCard(ins.Name, cron.ID, cron.Name),
		})
	default:
		return false, nil
	}
}

func (p *Provider) HandleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) (bool, error) {
	key := strings.TrimSpace(msg.EventKey)
	if key == "" {
		return false, nil
	}

	if key == wecom.EventKeyQinglongMenu {
		return true, p.sendActionMenu(ctx, userID)
	}

	if strings.HasPrefix(key, wecom.EventKeyQinglongInstanceSelectPrefix) {
		insID := strings.TrimPrefix(key, wecom.EventKeyQinglongInstanceSelectPrefix)
		insID = strings.TrimSpace(insID)
		ins, ok := p.instances[insID]
		if !ok {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "实例不存在或不可用，请重新选择。",
			})
		}
		p.state.Set(userID, core.ConversationState{
			ServiceKey: p.Key(),
			InstanceID: ins.ID,
		})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewQinglongActionCard(ins.Name),
		})
	}

	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() {
		return false, nil
	}
	ins, ok := p.instances[state.InstanceID]
	if !ok {
		return true, p.OnEnter(ctx, userID)
	}

	switch key {
	case wecom.EventKeyQinglongActionSwitchInstance:
		p.state.Clear(userID)
		return true, p.OnEnter(ctx, userID)

	case wecom.EventKeyQinglongActionList:
		return true, p.sendCronList(ctx, userID, ins, "", "任务列表")

	case wecom.EventKeyQinglongActionSearch:
		state.Step = core.StepAwaitingQinglongSearchKeyword
		p.state.Set(userID, state)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "请输入任务关键词：",
		})

	case wecom.EventKeyQinglongActionByID:
		state.Step = core.StepAwaitingQinglongCronID
		p.state.Set(userID, state)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "请输入任务ID：",
		})
	}

	if strings.HasPrefix(key, wecom.EventKeyQinglongCronSelectPrefix) {
		idStr := strings.TrimPrefix(key, wecom.EventKeyQinglongCronSelectPrefix)
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if err != nil || id <= 0 {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "任务ID不合法，请返回后重试。",
			})
		}
		cron, err := ins.Client.GetCron(ctx, id)
		if err != nil {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: fmt.Sprintf("获取任务失败：%s", err.Error()),
			})
		}
		state.CronID = cron.ID
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewQinglongCronActionCard(ins.Name, cron.ID, cron.Name),
		})
	}

	switch key {
	case wecom.EventKeyQinglongCronLog:
		if state.CronID <= 0 {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "请先选择任务。",
			})
		}
		logText, err := ins.Client.GetCronLog(ctx, state.CronID)
		if err != nil {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: fmt.Sprintf("获取日志失败：%s", err.Error()),
			})
		}
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: formatLogForWeCom(state.CronID, logText),
		})

	case wecom.EventKeyQinglongCronRun:
		return p.prepareConfirm(ctx, userID, state, core.ActionQinglongRun)
	case wecom.EventKeyQinglongCronEnable:
		return p.prepareConfirm(ctx, userID, state, core.ActionQinglongEnable)
	case wecom.EventKeyQinglongCronDisable:
		return p.prepareConfirm(ctx, userID, state, core.ActionQinglongDisable)
	default:
		return false, nil
	}
}

func (p *Provider) HandleConfirm(ctx context.Context, userID string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() || state.Step != core.StepAwaitingConfirm {
		return false, nil
	}
	ins, ok := p.instances[state.InstanceID]
	if !ok {
		return true, p.OnEnter(ctx, userID)
	}
	if state.CronID <= 0 {
		return true, errors.New("缺少任务ID")
	}

	// 清除“待确认动作”避免重复确认，但保留实例/任务选择提升可用性。
	state.Step = ""
	action := state.Action
	state.Action = ""
	p.state.Set(userID, state)

	start := time.Now()
	var err error
	switch action {
	case core.ActionQinglongRun:
		err = ins.Client.RunCrons(ctx, []int{state.CronID})
	case core.ActionQinglongEnable:
		err = ins.Client.EnableCrons(ctx, []int{state.CronID})
	case core.ActionQinglongDisable:
		err = ins.Client.DisableCrons(ctx, []int{state.CronID})
	default:
		return false, nil
	}
	cost := time.Since(start).Milliseconds()

	if err != nil {
		_ = p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("执行失败（%dms）：%s", cost, err.Error()),
		})
		return true, nil
	}
	_ = p.wecom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: fmt.Sprintf("执行成功（%dms）：%s 任务ID %d", cost, action.DisplayName(), state.CronID),
	})
	return true, nil
}

func (p *Provider) sendActionMenu(ctx context.Context, userID string) error {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() {
		return p.OnEnter(ctx, userID)
	}
	ins, ok := p.instances[state.InstanceID]
	if !ok {
		return p.OnEnter(ctx, userID)
	}
	state.Step = ""
	state.Action = ""
	p.state.Set(userID, state)
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewQinglongActionCard(ins.Name),
	})
}

func (p *Provider) sendCronListBySearch(ctx context.Context, userID string, ins Instance, keyword string) error {
	state, ok := p.state.Get(userID)
	if ok && state.ServiceKey == p.Key() {
		state.Step = ""
		p.state.Set(userID, state)
	}
	return p.sendCronList(ctx, userID, ins, keyword, "搜索结果")
}

func (p *Provider) sendCronList(ctx context.Context, userID string, ins Instance, keyword string, title string) error {
	page, err := ins.Client.ListCrons(ctx, ListCronsParams{
		SearchValue: keyword,
		Page:        1,
		Size:        20,
	})
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("获取任务列表失败：%s", err.Error()),
		})
	}
	if len(page.Data) == 0 {
		msg := "未找到任务。"
		if strings.TrimSpace(keyword) != "" {
			msg = "未找到任务，请更换关键词重试。"
		}
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: msg,
		})
	}

	const maxButtons = 4
	var opts []wecom.QinglongCronOption
	for i, cron := range page.Data {
		if i >= maxButtons {
			break
		}
		opts = append(opts, wecom.QinglongCronOption{
			ID:   cron.ID,
			Name: formatCronButtonText(cron.ID, cron.Name),
		})
	}

	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewQinglongCronListCard(title, ins.Name, opts),
	})
}

func (p *Provider) prepareConfirm(ctx context.Context, userID string, state core.ConversationState, action core.Action) (bool, error) {
	if state.CronID <= 0 {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "请先选择任务。",
		})
	}
	state.Step = core.StepAwaitingConfirm
	state.Action = action
	p.state.Set(userID, state)

	target := fmt.Sprintf("任务ID %d", state.CronID)
	return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewConfirmCard(action.DisplayName(), target),
	})
}

func formatCronButtonText(id int, name string) string {
	text := strings.TrimSpace(name)
	if text == "" {
		text = "任务"
	}
	text = fmt.Sprintf("%d: %s", id, text)
	return truncateRunes(text, 32)
}

func formatLogForWeCom(cronID int, logText string) string {
	content := strings.TrimSpace(logText)
	if content == "" {
		return fmt.Sprintf("任务ID %d 日志为空。", cronID)
	}
	content = tailRunes(content, 1200)
	return fmt.Sprintf("任务ID %d 最近日志：\n%s", cronID, content)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func tailRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…(前略)\n" + string(r[len(r)-max:])
}

func isValidInstanceID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 1 || len(id) > 32 {
		return false
	}
	for i, ch := range id {
		if i == 0 {
			if !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') {
				return false
			}
			continue
		}
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}
