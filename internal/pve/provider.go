package pve

// provider.go 将 PVE 能力适配为可插拔的企业微信交互 Provider（支持多实例）。
import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	AlertConfig AlertConfig
	Alerts      *AlertManager
}

type Provider struct {
	wecom  core.WeComSender
	state  *core.StateStore
	alerts *AlertManager

	alertCfg AlertConfig

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

	sort.SliceStable(order, func(i, j int) bool { return order[i].ID < order[j].ID })

	return &Provider{
		wecom:     deps.WeCom,
		state:     deps.State,
		alerts:    deps.Alerts,
		alertCfg:  deps.AlertConfig,
		instances: instances,
		order:     order,
	}
}

func (p *Provider) Key() string { return "pve" }

func (p *Provider) DisplayName() string { return "PVE" }

func (p *Provider) EntryKeywords() []string {
	return []string{"pve", "proxmox"}
}

func (p *Provider) OnEnter(ctx context.Context, userID string) error {
	if len(p.order) == 0 {
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "未配置 PVE 实例，请先在 config.yaml 配置 pve.instances。",
		})
	}

	p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})

	if len(p.order) == 1 {
		ins := p.order[0]
		p.state.Set(userID, core.ConversationState{
			ServiceKey:  p.Key(),
			InstanceID:  ins.ID,
			PVEGuestType: "",
			PVEGuestID:   0,
			PVENode:      "",
			PVEGuestName: "",
		})
		return p.sendActionMenu(ctx, userID, ins)
	}

	var opts []wecom.PVEInstanceOption
	for _, ins := range p.order {
		opts = append(opts, wecom.PVEInstanceOption{ID: ins.ID, Name: ins.Name})
	}
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewPVEInstanceSelectCard(opts),
	})
}

func (p *Provider) HandleText(ctx context.Context, userID, content string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() {
		return false, nil
	}

	switch state.Step {
	case core.StepAwaitingPVEGuestQuery:
		ins, ok := p.instanceFromState(state)
		if !ok {
			p.state.Clear(userID)
			return true, p.OnEnter(ctx, userID)
		}
		return true, p.handleGuestQuery(ctx, userID, ins, state, content)
	default:
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "请输入“菜单”打开操作菜单，或点击卡片按钮继续。",
		})
	}
}

func (p *Provider) HandleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) (bool, error) {
	key := strings.TrimSpace(msg.EventKey)
	if key == "" {
		return false, nil
	}

	if strings.HasPrefix(key, wecom.EventKeyPVEInstanceSelectPrefix) {
		id := strings.TrimPrefix(key, wecom.EventKeyPVEInstanceSelectPrefix)
		ins, ok := p.instances[id]
		if !ok {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "实例不可用，请重新选择。"})
		}
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key(), InstanceID: ins.ID})
		return true, p.sendActionMenu(ctx, userID, ins)
	}

	state, _ := p.state.Get(userID)
	if state.ServiceKey != p.Key() {
		state = core.ConversationState{ServiceKey: p.Key()}
	}

	switch key {
	case wecom.EventKeyPVEMenu:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.sendActionMenu(ctx, userID, ins)

	case wecom.EventKeyPVEActionSwitchInstance:
		p.state.Clear(userID)
		return true, p.OnEnter(ctx, userID)

	case wecom.EventKeyPVEActionOverview:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.sendOverview(ctx, userID, ins)

	case wecom.EventKeyPVEActionVMMenu:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewPVEVMActionCard(ins.Name),
		})

	case wecom.EventKeyPVEActionLXCMenu:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewPVELXCActionCard(ins.Name),
		})

	case wecom.EventKeyPVEActionAlertStatus:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		state.Step = ""
		p.state.Set(userID, state)
		return true, p.sendAlertStatus(ctx, userID, ins)

	case wecom.EventKeyPVEActionAlertMute:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		if p.alerts == nil || !p.alertCfg.Enabled {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "告警未启用，请在 config.yaml 打开 pve.alert.enabled 并重启服务。"})
		}
		until := time.Now().Add(p.alertCfg.MuteFor)
		p.alerts.Mute(ins.ID, until)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("已静默告警，直到 %s。", until.Format("2006-01-02 15:04:05")),
		})

	case wecom.EventKeyPVEActionAlertUnmute:
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		if p.alerts == nil || !p.alertCfg.Enabled {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "告警未启用。"})
		}
		p.alerts.Unmute(ins.ID)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "已解除静默。"})

	case wecom.EventKeyPVEVMStart:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeQEMU, core.ActionPVEStart)
	case wecom.EventKeyPVEVMShutdown:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeQEMU, core.ActionPVEShutdown)
	case wecom.EventKeyPVEVMReboot:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeQEMU, core.ActionPVEReboot)
	case wecom.EventKeyPVEVMStop:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeQEMU, core.ActionPVEStop)

	case wecom.EventKeyPVELXCStart:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeLXC, core.ActionPVEStart)
	case wecom.EventKeyPVELXCShutdown:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeLXC, core.ActionPVEShutdown)
	case wecom.EventKeyPVELXCReboot:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeLXC, core.ActionPVEReboot)
	case wecom.EventKeyPVELXCStop:
		return true, p.prepareGuestQuery(ctx, userID, state, GuestTypeLXC, core.ActionPVEStop)
	}

	if strings.HasPrefix(key, wecom.EventKeyPVEGuestSelectPrefix) {
		ins, ok := p.instanceFromState(state)
		if !ok {
			return true, p.OnEnter(ctx, userID)
		}
		return true, p.handleGuestSelect(ctx, userID, ins, state, key)
	}

	return false, nil
}

func (p *Provider) HandleConfirm(ctx context.Context, userID string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() || state.Step != core.StepAwaitingConfirm {
		return false, nil
	}

	ins, ok := p.instanceFromState(state)
	if !ok {
		p.state.Clear(userID)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "会话已过期，请重新进入 PVE 菜单。"})
	}

	guestType := GuestType(strings.TrimSpace(state.PVEGuestType))
	if !guestType.IsValid() || state.PVEGuestID <= 0 || strings.TrimSpace(state.PVENode) == "" {
		p.state.Clear(userID)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "缺少目标信息，请重新选择。"})
	}

	action, ok := coreActionToGuestAction(state.Action)
	if !ok {
		p.state.Clear(userID)
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "未知动作，请重新选择。"})
	}

	target := fmt.Sprintf("%s %d（%s）", strings.ToUpper(guestType.String()), state.PVEGuestID, state.PVENode)
	if n := strings.TrimSpace(state.PVEGuestName); n != "" {
		target = fmt.Sprintf("%s %d（%s | %s）", strings.ToUpper(guestType.String()), state.PVEGuestID, state.PVENode, n)
	}

	p.state.Clear(userID)

	upid, err := ins.Client.GuestAction(ctx, state.PVENode, guestType, state.PVEGuestID, action)
	if err != nil {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("%s失败：%s", state.Action.DisplayName(), err.Error()),
		})
	}

	_ = p.wecom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: fmt.Sprintf("已提交：%s %s\nUPID: %s", state.Action.DisplayName(), target, upid),
	})

	final, waitErr := waitTask(ctx, ins.Client, state.PVENode, upid, 90*time.Second)
	if waitErr != nil {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("任务状态获取失败（UPID: %s）：%s", upid, waitErr.Error()),
		})
	}

	if strings.TrimSpace(final.ExitStatus) != "" && strings.ToUpper(strings.TrimSpace(final.ExitStatus)) != "OK" {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("执行完成但状态异常：%s\n目标：%s\nUPID: %s", final.ExitStatus, target, upid),
		})
	}

	return true, p.wecom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: fmt.Sprintf("执行成功：%s %s\nUPID: %s", state.Action.DisplayName(), target, upid),
	})
}

func (p *Provider) prepareGuestQuery(ctx context.Context, userID string, state core.ConversationState, guestType GuestType, action core.Action) error {
	_, ok := p.instanceFromState(state)
	if !ok {
		return p.OnEnter(ctx, userID)
	}

	state.ServiceKey = p.Key()
	state.Step = core.StepAwaitingPVEGuestQuery
	state.Action = action
	state.PVEGuestType = guestType.String()
	state.PVEGuestID = 0
	state.PVENode = ""
	state.PVEGuestName = ""
	p.state.Set(userID, state)

	kind := "VM"
	if guestType == GuestTypeLXC {
		kind = "LXC"
	}
	return p.wecom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: fmt.Sprintf("已选择动作：%s（%s）\n请输入 VMID 或名称关键词：", action.DisplayName(), kind),
	})
}

func (p *Provider) handleGuestQuery(ctx context.Context, userID string, ins Instance, state core.ConversationState, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	guestType := GuestType(strings.TrimSpace(state.PVEGuestType))
	if !guestType.IsValid() || strings.TrimSpace(string(state.Action)) == "" {
		state.Step = ""
		p.state.Set(userID, state)
		return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewPVEActionCard(p.actionCardOptions(ins))})
	}

	if vmid, err := strconv.Atoi(content); err == nil && vmid > 0 {
		res, ok := findGuestByVMID(ctx, ins.Client, guestType, vmid)
		if !ok {
			return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "未找到目标，请确认 VMID 或改用名称关键词。"})
		}
		return p.prepareConfirm(ctx, userID, state, ins, guestType, res)
	}

	list, err := ins.Client.ListClusterResources(ctx, "vm")
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "查询失败：" + err.Error()})
	}

	kw := strings.ToLower(strings.TrimSpace(content))
	var hits []ClusterResource
	for _, r := range list {
		if GuestType(strings.TrimSpace(r.Type)) != guestType {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(r.Name))
		if kw != "" && strings.Contains(name, kw) {
			hits = append(hits, r)
		}
	}
	if len(hits) == 0 {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "未找到目标，请更换关键词重试。"})
	}
	if len(hits) == 1 {
		return p.prepareConfirm(ctx, userID, state, ins, guestType, hits[0])
	}

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].VMID < hits[j].VMID })

	const maxButtons = 4
	var opts []wecom.PVEGuestOption
	for i, r := range hits {
		if i >= maxButtons {
			break
		}
		text := fmt.Sprintf("%d: %s", r.VMID, strings.TrimSpace(r.Name))
		opts = append(opts, wecom.PVEGuestOption{
			Text:      truncateRunes(text, 32),
			GuestType: guestType.String(),
			VMID:      r.VMID,
			Node:      r.Node,
		})
	}

	state.Step = ""
	p.state.Set(userID, state)
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewPVEGuestSelectCard("搜索结果", ins.Name, opts),
	})
}

func (p *Provider) handleGuestSelect(ctx context.Context, userID string, ins Instance, state core.ConversationState, key string) error {
	suffix := strings.TrimPrefix(key, wecom.EventKeyPVEGuestSelectPrefix)
	parts := strings.Split(suffix, ".")
	if len(parts) != 3 {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "选择无效，请重新选择。"})
	}
	guestType := GuestType(strings.TrimSpace(parts[0]))
	vmid, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	node := strings.TrimSpace(parts[2])
	if err != nil || vmid <= 0 || !guestType.IsValid() || node == "" {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "选择无效，请重新选择。"})
	}

	// 确保目标仍存在，顺便拿到名称用于回显。
	res, ok := findGuestByVMID(ctx, ins.Client, guestType, vmid)
	if !ok {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "未找到目标，请重新搜索。"})
	}
	res.Node = node
	return p.prepareConfirm(ctx, userID, state, ins, guestType, res)
}

func (p *Provider) prepareConfirm(ctx context.Context, userID string, state core.ConversationState, ins Instance, guestType GuestType, res ClusterResource) error {
	state.ServiceKey = p.Key()
	state.Step = core.StepAwaitingConfirm
	state.PVEGuestType = guestType.String()
	state.PVEGuestID = res.VMID
	state.PVENode = strings.TrimSpace(res.Node)
	state.PVEGuestName = strings.TrimSpace(res.Name)
	p.state.Set(userID, state)

	target := fmt.Sprintf("%s %d（%s）", strings.ToUpper(guestType.String()), res.VMID, res.Node)
	if name := strings.TrimSpace(res.Name); name != "" {
		target = fmt.Sprintf("%s %d（%s | %s）", strings.ToUpper(guestType.String()), res.VMID, res.Node, name)
	}
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewConfirmCard(state.Action.DisplayName(), target),
	})
}

func (p *Provider) sendActionMenu(ctx context.Context, userID string, ins Instance) error {
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewPVEActionCard(p.actionCardOptions(ins)),
	})
}

func (p *Provider) actionCardOptions(ins Instance) wecom.PVEActionCardOptions {
	opts := wecom.PVEActionCardOptions{
		InstanceName:       ins.Name,
		ShowSwitchInstance: len(p.order) > 1,
		ShowAlertActions:   p.alertCfg.Enabled,
	}
	if !p.alertCfg.Enabled {
		return opts
	}
	if p.alerts != nil {
		if until, ok := p.alerts.MuteUntil(ins.ID); ok {
			opts.AlertMuted = true
			opts.AlertDesc = "告警：已静默至 " + until.Format("01-02 15:04")
			return opts
		}
	}
	opts.AlertDesc = "告警：已启用"
	return opts
}

func (p *Provider) sendOverview(ctx context.Context, userID string, ins Instance) error {
	nodes, err := ins.Client.ListClusterResources(ctx, "node")
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "获取节点信息失败：" + err.Error()})
	}
	storages, err := ins.Client.ListClusterResources(ctx, "storage")
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "获取存储信息失败：" + err.Error()})
	}

	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Node < nodes[j].Node })
	sort.SliceStable(storages, func(i, j int) bool {
		li := usagePercent(storages[i].Disk, storages[i].MaxDisk)
		lj := usagePercent(storages[j].Disk, storages[j].MaxDisk)
		return li > lj
	})

	var b strings.Builder
	b.WriteString("PVE 资源概览")
	if strings.TrimSpace(ins.Name) != "" {
		b.WriteString("（")
		b.WriteString(strings.TrimSpace(ins.Name))
		b.WriteString("）")
	}
	b.WriteString("\n\n节点：")
	for i, n := range nodes {
		if i >= 8 {
			break
		}
		cpu := n.CPU * 100
		mem := usagePercent(n.Mem, n.MaxMem)
		name := strings.TrimSpace(n.Node)
		if name == "" {
			name = strings.TrimSpace(n.Name)
		}
		if name == "" {
			name = "(unknown)"
		}
		status := strings.TrimSpace(n.Status)
		if status == "" {
			status = "unknown"
		}
		b.WriteString(fmt.Sprintf("\n- %s [%s] CPU %.0f%% MEM %.0f%%", name, status, cpu, mem))
	}

	b.WriteString("\n\n存储：")
	for i, s := range storages {
		if i >= 8 {
			break
		}
		usage := usagePercent(s.Disk, s.MaxDisk)
		name := strings.TrimSpace(s.Storage)
		if name == "" {
			name = strings.TrimSpace(s.Name)
		}
		if strings.TrimSpace(s.Node) != "" && name != "" {
			name = s.Node + "/" + name
		}
		if name == "" {
			name = "(unknown)"
		}
		b.WriteString(fmt.Sprintf("\n- %s %.0f%%", name, usage))
	}

	return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: b.String()})
}

func (p *Provider) sendAlertStatus(ctx context.Context, userID string, ins Instance) error {
	if !p.alertCfg.Enabled {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "告警未启用（pve.alert.enabled=false）。"})
	}

	var b strings.Builder
	b.WriteString("PVE 告警状态")
	if strings.TrimSpace(ins.Name) != "" {
		b.WriteString("（")
		b.WriteString(strings.TrimSpace(ins.Name))
		b.WriteString("）")
	}
	b.WriteString(fmt.Sprintf("\n阈值：CPU≥%.0f%% MEM≥%.0f%% 存储≥%.0f%%",
		p.alertCfg.CPUUsageThreshold, p.alertCfg.MemUsageThreshold, p.alertCfg.StorageUsageThreshold))
	b.WriteString("\n轮询：")
	b.WriteString(p.alertCfg.Interval.String())
	b.WriteString(" | 冷却：")
	b.WriteString(p.alertCfg.Cooldown.String())

	if p.alerts != nil {
		if until, ok := p.alerts.MuteUntil(ins.ID); ok {
			b.WriteString("\n静默：是（至 ")
			b.WriteString(until.Format("2006-01-02 15:04:05"))
			b.WriteString("）")
		} else {
			b.WriteString("\n静默：否")
		}
	}

	// 简单展示当前是否超阈值（不存储历史）。
	nodes, err := ins.Client.ListClusterResources(ctx, "node")
	if err == nil && len(nodes) > 0 {
		var cpuHits, memHits []string
		for _, n := range nodes {
			if strings.TrimSpace(n.Node) == "" {
				continue
			}
			cpu := n.CPU * 100
			mem := usagePercent(n.Mem, n.MaxMem)
			if cpu >= p.alertCfg.CPUUsageThreshold {
				cpuHits = append(cpuHits, fmt.Sprintf("%s %.0f%%", n.Node, cpu))
			}
			if mem >= p.alertCfg.MemUsageThreshold {
				memHits = append(memHits, fmt.Sprintf("%s %.0f%%", n.Node, mem))
			}
		}
		if len(cpuHits) > 0 {
			sort.Strings(cpuHits)
			b.WriteString("\nCPU 超阈值：")
			b.WriteString(strings.Join(limitStrings(cpuHits, 6), "；"))
		}
		if len(memHits) > 0 {
			sort.Strings(memHits)
			b.WriteString("\n内存超阈值：")
			b.WriteString(strings.Join(limitStrings(memHits, 6), "；"))
		}
	}

	storages, err := ins.Client.ListClusterResources(ctx, "storage")
	if err == nil && len(storages) > 0 {
		var hits []string
		for _, s := range storages {
			if strings.TrimSpace(s.Storage) == "" || s.MaxDisk <= 0 {
				continue
			}
			u := usagePercent(s.Disk, s.MaxDisk)
			if u >= p.alertCfg.StorageUsageThreshold {
				name := s.Storage
				if strings.TrimSpace(s.Node) != "" {
					name = s.Node + "/" + s.Storage
				}
				hits = append(hits, fmt.Sprintf("%s %.0f%%", name, u))
			}
		}
		if len(hits) > 0 {
			sort.Strings(hits)
			b.WriteString("\n存储超阈值：")
			b.WriteString(strings.Join(limitStrings(hits, 8), "；"))
		}
	}

	return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: b.String()})
}

func (p *Provider) instanceFromState(state core.ConversationState) (Instance, bool) {
	if strings.TrimSpace(state.InstanceID) == "" {
		if len(p.order) == 1 {
			return p.order[0], true
		}
		return Instance{}, false
	}
	ins, ok := p.instances[state.InstanceID]
	return ins, ok
}

func coreActionToGuestAction(a core.Action) (GuestAction, bool) {
	switch a {
	case core.ActionPVEStart:
		return GuestActionStart, true
	case core.ActionPVEShutdown:
		return GuestActionShutdown, true
	case core.ActionPVEReboot:
		return GuestActionReboot, true
	case core.ActionPVEStop:
		return GuestActionStop, true
	default:
		return "", false
	}
}

func findGuestByVMID(ctx context.Context, c *Client, guestType GuestType, vmid int) (ClusterResource, bool) {
	if c == nil || !guestType.IsValid() || vmid <= 0 {
		return ClusterResource{}, false
	}
	list, err := c.ListClusterResources(ctx, "vm")
	if err != nil {
		return ClusterResource{}, false
	}
	for _, r := range list {
		if GuestType(strings.TrimSpace(r.Type)) != guestType {
			continue
		}
		if r.VMID == vmid {
			return r, true
		}
	}
	return ClusterResource{}, false
}

func waitTask(ctx context.Context, c *Client, node string, upid string, timeout time.Duration) (TaskStatus, error) {
	if c == nil {
		return TaskStatus{}, errors.New("client 为空")
	}
	if strings.TrimSpace(node) == "" || strings.TrimSpace(upid) == "" {
		return TaskStatus{}, errors.New("node/upid 不能为空")
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		st, err := c.GetTaskStatus(ctx, node, upid)
		if err == nil {
			if strings.ToLower(strings.TrimSpace(st.Status)) == "stopped" {
				return st, nil
			}
		}

		select {
		case <-ctx.Done():
			if err != nil {
				return TaskStatus{}, err
			}
			return TaskStatus{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func usagePercent(used int64, total int64) float64 {
	if used <= 0 || total <= 0 {
		return 0
	}
	return (float64(used) / float64(total)) * 100
}

func limitStrings(ss []string, max int) []string {
	if max <= 0 || len(ss) <= max {
		return ss
	}
	return ss[:max]
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
