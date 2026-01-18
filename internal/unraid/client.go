// Package unraid 封装 Unraid Connect GraphQL API 的容器管理与查询能力。
package unraid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type ClientConfig struct {
	Endpoint string
	APIKey   string
	Origin   string

	// WebGUI 兜底配置（用于 GraphQL 不支持/不适合的操作）。
	// - WebGUICommandURL: 例如 http://<ip>/webGui/include/StartCommand.php（默认可从 Endpoint 推导）
	// - WebGUIEventsURL: 例如 http://<ip>/plugins/dynamix.docker.manager/include/Events.php（默认可从 Endpoint 推导）
	// - WebGUICSRFToken: 抓包/页面里看到的 csrf_token（通常随登录会话变化）
	// - WebGUICookie: 可选；如 WebGUI 需要登录，则需提供 Cookie 以通过鉴权
	WebGUICommandURL string
	WebGUIEventsURL  string
	WebGUICSRFToken  string
	WebGUICookie     string

	// 容器日志字段（默认 logs）。如 logs 返回对象，可通过 LogsPayloadField 指定承载日志文本的字段名。
	LogsField        string
	LogsTailArg      *string
	LogsPayloadField string

	// 容器资源统计字段（默认 stats）。如 stats 返回对象，StatsFields 用于指定 selection set。
	StatsField  string
	StatsFields []string

	// “强制更新”mutation 配置（默认 updateContainer）。
	// 默认形态：docker { updateContainer(id: PrefixedID!) { __typename } }
	ForceUpdateMutation     string
	ForceUpdateArgName      string
	ForceUpdateArgType      string
	ForceUpdateReturnFields []string
}

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	applyClientDefaults(&cfg)
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

var defaultStatsFields = []string{
	"cpuPercent",
	"memUsage",
	"memLimit",
	"netIO",
	"blockIO",
	"pids",
}

func applyClientDefaults(cfg *ClientConfig) {
	if strings.TrimSpace(cfg.LogsField) == "" {
		cfg.LogsField = "logs"
	}
	if cfg.LogsTailArg == nil {
		v := "tail"
		cfg.LogsTailArg = &v
	}

	if strings.TrimSpace(cfg.StatsField) == "" {
		cfg.StatsField = "stats"
	}
	if cfg.StatsFields == nil {
		cfg.StatsFields = append([]string(nil), defaultStatsFields...)
	}

	if strings.TrimSpace(cfg.ForceUpdateMutation) == "" {
		cfg.ForceUpdateMutation = "updateContainer"
	}
	if strings.TrimSpace(cfg.ForceUpdateArgName) == "" {
		cfg.ForceUpdateArgName = "id"
	}
	if strings.TrimSpace(cfg.ForceUpdateArgType) == "" {
		cfg.ForceUpdateArgType = "PrefixedID!"
	}
	if cfg.ForceUpdateReturnFields == nil {
		cfg.ForceUpdateReturnFields = []string{"__typename"}
	}

	if strings.TrimSpace(cfg.WebGUICommandURL) == "" {
		cfg.WebGUICommandURL = deriveWebGUICommandURL(cfg.Endpoint)
	}
	if strings.TrimSpace(cfg.WebGUIEventsURL) == "" {
		cfg.WebGUIEventsURL = deriveWebGUIEventsURL(cfg.Endpoint)
	}
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) RestartContainerByName(ctx context.Context, name string) error {
	if c.canWebGUIEvents() {
		return c.webGUIRestartContainer(ctx, name)
	}
	id, err := c.findContainerIDByName(ctx, name)
	if err != nil {
		return err
	}
	if err := c.stopContainer(ctx, id); err != nil {
		if !errors.Is(err, ErrAlreadyStopped) {
			return err
		}
	}
	if err := c.startContainer(ctx, id); err != nil {
		if !errors.Is(err, ErrAlreadyStarted) {
			return err
		}
	}
	return nil
}

func (c *Client) StopContainerByName(ctx context.Context, name string) error {
	id, err := c.findContainerIDByName(ctx, name)
	if err != nil {
		return err
	}
	if err := c.stopContainer(ctx, id); err != nil {
		if errors.Is(err, ErrAlreadyStopped) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) ForceUpdateContainerByName(ctx context.Context, name string) error {
	id, err := c.findContainerIDByName(ctx, name)
	if err != nil {
		return err
	}
	if err := c.callDockerForceUpdateMutation(ctx, id); err != nil {
		if isMaybeUnsupportedGraphQL(err) {
			if c.canWebGUICommand() {
				if errWeb := c.webGUIUpdateContainer(ctx, name); errWeb != nil {
					return fmt.Errorf("强制更新失败（GraphQL 不支持）且 WebGUI 兜底失败：%w", errWeb)
				}
				return nil
			}
			return fmt.Errorf("%w；如目标 Unraid 未提供对应 GraphQL mutation，可在 config.yaml 配置 unraid.webgui_csrf_token / unraid.webgui_cookie / unraid.webgui_command_url 使用 WebGUI 更新", err)
		}
		return err
	}
	return nil
}

type ContainerStatus struct {
	ID     string
	Name   string
	State  string
	Status string
	Uptime string
}

type ContainerStats struct {
	ID    string
	Name  string
	Stats interface{}
}

type SystemMetrics struct {
	CPUPercentTotal float64

	MemoryTotal     int64
	MemoryUsed      int64
	MemoryFree      int64
	MemoryAvailable int64
	MemoryPercent   float64

	PerCPU []CPUCoreLoad

	MemoryUsedEffective    int64
	MemoryPercentEffective float64
	HasMemoryEffective     bool

	NetworkRxBytesTotal int64
	NetworkTxBytesTotal int64
	HasNetworkTotals    bool

	// Unraid 系统运行时长（通常为秒）。用于展示“服务启动时长/运行时长”。
	UnraidUptimeSeconds int64
	HasUnraidUptime     bool
	UnraidUptimeNote    string

	// UPS 设备信息（如未配置/未检测到设备，可能为空）。
	UPSDevices []UPSDeviceMetrics
	UPSNote    string
}

type UPSDeviceMetrics struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Model  string `json:"model"`
	Status string `json:"status"`

	Battery *struct {
		ChargeLevel      *float64 `json:"chargeLevel"`
		EstimatedRuntime *float64 `json:"estimatedRuntime"`
		Health           *string  `json:"health"`
	} `json:"battery"`

	Power *struct {
		InputVoltage   *float64 `json:"inputVoltage"`
		OutputVoltage  *float64 `json:"outputVoltage"`
		LoadPercentage *float64 `json:"loadPercentage"`
	} `json:"power"`
}

type CPUCoreLoad struct {
	PercentTotal  float64
	PercentUser   float64
	PercentSystem float64
	PercentNice   float64
	PercentIdle   float64
	PercentIrq    float64
	PercentGuest  float64
	PercentSteal  float64
}

type ContainerLogs struct {
	ID    string
	Name  string
	Tail  int
	Logs  string
	Trunc bool
}

func (c *Client) GetContainerStatusByName(ctx context.Context, name string) (ContainerStatus, error) {
	ct, err := c.findContainerByName(ctx, name)
	if err != nil {
		return ContainerStatus{}, err
	}
	return ContainerStatus{
		ID:     ct.ID,
		Name:   ct.Name,
		State:  ct.State,
		Status: ct.Status,
		Uptime: parseUptimeFromDockerStatus(ct.Status),
	}, nil
}

func (c *Client) GetContainerStatsByName(ctx context.Context, name string) (ContainerStats, error) {
	fieldName, fieldExpr, err := c.buildStatsFieldExpr()
	if err != nil {
		return ContainerStats{}, err
	}

	ct, v, err := c.queryContainerExtraByName(ctx, name, fieldName, fieldExpr)
	if err != nil {
		return ContainerStats{}, wrapMaybeUnsupported(err, "资源统计", []string{
			"unraid.stats_field",
			"unraid.stats_fields",
		})
	}

	return ContainerStats{
		ID:    ct.ID,
		Name:  ct.Name,
		Stats: v,
	}, nil
}

type bigIntString string

func (b *bigIntString) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*b = ""
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		*b = bigIntString(s[1 : len(s)-1])
		return nil
	}
	*b = bigIntString(s)
	return nil
}

type systemMetricsResp struct {
	Metrics struct {
		CPU struct {
			PercentTotal float64 `json:"percentTotal"`
			CPUs         []struct {
				PercentTotal  float64 `json:"percentTotal"`
				PercentUser   float64 `json:"percentUser"`
				PercentSystem float64 `json:"percentSystem"`
				PercentNice   float64 `json:"percentNice"`
				PercentIdle   float64 `json:"percentIdle"`
				PercentIrq    float64 `json:"percentIrq"`
				PercentGuest  float64 `json:"percentGuest"`
				PercentSteal  float64 `json:"percentSteal"`
			} `json:"cpus"`
		} `json:"cpu"`
		Memory struct {
			Total        bigIntString `json:"total"`
			Used         bigIntString `json:"used"`
			Free         bigIntString `json:"free"`
			Available    bigIntString `json:"available"`
			PercentTotal float64      `json:"percentTotal"`
		} `json:"memory"`
	} `json:"metrics"`
}

func parseInt64FromBigIntString(s bigIntString) int64 {
	v := strings.TrimSpace(string(s))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func (c *Client) GetSystemMetrics(ctx context.Context) (SystemMetrics, error) {
	const q = `query { metrics { cpu { percentTotal cpus { percentTotal percentUser percentSystem percentNice percentIdle percentIrq percentGuest percentSteal } } memory { total used free available percentTotal } } }`

	var resp systemMetricsResp
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return SystemMetrics{}, err
	}

	out := SystemMetrics{
		CPUPercentTotal: resp.Metrics.CPU.PercentTotal,
		MemoryTotal:     parseInt64FromBigIntString(resp.Metrics.Memory.Total),
		MemoryUsed:      parseInt64FromBigIntString(resp.Metrics.Memory.Used),
		MemoryFree:      parseInt64FromBigIntString(resp.Metrics.Memory.Free),
		MemoryAvailable: parseInt64FromBigIntString(resp.Metrics.Memory.Available),
		MemoryPercent:   resp.Metrics.Memory.PercentTotal,
	}
	if out.MemoryTotal > 0 && out.MemoryAvailable >= 0 && out.MemoryAvailable <= out.MemoryTotal {
		out.MemoryUsedEffective = out.MemoryTotal - out.MemoryAvailable
		out.MemoryPercentEffective = float64(out.MemoryUsedEffective) / float64(out.MemoryTotal) * 100
		out.HasMemoryEffective = true
	}

	if len(resp.Metrics.CPU.CPUs) > 0 {
		out.PerCPU = make([]CPUCoreLoad, 0, len(resp.Metrics.CPU.CPUs))
		for _, c := range resp.Metrics.CPU.CPUs {
			out.PerCPU = append(out.PerCPU, CPUCoreLoad{
				PercentTotal:  c.PercentTotal,
				PercentUser:   c.PercentUser,
				PercentSystem: c.PercentSystem,
				PercentNice:   c.PercentNice,
				PercentIdle:   c.PercentIdle,
				PercentIrq:    c.PercentIrq,
				PercentGuest:  c.PercentGuest,
				PercentSteal:  c.PercentSteal,
			})
		}
	}

	if rx, tx, ok, err := c.getDockerNetworkIOTotals(ctx); err == nil && ok {
		out.NetworkRxBytesTotal = rx
		out.NetworkTxBytesTotal = tx
		out.HasNetworkTotals = true
	}

	if seconds, ok, err := c.getUnraidUptimeSeconds(ctx); err != nil {
		out.UnraidUptimeNote = "未获取到"
	} else if ok {
		out.UnraidUptimeSeconds = seconds
		out.HasUnraidUptime = true
	} else {
		out.UnraidUptimeNote = "未获取到"
	}

	if devices, ok, err := c.getUPSDevices(ctx); err != nil {
		out.UPSNote = "未获取到"
	} else if ok {
		if len(devices) == 0 {
			out.UPSNote = "未检测到"
		} else {
			out.UPSDevices = devices
		}
	} else {
		out.UPSNote = "未获取到"
	}

	return out, nil
}

func (c *Client) getUnraidUptimeSeconds(ctx context.Context) (seconds int64, ok bool, err error) {
	const q = `query { info { os { uptime } } }`
	var resp struct {
		Info struct {
			OS struct {
				Uptime interface{} `json:"uptime"`
			} `json:"os"`
		} `json:"info"`
	}
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return 0, false, err
	}
	seconds, ok = parseNumberishToInt64(resp.Info.OS.Uptime)
	return seconds, ok, nil
}

func (c *Client) getUPSDevices(ctx context.Context) ([]UPSDeviceMetrics, bool, error) {
	const q = `query { upsDevices { id name model status battery { chargeLevel estimatedRuntime health } power { inputVoltage outputVoltage loadPercentage } } }`

	var resp struct {
		UPSDevices []UPSDeviceMetrics `json:"upsDevices"`
	}
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return nil, false, err
	}
	return resp.UPSDevices, true, nil
}

func parseNumberishToInt64(v interface{}) (int64, bool) {
	switch vv := v.(type) {
	case nil:
		return 0, false
	case float64:
		return int64(vv), true
	case int64:
		return vv, true
	case json.Number:
		n, err := vv.Int64()
		if err != nil {
			return 0, false
		}
		return n, true
	case string:
		s := strings.TrimSpace(vv)
		if s == "" {
			return 0, false
		}
		// uptime 可能为整型秒，也可能被序列化为字符串数字。
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func (c *Client) getDockerNetworkIOTotals(ctx context.Context) (rxTotal int64, txTotal int64, ok bool, err error) {
	fieldName := strings.TrimSpace(c.cfg.StatsField)
	if fieldName == "" {
		return 0, 0, false, nil
	}

	// 优先尝试 stats 为对象形态：{ netIO }
	var raw map[string]interface{}
	qObj := fmt.Sprintf(`query { docker { containers { %s { netIO } } } }`, fieldName)
	if err := c.do(ctx, qObj, nil, &raw); err != nil {
		// 兼容：部分实现 stats 为标量（JSON/string），不支持 selection set。
		if !isMaybeUnsupportedGraphQL(err) {
			return 0, 0, false, err
		}
		qScalar := fmt.Sprintf(`query { docker { containers { %s } } }`, fieldName)
		raw = nil
		if err2 := c.do(ctx, qScalar, nil, &raw); err2 != nil {
			return 0, 0, false, err2
		}
	}

	dockerObj, _ := raw["docker"].(map[string]interface{})
	list, _ := dockerObj["containers"].([]interface{})

	var parsed bool
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		netIO, ok := extractDockerNetIOString(m[fieldName])
		if !ok {
			continue
		}
		rx, tx, ok := parseDockerNetIO(netIO)
		if !ok {
			continue
		}
		rxTotal += rx
		txTotal += tx
		parsed = true
	}
	if !parsed {
		return 0, 0, false, nil
	}
	return rxTotal, txTotal, true, nil
}

func extractDockerNetIOString(v interface{}) (string, bool) {
	switch vv := v.(type) {
	case nil:
		return "", false
	case string:
		s := strings.TrimSpace(vv)
		return s, s != ""
	case map[string]interface{}:
		s, _ := vv["netIO"].(string)
		s = strings.TrimSpace(s)
		return s, s != ""
	default:
		return "", false
	}
}

func parseDockerNetIO(s string) (rxBytes int64, txBytes int64, ok bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	rx, ok1 := parseHumanBytes(parts[0])
	tx, ok2 := parseHumanBytes(parts[1])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return rx, tx, true
}

func parseHumanBytes(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	// 支持：12.3kB / 12.3 kB / 12.3KiB / 123B
	var i int
	for i < len(s) {
		b := s[i]
		if (b >= '0' && b <= '9') || b == '.' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return 0, false
	}

	numPart := strings.TrimSpace(s[:i])
	unitPart := strings.TrimSpace(s[i:])
	if unitPart == "" {
		unitPart = "B"
	}

	v, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, false
	}

	u := strings.ToUpper(strings.ReplaceAll(unitPart, " ", ""))
	var mul float64
	switch u {
	case "B":
		mul = 1
	case "KB", "K":
		mul = 1000
	case "MB":
		mul = 1000 * 1000
	case "GB":
		mul = 1000 * 1000 * 1000
	case "TB":
		mul = 1000 * 1000 * 1000 * 1000
	case "KIB":
		mul = 1024
	case "MIB":
		mul = 1024 * 1024
	case "GIB":
		mul = 1024 * 1024 * 1024
	case "TIB":
		mul = 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}

	bytes := v * mul
	if bytes < 0 {
		return 0, false
	}
	// 仅用于统计展示，保留四舍五入即可。
	return int64(bytes + 0.5), true
}

func (c *Client) GetContainerLogsByName(ctx context.Context, name string, tail int) (ContainerLogs, error) {
	tail = clampInt(tail, 1, 200)
	fieldName, fieldExpr, extractPath, err := c.buildLogsFieldExpr(tail)
	if err != nil {
		return ContainerLogs{}, err
	}

	ct, v, err := c.queryContainerExtraByName(ctx, name, fieldName, fieldExpr)
	if err != nil {
		return ContainerLogs{}, wrapMaybeUnsupported(err, "日志", []string{
			"unraid.logs_field",
			"unraid.logs_tail_arg",
			"unraid.logs_payload_field",
		})
	}

	raw, ok := extractByPath(v, extractPath)
	if !ok {
		raw = v
	}

	logs, ok := stringifyGraphQLValue(raw)
	if !ok {
		return ContainerLogs{}, fmt.Errorf("日志返回类型不支持: %T", raw)
	}

	logs, truncated := tailLines(logs, tail)

	return ContainerLogs{
		ID:    ct.ID,
		Name:  ct.Name,
		Tail:  tail,
		Logs:  logs,
		Trunc: truncated,
	}, nil
}

var (
	ErrAlreadyStopped = errors.New("container already stopped")
	ErrAlreadyStarted = errors.New("container already started")
)

type containerInfo struct {
	ID     string
	Name   string
	State  string
	Status string
}

func (c *Client) findContainerIDByName(ctx context.Context, name string) (string, error) {
	ct, err := c.findContainerByName(ctx, name)
	if err != nil {
		return "", err
	}
	return ct.ID, nil
}

func (c *Client) findContainerByName(ctx context.Context, name string) (containerInfo, error) {
	const q = `query { docker { containers { id names state status } } }`
	var resp struct {
		Docker struct {
			Containers []struct {
				ID     string      `json:"id"`
				Names  interface{} `json:"names"`
				State  string      `json:"state"`
				Status string      `json:"status"`
			} `json:"containers"`
		} `json:"docker"`
	}
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return containerInfo{}, err
	}

	seen := make(map[string]struct{}, 64)
	var candidates []string
	want := normalizeName(name)
	for _, ct := range resp.Docker.Containers {
		for _, n := range normalizeContainerNames(ct.Names) {
			nn := normalizeName(n)
			if nn == want {
				return containerInfo{
					ID:     normalizePrefixedID(ct.ID),
					Name:   nn,
					State:  ct.State,
					Status: ct.Status,
				}, nil
			}
			if nn == "" {
				continue
			}
			if _, ok := seen[nn]; ok {
				continue
			}
			seen[nn] = struct{}{}
			candidates = append(candidates, nn)
		}
	}

	sort.Strings(candidates)
	if len(candidates) > 0 {
		const max = 10
		if len(candidates) > max {
			candidates = candidates[:max]
		}
		return containerInfo{}, fmt.Errorf("未找到容器：%s（可选容器示例：%s）", name, strings.Join(candidates, ", "))
	}
	return containerInfo{}, fmt.Errorf("未找到容器：%s", name)
}

func (c *Client) stopContainer(ctx context.Context, id string) error {
	const q = `mutation Stop($dockerId: PrefixedID!) { docker { stop(id: $dockerId) { id state status } } }`
	var resp struct {
		Docker struct {
			Stop struct {
				State string `json:"state"`
			} `json:"stop"`
		} `json:"docker"`
	}
	if err := c.do(ctx, q, map[string]interface{}{"dockerId": id}, &resp); err != nil {
		if strings.Contains(err.Error(), "already") && strings.Contains(err.Error(), "stopped") {
			return ErrAlreadyStopped
		}
		return err
	}
	if strings.EqualFold(resp.Docker.Stop.State, "exited") || strings.EqualFold(resp.Docker.Stop.State, "stopped") {
		return nil
	}
	return nil
}

func (c *Client) startContainer(ctx context.Context, id string) error {
	const q = `mutation Start($dockerId: PrefixedID!) { docker { start(id: $dockerId) { id state status } } }`
	var resp struct {
		Docker struct {
			Start struct {
				State string `json:"state"`
			} `json:"start"`
		} `json:"docker"`
	}
	if err := c.do(ctx, q, map[string]interface{}{"dockerId": id}, &resp); err != nil {
		if strings.Contains(err.Error(), "already") && strings.Contains(err.Error(), "started") {
			return ErrAlreadyStarted
		}
		return err
	}
	return nil
}

func normalizePrefixedID(id string) string {
	if parts := strings.SplitN(id, ":", 2); len(parts) == 2 {
		return parts[1]
	}
	return id
}

func normalizeName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

func normalizeContainerNames(v interface{}) []string {
	switch vv := v.(type) {
	case string:
		return []string{vv}
	case []interface{}:
		var ret []string
		for _, item := range vv {
			if s, ok := item.(string); ok {
				ret = append(ret, s)
			}
		}
		return ret
	default:
		return nil
	}
}

func (c *Client) do(ctx context.Context, query string, variables map[string]interface{}, out interface{}) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	if c.cfg.Origin != "" {
		req.Header.Set("Origin", c.cfg.Origin)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("unraid graphql http status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}

	var raw graphQLResponse
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		var msgs []string
		for _, e := range raw.Errors {
			if e.Message != "" {
				msgs = append(msgs, e.Message)
			}
		}
		if len(msgs) == 0 {
			return errors.New("graphql error")
		}
		return fmt.Errorf("graphql error: %s", strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw.Data, out)
}

func deriveWebGUICommandURL(endpoint string) string {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return ""
	}

	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/graphql") {
		path = strings.TrimSuffix(path, "/graphql")
	}
	u.Path = strings.TrimSuffix(path, "/") + "/webGui/include/StartCommand.php"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func deriveWebGUIEventsURL(endpoint string) string {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return ""
	}

	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/graphql") {
		path = strings.TrimSuffix(path, "/graphql")
	}
	u.Path = strings.TrimSuffix(path, "/") + "/plugins/dynamix.docker.manager/include/Events.php"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isMaybeUnsupportedGraphQL(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot query field") ||
		strings.Contains(msg, "Unknown argument") ||
		strings.Contains(msg, "Unknown type") ||
		strings.HasPrefix(msg, "graphql error:")
}

func (c *Client) canWebGUICommand() bool {
	return strings.TrimSpace(c.cfg.WebGUICommandURL) != "" && strings.TrimSpace(c.cfg.WebGUICSRFToken) != ""
}

func (c *Client) canWebGUIEvents() bool {
	return strings.TrimSpace(c.cfg.WebGUIEventsURL) != "" && strings.TrimSpace(c.cfg.WebGUICSRFToken) != ""
}

func (c *Client) webGUIRestartContainer(ctx context.Context, name string) error {
	id, err := c.findContainerIDByName(ctx, name)
	if err != nil {
		return err
	}
	return c.doWebGUIEvent(ctx, "restart", id)
}

func (c *Client) webGUIUpdateContainer(ctx context.Context, name string) error {
	cmd := "update_container " + normalizeName(name)
	return c.doWebGUICommand(ctx, cmd)
}

func (c *Client) doWebGUICommand(ctx context.Context, cmd string) error {
	cmdURL := strings.TrimSpace(c.cfg.WebGUICommandURL)
	if cmdURL == "" {
		cmdURL = deriveWebGUICommandURL(c.cfg.Endpoint)
	}
	csrf := strings.TrimSpace(c.cfg.WebGUICSRFToken)
	if cmdURL == "" || csrf == "" {
		return errors.New("未配置 WebGUI StartCommand 兜底（需配置 unraid.webgui_csrf_token，可选 unraid.webgui_cookie / unraid.webgui_command_url）")
	}

	form := url.Values{}
	form.Set("cmd", cmd)
	form.Set("start", "0")
	form.Set("csrf_token", csrf)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cmdURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if cookie := strings.TrimSpace(c.cfg.WebGUICookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	body := strings.TrimSpace(string(b))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("unraid webgui http status %d: %s", res.StatusCode, body)
	}

	contentType := strings.ToLower(strings.TrimSpace(res.Header.Get("Content-Type")))
	bodyLower := strings.ToLower(body)
	if strings.Contains(bodyLower, "invalid csrf") || strings.Contains(bodyLower, "csrf") && strings.Contains(bodyLower, "invalid") {
		return errors.New("unraid webgui csrf_token 无效或已过期")
	}
	if strings.Contains(contentType, "text/html") && strings.Contains(bodyLower, "login") && strings.Contains(bodyLower, "password") {
		return errors.New("unraid webgui 可能未登录（请配置 unraid.webgui_cookie）或 csrf_token 已失效")
	}
	return nil
}

func (c *Client) doWebGUIEvent(ctx context.Context, action string, containerID string) error {
	eventsURL := strings.TrimSpace(c.cfg.WebGUIEventsURL)
	if eventsURL == "" {
		eventsURL = deriveWebGUIEventsURL(c.cfg.Endpoint)
	}
	csrf := strings.TrimSpace(c.cfg.WebGUICSRFToken)
	if eventsURL == "" || csrf == "" {
		return errors.New("未配置 WebGUI Events 兜底（需配置 unraid.webgui_csrf_token，可选 unraid.webgui_cookie / unraid.webgui_events_url）")
	}
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id 不能为空")
	}

	form := url.Values{}
	form.Set("action", action)
	form.Set("container", containerID)
	form.Set("csrf_token", csrf)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, eventsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if cookie := strings.TrimSpace(c.cfg.WebGUICookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	body := strings.TrimSpace(string(b))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("unraid webgui http status %d: %s", res.StatusCode, body)
	}

	contentType := strings.ToLower(strings.TrimSpace(res.Header.Get("Content-Type")))
	bodyLower := strings.ToLower(body)
	if strings.Contains(bodyLower, "invalid csrf") || strings.Contains(bodyLower, "csrf") && strings.Contains(bodyLower, "invalid") {
		return errors.New("unraid webgui csrf_token 无效或已过期")
	}
	if strings.Contains(contentType, "text/html") && strings.Contains(bodyLower, "login") && strings.Contains(bodyLower, "password") {
		return errors.New("unraid webgui 可能未登录（请配置 unraid.webgui_cookie）或 csrf_token 已失效")
	}
	return nil
}

func (c *Client) queryContainerExtraByName(ctx context.Context, name string, fieldName string, fieldExpr string) (containerInfo, interface{}, error) {
	q := fmt.Sprintf(`query { docker { containers { id names state status %s } } }`, fieldExpr)

	var raw map[string]interface{}
	if err := c.do(ctx, q, nil, &raw); err != nil {
		return containerInfo{}, nil, err
	}

	dockerObj, _ := raw["docker"].(map[string]interface{})
	list, _ := dockerObj["containers"].([]interface{})

	ct, m, err := pickContainerObjectByName(list, name)
	if err != nil {
		return containerInfo{}, nil, err
	}

	v := m[fieldName]
	return ct, v, nil
}

func pickContainerObjectByName(containers []interface{}, name string) (containerInfo, map[string]interface{}, error) {
	want := normalizeName(name)
	seen := make(map[string]struct{}, 64)
	var candidates []string

	for _, item := range containers {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		state, _ := m["state"].(string)
		status, _ := m["status"].(string)
		for _, n := range normalizeContainerNames(m["names"]) {
			nn := normalizeName(n)
			if nn == want {
				return containerInfo{
					ID:     normalizePrefixedID(id),
					Name:   nn,
					State:  state,
					Status: status,
				}, m, nil
			}
			if nn == "" {
				continue
			}
			if _, ok := seen[nn]; ok {
				continue
			}
			seen[nn] = struct{}{}
			candidates = append(candidates, nn)
		}
	}

	sort.Strings(candidates)
	if len(candidates) > 0 {
		const max = 10
		if len(candidates) > max {
			candidates = candidates[:max]
		}
		return containerInfo{}, nil, fmt.Errorf("未找到容器：%s（可选容器示例：%s）", name, strings.Join(candidates, ", "))
	}
	return containerInfo{}, nil, fmt.Errorf("未找到容器：%s", name)
}

func (c *Client) buildStatsFieldExpr() (fieldName string, fieldExpr string, err error) {
	fieldName = strings.TrimSpace(c.cfg.StatsField)
	if fieldName == "" {
		return "", "", errors.New("未配置 unraid.stats_field")
	}

	fields := c.cfg.StatsFields
	if len(fields) == 0 {
		return fieldName, fieldName, nil
	}
	return fieldName, fieldName + " { " + strings.Join(fields, " ") + " }", nil
}

func (c *Client) buildLogsFieldExpr(tail int) (fieldName string, fieldExpr string, extractPath []string, err error) {
	fieldName = strings.TrimSpace(c.cfg.LogsField)
	if fieldName == "" {
		return "", "", nil, errors.New("未配置 unraid.logs_field")
	}

	var tailArg string
	if c.cfg.LogsTailArg != nil {
		tailArg = strings.TrimSpace(*c.cfg.LogsTailArg)
	}

	fieldExpr = fieldName
	if tailArg != "" {
		fieldExpr = fmt.Sprintf("%s(%s: %d)", fieldName, tailArg, tail)
	}

	payload := strings.TrimSpace(c.cfg.LogsPayloadField)
	if payload != "" {
		fieldExpr = fieldExpr + " { " + payload + " }"
		extractPath = []string{payload}
	}
	return fieldName, fieldExpr, extractPath, nil
}

func wrapMaybeUnsupported(err error, feature string, cfgKeys []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "graphql error:") ||
		strings.Contains(msg, "Cannot query field") ||
		strings.Contains(msg, "Unknown argument") ||
		strings.Contains(msg, "Unknown type") {
		return fmt.Errorf("%s查询失败：%w（可在 config.yaml 配置 %s）", feature, err, strings.Join(cfgKeys, " / "))
	}
	return err
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

func extractByPath(v interface{}, path []string) (interface{}, bool) {
	cur := v
	for _, p := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func stringifyGraphQLValue(v interface{}) (string, bool) {
	switch vv := v.(type) {
	case nil:
		return "", true
	case string:
		return vv, true
	case []interface{}:
		var lines []string
		for _, item := range vv {
			s, ok := item.(string)
			if !ok {
				continue
			}
			lines = append(lines, s)
		}
		return strings.Join(lines, "\n"), true
	default:
		return "", false
	}
}

func tailLines(s string, want int) (string, bool) {
	if want <= 0 {
		return s, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= want {
		return s, false
	}
	return strings.Join(lines[len(lines)-want:], "\n"), true
}

func parseUptimeFromDockerStatus(status string) string {
	s := strings.TrimSpace(status)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "Up ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "Up "))
	} else if strings.HasPrefix(s, "Up") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "Up"))
	} else {
		return ""
	}
	if idx := strings.Index(s, "("); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

func (c *Client) callDockerForceUpdateMutation(ctx context.Context, id string) error {
	argName := strings.TrimSpace(c.cfg.ForceUpdateArgName)
	if argName == "" {
		argName = "id"
	}
	argType := strings.TrimSpace(c.cfg.ForceUpdateArgType)
	if argType == "" {
		argType = "PrefixedID!"
	}

	selection := ""
	if len(c.cfg.ForceUpdateReturnFields) > 0 {
		selection = " { " + strings.Join(c.cfg.ForceUpdateReturnFields, " ") + " }"
	}

	cfgKeys := []string{
		"unraid.force_update_mutation",
		"unraid.force_update_arg",
		"unraid.force_update_arg_type",
		"unraid.force_update_return_fields",
	}

	mutations := forceUpdateMutationCandidates(c.cfg.ForceUpdateMutation)
	if len(mutations) == 0 {
		return errors.New("未配置强制更新 mutation")
	}

	var lastErr error
	for idxMutation, mutation := range mutations {
		q := fmt.Sprintf(`mutation ForceUpdate($v: %s) { docker { %s(%s: $v)%s } }`, argType, mutation, argName, selection)
		if err := c.do(ctx, q, map[string]interface{}{"v": id}, nil); err != nil {
			lastErr = err
			// 兼容：不同 Unraid Connect 版本/实现可能使用不同的 mutation 名称。
			// 仅当明确为“字段不存在（Cannot query field）”时才回退尝试下一个候选，避免重复执行真实更新操作。
			if idxMutation+1 < len(mutations) && isCannotQueryField(err, mutation) {
				continue
			}
			return wrapMaybeUnsupported(err, "强制更新", cfgKeys)
		}
		return nil
	}
	if lastErr != nil {
		return wrapMaybeUnsupported(lastErr, "强制更新", cfgKeys)
	}
	return nil
}

func forceUpdateMutationCandidates(configured string) []string {
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, exists := range out {
			if exists == v {
				return
			}
		}
		out = append(out, v)
	}

	add(configured)

	// 目前已知的常见名称（按优先级）：
	// - updateContainer: Unraid API DockerMutations 常见实现
	// - update: 历史/兼容命名（部分实现）
	add("updateContainer")
	add("update")

	return out
}

func isCannotQueryField(err error, field string) bool {
	if err == nil {
		return false
	}
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "Cannot query field") {
		return false
	}

	candidates := []string{
		`Cannot query field "` + field + `"`,
		`Cannot query field \"` + field + `\"`,
		`Cannot query field \\\"` + field + `\\\"`,
		`Cannot query field '` + field + `'`,
	}
	for _, c := range candidates {
		if strings.Contains(msg, c) {
			return true
		}
	}

	// 兜底：不同实现的转义/引号风格可能不一致，但仍会同时包含固定前缀与字段名。
	prefixIdx := strings.Index(msg, "Cannot query field")
	if prefixIdx < 0 {
		return false
	}
	return containsGraphQLIdentifierToken(msg[prefixIdx:], field)
}

func containsGraphQLIdentifierToken(s string, token string) bool {
	if token == "" {
		return false
	}
	for idx := strings.Index(s, token); idx >= 0; {
		beforeOK := idx == 0 || !isGraphQLIdentifierChar(s[idx-1])
		after := idx + len(token)
		afterOK := after >= len(s) || !isGraphQLIdentifierChar(s[after])
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(s[idx+1:], token)
		if next < 0 {
			break
		}
		idx += next + 1
	}
	return false
}

func isGraphQLIdentifierChar(b byte) bool {
	if b == '_' {
		return true
	}
	if b >= '0' && b <= '9' {
		return true
	}
	if b >= 'A' && b <= 'Z' {
		return true
	}
	if b >= 'a' && b <= 'z' {
		return true
	}
	return false
}
