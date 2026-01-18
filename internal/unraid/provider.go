package unraid

// provider.go 将 Unraid 容器管理与查看能力适配为可插拔的企业微信交互 Provider。
import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zcw199604/wecom-home-ops/internal/core"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type ProviderDeps struct {
	WeCom  core.WeComSender
	Client *Client
	State  *core.StateStore
}

type Provider struct {
	wecom  core.WeComSender
	client *Client
	state  *core.StateStore
}

func NewProvider(deps ProviderDeps) *Provider {
	return &Provider{
		wecom:  deps.WeCom,
		client: deps.Client,
		state:  deps.State,
	}
}

func (p *Provider) Key() string { return "unraid" }

func (p *Provider) DisplayName() string { return "Unraid 容器" }

func (p *Provider) EntryKeywords() []string {
	return []string{"容器", "docker", "unraid"}
}

func (p *Provider) OnEnter(ctx context.Context, userID string) error {
	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewUnraidEntryCard(),
	})
}

func (p *Provider) HandleText(ctx context.Context, userID, content string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() {
		return false, nil
	}

	switch state.Step {
	case core.StepAwaitingUnraidViewAction:
		action, ok := parseUnraidViewAction(content)
		if !ok {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: unraidViewTextMenu(),
			})
		}
		state.Action = action

		state.Step = core.StepAwaitingContainerName
		p.state.Set(userID, state)

		prompt := fmt.Sprintf("已选择动作：%s\n请输入容器名：", action.DisplayName())
		if action == core.ActionUnraidViewLogs {
			prompt = fmt.Sprintf("已选择动作：%s\n请输入：容器名 [行数]（默认%d，最大%d）：", action.DisplayName(), defaultLogTail, maxLogTail)
		}
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: prompt})

	case core.StepAwaitingUnraidOpsAction:
		action, ok := parseUnraidOpsAction(content)
		if !ok {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: unraidOpsTextMenu(),
			})
		}
		state.Step = core.StepAwaitingContainerName
		state.Action = action
		p.state.Set(userID, state)

		prompt := fmt.Sprintf("已选择动作：%s\n请输入容器名：", action.DisplayName())
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: prompt})

	case core.StepAwaitingUnraidSystemAction:
		action, ok := parseUnraidSystemAction(content)
		if !ok {
			return true, p.wecom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: unraidSystemTextMenu(),
			})
		}

		state.Step = ""
		state.Action = ""
		state.ContainerName = ""
		p.state.Set(userID, state)
		return true, p.execViewAndReply(ctx, userID, action, "", 0)

	case core.StepAwaitingContainerName:
		if state.Action == core.ActionUnraidViewSystemStats || state.Action == core.ActionUnraidViewSystemStatsDetail {
			action := state.Action
			state.Step = ""
			state.Action = ""
			state.ContainerName = ""
			p.state.Set(userID, state)
			return true, p.execViewAndReply(ctx, userID, action, "", 0)
		}

	default:
		// 兼容模板卡片流程：当已选择动作但 Step 为空时，允许直接输入容器名（例如卡片不展示/手动输入）。
		if state.Step == "" && unraidActionNeedsContainer(state.Action) {
			break
		}
		return false, nil
	}

	containerNameRaw, logTail, err := parseContainerAndOptionalTail(content, state.Action)
	if err != nil {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: err.Error()})
	}

	containerName, err := core.ValidateContainerName(containerNameRaw)
	if err != nil {
		return true, p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("容器名不合法：%s", err.Error()),
		})
	}

	if state.Action.RequiresConfirm() {
		state.ContainerName = containerName
		state.Step = core.StepAwaitingConfirm
		p.state.Set(userID, state)

		_ = p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("确认执行：%s %s\n回复“确认”继续，回复“取消”终止。", state.Action.DisplayName(), state.ContainerName),
		})
		_ = p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewConfirmCard(state.Action.DisplayName(), state.ContainerName),
		})
		return true, nil
	}

	// 查看类动作：执行并回显，清除“待输入”状态但保留 ServiceKey。
	action := state.Action
	state.Step = ""
	state.Action = ""
	state.ContainerName = ""
	p.state.Set(userID, state)

	return true, p.execViewAndReply(ctx, userID, action, containerName, logTail)
}

func (p *Provider) HandleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) (bool, error) {
	key := strings.TrimSpace(msg.EventKey)
	if strings.HasPrefix(key, wecom.EventKeyUnraidContainerSelectPrefix) {
		suffix := strings.TrimPrefix(key, wecom.EventKeyUnraidContainerSelectPrefix)
		return true, p.handleContainerSelect(ctx, userID, suffix)
	}
	if strings.HasPrefix(key, wecom.EventKeyUnraidContainerPagePrefix) {
		suffix := strings.TrimPrefix(key, wecom.EventKeyUnraidContainerPagePrefix)
		return true, p.handleContainerPage(ctx, userID, suffix)
	}

	switch key {
	case wecom.EventKeyUnraidMenuOps:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidOpsCard()})
	case wecom.EventKeyUnraidMenuView:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidViewCard()})
	case wecom.EventKeyUnraidMenuSystem:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidSystemCard()})
	case wecom.EventKeyUnraidBackToMenu:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidEntryCard()})

	case wecom.EventKeyUnraidRestart, wecom.EventKeyUnraidStop, wecom.EventKeyUnraidForceUpdate,
		wecom.EventKeyUnraidViewStatus, wecom.EventKeyUnraidViewSystemStats, wecom.EventKeyUnraidViewSystemStatsDetail, wecom.EventKeyUnraidViewLogs:
		action := core.ActionFromEventKey(key)

		switch action {
		case core.ActionUnraidViewSystemStats, core.ActionUnraidViewSystemStatsDetail:
			return true, p.execViewAndReply(ctx, userID, action, "", 0)
		default:
			state := core.ConversationState{
				ServiceKey: p.Key(),
				Action:     action,
			}
			p.state.Set(userID, state)

			if err := p.sendContainerSelectCard(ctx, userID, action, 1); err != nil {
				state.Step = core.StepAwaitingContainerName
				p.state.Set(userID, state)

				prompt := fmt.Sprintf("已选择动作：%s\n请输入容器名：", action.DisplayName())
				if action == core.ActionUnraidViewLogs {
					prompt = fmt.Sprintf("已选择动作：%s\n请输入：容器名 [行数]（默认%d，最大%d）：", action.DisplayName(), defaultLogTail, maxLogTail)
				}
				return true, p.wecom.SendText(ctx, wecom.TextMessage{
					ToUser:  userID,
					Content: "获取容器列表失败，已切换为文本输入。\n" + prompt,
				})
			}
			return true, nil
		}
	default:
		return false, nil
	}
}

func (p *Provider) handleContainerPage(ctx context.Context, userID string, pageStr string) error {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() || !unraidActionNeedsContainer(state.Action) {
		_ = p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "会话已过期，请重新选择动作。"})
		_ = p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidOpsCard()})
		return nil
	}

	page, err := strconv.Atoi(strings.TrimSpace(pageStr))
	if err != nil || page <= 0 {
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "页码不合法，请重新选择。"})
	}
	return p.sendContainerSelectCard(ctx, userID, state.Action, page)
}

func (p *Provider) handleContainerSelect(ctx context.Context, userID string, containerNameRaw string) error {
	containerName, err := core.ValidateContainerName(containerNameRaw)
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("容器名不合法：%s", err.Error()),
		})
	}

	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() || !unraidActionNeedsContainer(state.Action) {
		_ = p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "会话已过期，请重新选择动作。"})
		_ = p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidOpsCard()})
		return nil
	}

	switch state.Action {
	case core.ActionUnraidRestart, core.ActionUnraidStop, core.ActionUnraidForceUpdate:
		state.Step = core.StepAwaitingConfirm
		state.ContainerName = containerName
		p.state.Set(userID, state)
		return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewConfirmCard(state.Action.DisplayName(), containerName),
		})

	case core.ActionUnraidViewStatus:
		action := state.Action
		state.Step = ""
		state.Action = ""
		state.ContainerName = ""
		p.state.Set(userID, state)
		return p.execViewAndReply(ctx, userID, action, containerName, 0)

	case core.ActionUnraidViewLogs:
		action := state.Action
		state.Step = ""
		state.Action = ""
		state.ContainerName = ""
		p.state.Set(userID, state)
		return p.execViewAndReply(ctx, userID, action, containerName, defaultLogTail)

	default:
		return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: "未知动作，请返回后重试。"})
	}
}

const unraidContainerSelectPageSize = 3

func (p *Provider) sendContainerSelectCard(ctx context.Context, userID string, action core.Action, page int) error {
	if p.client == nil {
		return errors.New("unraid client 未配置")
	}

	names, err := p.listContainerNames(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("未找到任何容器")
	}

	if page <= 0 {
		page = 1
	}
	totalPages := (len(names) + unraidContainerSelectPageSize - 1) / unraidContainerSelectPageSize
	if totalPages <= 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * unraidContainerSelectPageSize
	if start < 0 {
		start = 0
	}
	if start >= len(names) {
		start = (totalPages - 1) * unraidContainerSelectPageSize
	}
	end := start + unraidContainerSelectPageSize
	if end > len(names) {
		end = len(names)
	}

	var opts []wecom.UnraidContainerOption
	for _, name := range names[start:end] {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		opts = append(opts, wecom.UnraidContainerOption{
			Name: n,
			Text: truncateRunes(n, 32),
		})
	}

	prevPage := 0
	nextPage := 0
	if page > 1 {
		prevPage = page - 1
	}
	if page < totalPages {
		nextPage = page + 1
	}

	return p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
		ToUser: userID,
		Card:   wecom.NewUnraidContainerSelectCard(action.DisplayName(), page, totalPages, opts, prevPage, nextPage),
	})
}

func (p *Provider) listContainerNames(ctx context.Context) ([]string, error) {
	if p.client == nil {
		return nil, errors.New("unraid client 未配置")
	}

	const q = `query { docker { containers { id names state status } } }`
	var resp struct {
		Docker struct {
			Containers []struct {
				Names interface{} `json:"names"`
			} `json:"containers"`
		} `json:"docker"`
	}
	if err := p.client.do(ctx, q, nil, &resp); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, 64)
	var names []string
	for _, ct := range resp.Docker.Containers {
		for _, n := range normalizeContainerNames(ct.Names) {
			nn := normalizeName(n)
			if nn == "" {
				continue
			}
			if _, ok := seen[nn]; ok {
				continue
			}
			seen[nn] = struct{}{}
			names = append(names, nn)
		}
	}
	sort.Strings(names)
	return names, nil
}

func unraidActionNeedsContainer(action core.Action) bool {
	switch action {
	case core.ActionUnraidRestart, core.ActionUnraidStop, core.ActionUnraidForceUpdate,
		core.ActionUnraidViewStatus, core.ActionUnraidViewLogs:
		return true
	default:
		return false
	}
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return ""
	}
	i := 0
	for idx := range s {
		if i >= max {
			return s[:idx] + "…"
		}
		i++
	}
	return s
}

func unraidViewTextMenu() string {
	return "Unraid 容器查看（文本模式）\n" +
		"1. 查看状态\n" +
		"2. 查看日志\n" +
		"\n提示：系统资源请从“系统监控”进入。\n" +
		"\n回复序号选择。"
}

func unraidSystemTextMenu() string {
	return "Unraid 系统监控（文本模式）\n" +
		"1. 系统资源概览\n" +
		"2. 系统资源详情\n" +
		"\n回复序号选择。"
}

func unraidOpsTextMenu() string {
	return "Unraid 容器操作（文本模式）\n" +
		"1. 重启容器\n" +
		"2. 停止容器\n" +
		"3. 强制更新\n" +
		"\n回复序号选择。"
}

func parseUnraidViewAction(input string) (core.Action, bool) {
	s := strings.ToLower(strings.TrimSpace(input))
	switch s {
	case "1", "状态", "查看状态", "status":
		return core.ActionUnraidViewStatus, true
	case "2", "日志", "查看日志", "logs":
		return core.ActionUnraidViewLogs, true
	default:
		return "", false
	}
}

func parseUnraidSystemAction(input string) (core.Action, bool) {
	s := strings.ToLower(strings.TrimSpace(input))
	switch s {
	case "1", "概览", "系统资源概览", "系统概览", "sys":
		return core.ActionUnraidViewSystemStats, true
	case "2", "详情", "系统资源详情", "系统详情", "detail":
		return core.ActionUnraidViewSystemStatsDetail, true
	default:
		return "", false
	}
}

func parseUnraidOpsAction(input string) (core.Action, bool) {
	s := strings.ToLower(strings.TrimSpace(input))
	switch s {
	case "1", "重启", "重启容器", "restart":
		return core.ActionUnraidRestart, true
	case "2", "停止", "停止容器", "stop":
		return core.ActionUnraidStop, true
	case "3", "强制更新", "更新", "update":
		return core.ActionUnraidForceUpdate, true
	default:
		return "", false
	}
}

func (p *Provider) HandleConfirm(ctx context.Context, userID string) (bool, error) {
	state, ok := p.state.Get(userID)
	if !ok || state.ServiceKey != p.Key() || state.Step != core.StepAwaitingConfirm {
		return false, nil
	}
	p.state.Clear(userID)

	start := time.Now()
	err := p.execOperationAction(ctx, state.Action, state.ContainerName)
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
		Content: fmt.Sprintf("执行成功（%dms）：%s %s", cost, state.Action.DisplayName(), state.ContainerName),
	})
	return true, nil
}

func (p *Provider) execOperationAction(ctx context.Context, action core.Action, containerName string) error {
	switch action {
	case core.ActionUnraidRestart:
		return p.client.RestartContainerByName(ctx, containerName)
	case core.ActionUnraidStop:
		return p.client.StopContainerByName(ctx, containerName)
	case core.ActionUnraidForceUpdate:
		return p.client.ForceUpdateContainerByName(ctx, containerName)
	default:
		return fmt.Errorf("未知动作: %s", action)
	}
}

func (p *Provider) execViewAndReply(ctx context.Context, userID string, action core.Action, containerName string, logTail int) error {
	start := time.Now()
	content, err := p.execViewAction(ctx, action, containerName, logTail)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		return p.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("查询失败（%dms）：%s", cost, err.Error()),
		})
	}
	return p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: truncateForWecom(content)})
}

func (p *Provider) execViewAction(ctx context.Context, action core.Action, containerName string, logTail int) (string, error) {
	switch action {
	case core.ActionUnraidViewStatus:
		st, err := p.client.GetContainerStatusByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatus(st), nil

	case core.ActionUnraidViewSystemStats:
		m, err := p.client.GetSystemMetrics(ctx)
		if err != nil {
			return "", err
		}
		return formatSystemMetricsOverview(m), nil

	case core.ActionUnraidViewSystemStatsDetail:
		m, err := p.client.GetSystemMetrics(ctx)
		if err != nil {
			return "", err
		}
		return formatSystemMetricsDetail(m), nil

	case core.ActionUnraidViewLogs:
		logs, err := p.client.GetContainerLogsByName(ctx, containerName, logTail)
		if err != nil {
			return "", err
		}
		return formatContainerLogs(logs), nil

	default:
		return "", fmt.Errorf("未知动作: %s", action)
	}
}

const (
	defaultLogTail     = 50
	maxLogTail         = 200
	maxWecomTextBytes  = 1800
	wecomTruncSuffix   = "\n…（已截断）"
	wecomTruncMinBytes = 64
)

func parseContainerAndOptionalTail(input string, action core.Action) (container string, tail int, err error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return "", 0, errors.New("请输入容器名。")
	}

	container = fields[0]
	if action != core.ActionUnraidViewLogs {
		return container, 0, nil
	}

	tail = defaultLogTail
	if len(fields) >= 2 {
		n, err2 := strconv.Atoi(fields[1])
		if err2 != nil {
			return "", 0, fmt.Errorf("日志行数不合法：%s", fields[1])
		}
		tail = clampInt(n, 1, maxLogTail)
	}
	return container, tail, nil
}

func formatContainerStatus(st ContainerStatus) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【状态】%s", st.Name))
	lines = append(lines, fmt.Sprintf("state: %s", st.State))
	lines = append(lines, fmt.Sprintf("status: %s", st.Status))
	if st.Uptime != "" {
		lines = append(lines, fmt.Sprintf("运行时长: %s", st.Uptime))
	}
	return strings.Join(lines, "\n")
}

func formatSystemMetricsOverview(m SystemMetrics) string {
	var lines []string
	lines = append(lines, "【系统资源概览】")
	lines = append(lines, fmt.Sprintf("CPU 总使用率: %.2f%%", m.CPUPercentTotal))
	if m.MemoryTotal > 0 {
		used := m.MemoryUsed
		percent := m.MemoryPercent
		usedLabel := "已用"
		if m.HasMemoryEffective {
			used = m.MemoryUsedEffective
			percent = m.MemoryPercentEffective
			usedLabel = "实际占用"
		}
		memLine := fmt.Sprintf("内存%s: %s / %s（%.2f%%）", usedLabel, formatBytesIEC(used), formatBytesIEC(m.MemoryTotal), percent)
		if m.MemoryAvailable > 0 {
			memLine = memLine + fmt.Sprintf("，可用 %s", formatBytesIEC(m.MemoryAvailable))
		}
		lines = append(lines, memLine)
	} else {
		lines = append(lines, fmt.Sprintf("内存已用: %s（占用率 %.2f%%）", formatBytesIEC(m.MemoryUsed), m.MemoryPercent))
		lines = append(lines, fmt.Sprintf("内存可用: %s；空闲: %s", formatBytesIEC(m.MemoryAvailable), formatBytesIEC(m.MemoryFree)))
	}
	if m.HasUnraidUptime {
		lines = append(lines, fmt.Sprintf("Unraid 启动时长: %s", formatSecondsCN(m.UnraidUptimeSeconds)))
	} else if note := strings.TrimSpace(m.UnraidUptimeNote); note != "" {
		lines = append(lines, fmt.Sprintf("Unraid 启动时长: %s", note))
	}
	if len(m.UPSDevices) > 0 {
		first := formatUPSDeviceInline(m.UPSDevices[0])
		if len(m.UPSDevices) == 1 {
			lines = append(lines, fmt.Sprintf("UPS: %s", first))
		} else {
			lines = append(lines, fmt.Sprintf("UPS: %d 台（首台 %s）", len(m.UPSDevices), first))
		}
	} else if note := strings.TrimSpace(m.UPSNote); note != "" {
		lines = append(lines, fmt.Sprintf("UPS: %s", note))
	}
	if m.HasNetworkTotals {
		lines = append(lines, fmt.Sprintf("网络（容器累计）: 接收 %s；发送 %s", formatBytesIEC(m.NetworkRxBytesTotal), formatBytesIEC(m.NetworkTxBytesTotal)))
	}
	return strings.Join(lines, "\n")
}

func formatSystemMetricsDetail(m SystemMetrics) string {
	var lines []string
	lines = append(lines, "【系统资源详情】")
	lines = append(lines, fmt.Sprintf("CPU 总使用率: %.2f%%", m.CPUPercentTotal))
	if len(m.PerCPU) > 0 {
		maxCores := len(m.PerCPU)
		if maxCores > 8 {
			maxCores = 8
		}

		minTotal, maxTotal := m.PerCPU[0].PercentTotal, m.PerCPU[0].PercentTotal
		minIdle, maxIdle := m.PerCPU[0].PercentIdle, m.PerCPU[0].PercentIdle
		for i := 0; i < maxCores; i++ {
			c := m.PerCPU[i]
			if c.PercentTotal < minTotal {
				minTotal = c.PercentTotal
			}
			if c.PercentTotal > maxTotal {
				maxTotal = c.PercentTotal
			}
			if c.PercentIdle < minIdle {
				minIdle = c.PercentIdle
			}
			if c.PercentIdle > maxIdle {
				maxIdle = c.PercentIdle
			}
		}
		lines = append(lines, fmt.Sprintf("CPU 核心（前 %d 个）: %.2f%%~%.2f%%（空闲 %.2f%%~%.2f%%）", maxCores, minTotal, maxTotal, minIdle, maxIdle))

		for i := 0; i < maxCores; i++ {
			c := m.PerCPU[i]
			lines = append(lines, fmt.Sprintf("CPU%d: 总 %.2f%%（用户 %.2f%%，系统 %.2f%%，空闲 %.2f%%）", i, c.PercentTotal, c.PercentUser, c.PercentSystem, c.PercentIdle))
		}
		if len(m.PerCPU) > maxCores {
			lines = append(lines, fmt.Sprintf("…（仅展示前 %d 个 CPU 核）", maxCores))
		}
	}

	if m.HasUnraidUptime {
		lines = append(lines, fmt.Sprintf("Unraid 启动时长: %s", formatSecondsCN(m.UnraidUptimeSeconds)))
	} else if note := strings.TrimSpace(m.UnraidUptimeNote); note != "" {
		lines = append(lines, fmt.Sprintf("Unraid 启动时长: %s", note))
	}

	lines = append(lines, fmt.Sprintf("内存总量: %s", formatBytesIEC(m.MemoryTotal)))
	if m.HasMemoryEffective {
		lines = append(lines, fmt.Sprintf("内存实际占用: %s（%.2f%%）", formatBytesIEC(m.MemoryUsedEffective), m.MemoryPercentEffective))
		if m.MemoryAvailable > 0 {
			lines = append(lines, fmt.Sprintf("内存可用: %s", formatBytesIEC(m.MemoryAvailable)))
		}
		if m.MemoryFree > 0 {
			lines = append(lines, fmt.Sprintf("内存空闲: %s", formatBytesIEC(m.MemoryFree)))
		}
		if m.MemoryUsed > 0 {
			lines = append(lines, fmt.Sprintf("内存原始已用（含缓存/缓冲）: %s", formatBytesIEC(m.MemoryUsed)))
		}
		lines = append(lines, "提示：判断内存压力优先看“内存可用/内存实际占用”。")
	} else {
		lines = append(lines, fmt.Sprintf("内存已用: %s（%.2f%%）", formatBytesIEC(m.MemoryUsed), m.MemoryPercent))
		lines = append(lines, fmt.Sprintf("内存可用: %s；空闲: %s", formatBytesIEC(m.MemoryAvailable), formatBytesIEC(m.MemoryFree)))
	}
	if m.HasNetworkTotals {
		lines = append(lines, fmt.Sprintf("网络（容器累计）: 接收 %s；发送 %s", formatBytesIEC(m.NetworkRxBytesTotal), formatBytesIEC(m.NetworkTxBytesTotal)))
	}
	if len(m.UPSDevices) > 0 {
		maxDevices := len(m.UPSDevices)
		if maxDevices > 2 {
			maxDevices = 2
		}
		for i := 0; i < maxDevices; i++ {
			lines = append(lines, fmt.Sprintf("UPS%d: %s", i+1, formatUPSDeviceInline(m.UPSDevices[i])))
		}
		if len(m.UPSDevices) > maxDevices {
			lines = append(lines, fmt.Sprintf("…（仅展示前 %d 台 UPS）", maxDevices))
		}
	} else if note := strings.TrimSpace(m.UPSNote); note != "" {
		lines = append(lines, fmt.Sprintf("UPS: %s", note))
	}
	return strings.Join(lines, "\n")
}

func formatSecondsCN(seconds int64) string {
	if seconds <= 0 {
		return "0 分钟"
	}
	days := seconds / (24 * 60 * 60)
	hours := (seconds % (24 * 60 * 60)) / (60 * 60)
	minutes := (seconds % (60 * 60)) / 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d 天 %d 小时", days, hours)
		}
		return fmt.Sprintf("%d 天", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d 小时 %d 分钟", hours, minutes)
		}
		return fmt.Sprintf("%d 小时", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return "不足 1 分钟"
}

func formatPercent(v float64) string {
	if v >= 0 && v <= 1 {
		v = v * 100
	}
	return fmt.Sprintf("%.0f%%", v)
}

func formatUPSDeviceInline(d UPSDeviceMetrics) string {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		name = strings.TrimSpace(d.Model)
	}
	if name == "" {
		name = strings.TrimSpace(d.ID)
	}
	if name == "" {
		name = "UPS"
	}

	status := strings.TrimSpace(d.Status)
	if status == "" {
		status = "未知"
	}

	parts := []string{status}
	if d.Battery != nil {
		if d.Battery.ChargeLevel != nil {
			parts = append(parts, "电量 "+formatPercent(*d.Battery.ChargeLevel))
		}
		if d.Battery.EstimatedRuntime != nil {
			rt := int64(*d.Battery.EstimatedRuntime)
			if rt > 0 {
				parts = append(parts, "续航 "+formatSecondsCN(rt))
			}
		}
	}
	if d.Power != nil {
		if d.Power.LoadPercentage != nil {
			parts = append(parts, "负载 "+formatPercent(*d.Power.LoadPercentage))
		}
	}
	return fmt.Sprintf("%s（%s）", name, strings.Join(parts, "，"))
}

func formatBytesIEC(b int64) string {
	if b <= 0 {
		return "0B"
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	abs := b
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= tib:
		return fmt.Sprintf("%.2fTiB", float64(b)/float64(tib))
	case abs >= gib:
		return fmt.Sprintf("%.2fGiB", float64(b)/float64(gib))
	case abs >= mib:
		return fmt.Sprintf("%.2fMiB", float64(b)/float64(mib))
	case abs >= kib:
		return fmt.Sprintf("%.2fKiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func formatContainerLogs(lg ContainerLogs) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【日志】%s（tail %d 行）", lg.Name, lg.Tail))
	if lg.Trunc {
		lines = append(lines, "（已截取最新日志）")
	}
	lines = append(lines, lg.Logs)
	return strings.Join(lines, "\n")
}

func truncateForWecom(s string) string {
	if len(s) <= maxWecomTextBytes {
		return s
	}
	suffix := wecomTruncSuffix
	maxBytes := maxWecomTextBytes
	if maxBytes <= wecomTruncMinBytes {
		maxBytes = wecomTruncMinBytes
	}
	if len(suffix) >= maxBytes {
		return safeTruncateUTF8(s, maxBytes)
	}
	cut := safeTruncateUTF8(s, maxBytes-len(suffix))
	return cut + suffix
}

func safeTruncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := []byte(s)
	if maxBytes >= len(b) {
		return s
	}
	b = b[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}
