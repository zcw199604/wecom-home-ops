// Package core 提供企业微信会话的动作路由与状态管理。
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"daily-help/internal/unraid"
	"daily-help/internal/wecom"
)

type RouterDeps struct {
	WeCom         *wecom.Client
	Unraid        *unraid.Client
	AllowedUserID map[string]struct{}
}

type Router struct {
	WeCom         *wecom.Client
	Unraid        *unraid.Client
	AllowedUserID map[string]struct{}

	state *StateStore
}

func NewRouter(deps RouterDeps) *Router {
	return &Router{
		WeCom:         deps.WeCom,
		Unraid:        deps.Unraid,
		AllowedUserID: deps.AllowedUserID,
		state:         NewStateStore(30 * time.Minute),
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

	if isEntryKeyword(content) {
		r.state.Clear(userID)
		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewUnraidEntryCard(),
		})
	}

	cmd, recognized, err := parseTextCommand(content)
	if recognized {
		r.state.Clear(userID)
		if err != nil {
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: err.Error(),
			})
		}
		return r.execViewAndReply(ctx, userID, cmd.Action, cmd.ContainerName, cmd.LogTail)
	}

	state, ok := r.state.Get(userID)
	if !ok || state.Step != StepAwaitingContainerName {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "请输入“容器”打开菜单，或发送：状态 <容器名> / 资源 <容器名> / 详情 <容器名> / 日志 <容器名> [行数]",
		})
	}

	containerNameRaw, logTail, err := parseContainerAndOptionalTail(content, state.Action)
	if err != nil {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: err.Error(),
		})
	}

	containerName, err := validateContainerName(containerNameRaw)
	if err != nil {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("容器名不合法：%s", err.Error()),
		})
	}

	if state.Action.RequiresConfirm() {
		state.ContainerName = containerName
		state.Step = StepAwaitingConfirm
		r.state.Set(userID, state)

		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewConfirmCard(state.Action.DisplayName(), state.ContainerName),
		})
	}

	r.state.Clear(userID)
	return r.execViewAndReply(ctx, userID, state.Action, containerName, logTail)
}

func (r *Router) handleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) error {
	if msg.Event == "enter_agent" {
		r.state.Clear(userID)
		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewUnraidEntryCard(),
		})
	}

	if msg.Event != "template_card_event" {
		return nil
	}

	key := strings.TrimSpace(msg.EventKey)
	switch key {
	case wecom.EventKeyUnraidMenuOps:
		r.state.Clear(userID)
		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewUnraidOpsCard(),
		})

	case wecom.EventKeyUnraidMenuView:
		r.state.Clear(userID)
		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewUnraidViewCard(),
		})

	case wecom.EventKeyUnraidBackToMenu:
		r.state.Clear(userID)
		return r.WeCom.SendTemplateCard(ctx, wecom.TemplateCardMessage{
			ToUser: userID,
			Card:   wecom.NewUnraidEntryCard(),
		})

	case wecom.EventKeyUnraidRestart, wecom.EventKeyUnraidStop, wecom.EventKeyUnraidForceUpdate,
		wecom.EventKeyUnraidViewStatus, wecom.EventKeyUnraidViewStats, wecom.EventKeyUnraidViewStatsDetail, wecom.EventKeyUnraidViewLogs:
		action := ActionFromEventKey(key)
		r.state.Set(userID, ConversationState{
			Step:   StepAwaitingContainerName,
			Action: action,
		})

		prompt := fmt.Sprintf("已选择动作：%s\n请输入容器名：", action.DisplayName())
		if action == ActionViewLogs {
			prompt = fmt.Sprintf("已选择动作：%s\n请输入：容器名 [行数]（默认%d，最大%d）：", action.DisplayName(), defaultLogTail, maxLogTail)
		}
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: prompt,
		})

	case wecom.EventKeyConfirm:
		state, ok := r.state.Get(userID)
		if !ok || state.Step != StepAwaitingConfirm {
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: "会话已过期，请输入“容器”重新开始。",
			})
		}
		r.state.Clear(userID)

		start := time.Now()
		err := r.execOperationAction(ctx, state.Action, state.ContainerName)
		cost := time.Since(start).Milliseconds()
		if err != nil {
			return r.WeCom.SendText(ctx, wecom.TextMessage{
				ToUser:  userID,
				Content: fmt.Sprintf("执行失败（%dms）：%s", cost, err.Error()),
			})
		}

		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("执行成功（%dms）：%s %s", cost, state.Action.DisplayName(), state.ContainerName),
		})

	case wecom.EventKeyCancel:
		r.state.Clear(userID)
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: "已取消。",
		})
	default:
		return nil
	}
}

func (r *Router) execOperationAction(ctx context.Context, action Action, containerName string) error {
	switch action {
	case ActionRestart:
		return r.Unraid.RestartContainerByName(ctx, containerName)
	case ActionStop:
		return r.Unraid.StopContainerByName(ctx, containerName)
	case ActionForceUpdate:
		return r.Unraid.ForceUpdateContainerByName(ctx, containerName)
	default:
		return fmt.Errorf("未知动作: %s", action)
	}
}

func (r *Router) execViewAndReply(ctx context.Context, userID string, action Action, containerName string, logTail int) error {
	start := time.Now()
	content, err := r.execViewAction(ctx, action, containerName, logTail)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		return r.WeCom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: fmt.Sprintf("查询失败（%dms）：%s", cost, err.Error()),
		})
	}

	return r.WeCom.SendText(ctx, wecom.TextMessage{
		ToUser:  userID,
		Content: truncateForWecom(content),
	})
}

func (r *Router) execViewAction(ctx context.Context, action Action, containerName string, logTail int) (string, error) {
	switch action {
	case ActionViewStatus:
		st, err := r.Unraid.GetContainerStatusByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatus(st), nil

	case ActionViewStats:
		stats, err := r.Unraid.GetContainerStatsByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatsOverview(stats), nil

	case ActionViewStatsDetail:
		stats, err := r.Unraid.GetContainerStatsByName(ctx, containerName)
		if err != nil {
			return "", err
		}
		return formatContainerStatsDetail(stats), nil

	case ActionViewLogs:
		logs, err := r.Unraid.GetContainerLogsByName(ctx, containerName, logTail)
		if err != nil {
			return "", err
		}
		return formatContainerLogs(logs), nil

	default:
		return "", fmt.Errorf("未知动作: %s", action)
	}
}

func isEntryKeyword(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "help", "菜单", "容器", "docker", "unraid":
		return true
	default:
		return false
	}
}

const (
	defaultLogTail     = 50
	maxLogTail         = 200
	maxWecomTextBytes  = 1800
	wecomTruncSuffix   = "\n…（已截断）"
	wecomTruncMinBytes = 64
)

type textCommand struct {
	Action        Action
	ContainerName string
	LogTail       int
}

func parseTextCommand(content string) (textCommand, bool, error) {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 {
		return textCommand{}, false, nil
	}

	cmd := strings.ToLower(fields[0])

	switch cmd {
	case "状态", "status":
		if len(fields) < 2 {
			return textCommand{}, true, errors.New("用法：状态 <容器名>")
		}
		name, err := validateContainerName(fields[1])
		if err != nil {
			return textCommand{}, true, fmt.Errorf("容器名不合法：%s", err.Error())
		}
		return textCommand{Action: ActionViewStatus, ContainerName: name}, true, nil

	case "资源", "stats":
		if len(fields) < 2 {
			return textCommand{}, true, errors.New("用法：资源 <容器名>")
		}
		name, err := validateContainerName(fields[1])
		if err != nil {
			return textCommand{}, true, fmt.Errorf("容器名不合法：%s", err.Error())
		}
		return textCommand{Action: ActionViewStats, ContainerName: name}, true, nil

	case "详情", "资源详情", "detail":
		if len(fields) < 2 {
			return textCommand{}, true, errors.New("用法：详情 <容器名>")
		}
		name, err := validateContainerName(fields[1])
		if err != nil {
			return textCommand{}, true, fmt.Errorf("容器名不合法：%s", err.Error())
		}
		return textCommand{Action: ActionViewStatsDetail, ContainerName: name}, true, nil

	case "日志", "log", "logs":
		if len(fields) < 2 {
			return textCommand{}, true, fmt.Errorf("用法：日志 <容器名> [行数]（默认%d，最大%d）", defaultLogTail, maxLogTail)
		}
		name, err := validateContainerName(fields[1])
		if err != nil {
			return textCommand{}, true, fmt.Errorf("容器名不合法：%s", err.Error())
		}

		tail := defaultLogTail
		if len(fields) >= 3 {
			n, err := strconv.Atoi(fields[2])
			if err != nil {
				return textCommand{}, true, fmt.Errorf("日志行数不合法：%s", fields[2])
			}
			tail = clampInt(n, 1, maxLogTail)
		}
		return textCommand{Action: ActionViewLogs, ContainerName: name, LogTail: tail}, true, nil

	default:
		return textCommand{}, false, nil
	}
}

func parseContainerAndOptionalTail(input string, action Action) (container string, tail int, err error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return "", 0, errors.New("请输入容器名。")
	}

	container = fields[0]
	if action != ActionViewLogs {
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

func formatContainerStatus(st unraid.ContainerStatus) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【状态】%s", st.Name))
	lines = append(lines, fmt.Sprintf("state: %s", st.State))
	lines = append(lines, fmt.Sprintf("status: %s", st.Status))
	if st.Uptime != "" {
		lines = append(lines, fmt.Sprintf("运行时长: %s", st.Uptime))
	}
	return strings.Join(lines, "\n")
}

func formatContainerStatsOverview(st unraid.ContainerStats) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【资源概览】%s", st.Name))

	m, ok := st.Stats.(map[string]interface{})
	if ok {
		cpu, _ := pickAnyString(m,
			"cpuPercent", "cpu_percent", "cpu", "cpuUsage", "cpu_usage",
		)
		memUsage, _ := pickAnyString(m,
			"memUsage", "memoryUsage", "mem_usage", "memory_usage",
		)
		memLimit, _ := pickAnyString(m,
			"memLimit", "memoryLimit", "mem_limit", "memory_limit",
		)
		netIO, _ := pickAnyString(m,
			"netIO", "net_io", "networkIO", "network_io",
		)
		blockIO, _ := pickAnyString(m,
			"blockIO", "block_io", "diskIO", "disk_io",
		)
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

func formatContainerStatsDetail(st unraid.ContainerStats) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("【资源详情】%s", st.Name))
	lines = append(lines, mustJSON(st.Stats))
	return strings.Join(lines, "\n")
}

func formatContainerLogs(lg unraid.ContainerLogs) string {
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

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
