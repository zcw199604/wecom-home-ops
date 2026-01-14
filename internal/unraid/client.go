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
	"sort"
	"strings"
)

type ClientConfig struct {
	Endpoint string
	APIKey   string
	Origin   string

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
	if strings.TrimSpace(c.cfg.ForceUpdateMutation) == "" {
		return errors.New("未配置 unraid.force_update_mutation（已移除 introspection 探测，请在 config.yaml 显式指定）")
	}
	return c.callDockerForceUpdateMutation(ctx, id)
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
	mutation := strings.TrimSpace(c.cfg.ForceUpdateMutation)
	if mutation == "" {
		return errors.New("未配置 unraid.force_update_mutation")
	}
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

	q := fmt.Sprintf(`mutation ForceUpdate($v: %s) { docker { %s(%s: $v)%s } }`, argType, mutation, argName, selection)
	if err := c.do(ctx, q, map[string]interface{}{"v": id}, nil); err != nil {
		return wrapMaybeUnsupported(err, "强制更新", []string{
			"unraid.force_update_mutation",
			"unraid.force_update_arg",
			"unraid.force_update_arg_type",
			"unraid.force_update_return_fields",
		})
	}
	return nil
}
