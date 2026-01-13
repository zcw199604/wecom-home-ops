package unraid

// provider.go 将 Unraid 容器管理与查看能力适配为可插拔的企业微信交互 Provider。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if state.Step != core.StepAwaitingContainerName {
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

		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewConfirmCard(state.Action.DisplayName(), state.ContainerName),
		})
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
	switch key {
	case wecom.EventKeyUnraidMenuOps:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidOpsCard()})
	case wecom.EventKeyUnraidMenuView:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidViewCard()})
	case wecom.EventKeyUnraidBackToMenu:
		p.state.Set(userID, core.ConversationState{ServiceKey: p.Key()})
		return true, p.wecom.SendTemplateCard(ctx, wecom.TemplateCardMessage{ToUser: userID, Card: wecom.NewUnraidEntryCard()})

	case wecom.EventKeyUnraidRestart, wecom.EventKeyUnraidStop, wecom.EventKeyUnraidForceUpdate,
		wecom.EventKeyUnraidViewStatus, wecom.EventKeyUnraidViewStats, wecom.EventKeyUnraidViewStatsDetail, wecom.EventKeyUnraidViewLogs:
		action := core.ActionFromEventKey(key)
		p.state.Set(userID, core.ConversationState{
			ServiceKey: p.Key(),
			Step:       core.StepAwaitingContainerName,
			Action:     action,
		})

		prompt := fmt.Sprintf("已选择动作：%s\n请输入容器名：", action.DisplayName())
		if action == core.ActionUnraidViewLogs {
			prompt = fmt.Sprintf("已选择动作：%s\n请输入：容器名 [行数]（默认%d，最大%d）：", action.DisplayName(), defaultLogTail, maxLogTail)
		}
		return true, p.wecom.SendText(ctx, wecom.TextMessage{ToUser: userID, Content: prompt})
	default:
		return false, nil
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

	case core.ActionUnraidViewStats:
		stats, err := p.client.GetContainerStatsByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatsOverview(stats), nil

	case core.ActionUnraidViewStatsDetail:
		stats, err := p.client.GetContainerStatsByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatsDetail(stats), nil

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

func formatContainerStatsOverview(st ContainerStats) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【资源概览】%s", st.Name))

	m, ok := st.Stats.(map[string]interface{})
	if ok {
		cpu, _ := pickAnyString(m, "cpuPercent", "cpu_percent", "cpu", "cpuUsage", "cpu_usage")
		memUsage, _ := pickAnyString(m, "memUsage", "memoryUsage", "mem_usage", "memory_usage")
		memLimit, _ := pickAnyString(m, "memLimit", "memoryLimit", "mem_limit", "memory_limit")
		netIO, _ := pickAnyString(m, "netIO", "net_io", "networkIO", "network_io")
		blockIO, _ := pickAnyString(m, "blockIO", "block_io", "diskIO", "disk_io")
		pids, _ := pickAnyString(m, "pids", "pid", "pidsCurrent", "pids_current")

		if cpu != "" {
			lines = append(lines, fmt.Sprintf("CPU: %s", cpu))
		}
		if memUsage != "" || memLimit != "" {
			switch {
			case memUsage != "" && memLimit != "":
				lines = append(lines, fmt.Sprintf("内存: %s / %s", memUsage, memLimit))
			case memUsage != "":
				lines = append(lines, fmt.Sprintf("内存: %s", memUsage))
			default:
				lines = append(lines, fmt.Sprintf("内存限制: %s", memLimit))
			}
		}
		if netIO != "" {
			lines = append(lines, fmt.Sprintf("网络IO: %s", netIO))
		}
		if blockIO != "" {
			lines = append(lines, fmt.Sprintf("磁盘IO: %s", blockIO))
		}
		if pids != "" {
			lines = append(lines, fmt.Sprintf("PIDs: %s", pids))
		}
	}

	if len(lines) == 1 {
		lines = append(lines, "（未识别到常见字段，返回原始数据）")
		lines = append(lines, mustJSON(st.Stats))
	}
	return strings.Join(lines, "\n")
}

func formatContainerStatsDetail(st ContainerStats) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【资源详情】%s", st.Name))
	lines = append(lines, mustJSON(st.Stats))
	return strings.Join(lines, "\n")
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

func pickAnyString(m map[string]interface{}, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return fmt.Sprint(v), true
		}
	}
	return "", false
}

func mustJSON(v interface{}) string {
	if v == nil {
		return "（无数据）"
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
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
