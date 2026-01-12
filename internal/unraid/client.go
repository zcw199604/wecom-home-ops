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
	"sync"
	"time"
)

type ClientConfig struct {
	Endpoint            string
	APIKey              string
	Origin              string
	ForceUpdateMutation string
}

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client

	mu               sync.Mutex
	inspectMeta      containerInspectMeta
	inspectMetaExpAt time.Time
	inspectMetaOK    bool
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
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

	meta, supported, err := c.detectDockerForceUpdateMutation(ctx)
	if err != nil {
		return err
	}
	if !supported {
		return fmt.Errorf("当前 Unraid GraphQL API 未发现可用的“强制更新”mutation（可升级 Unraid Connect 插件或更换实现路径）")
	}

	return c.callDockerForceUpdateMutation(ctx, meta, id)
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
	meta, err := c.getContainerInspectMeta(ctx)
	if err != nil {
		return ContainerStats{}, err
	}
	if meta.ContainerTypeName == "" || meta.StatsField == nil {
		return ContainerStats{}, errors.New("当前 Unraid GraphQL API 未发现可用的资源统计字段（可能需要升级 Unraid Connect 插件或更换实现路径）")
	}

	fieldExpr, err := c.buildStatsFieldExpr(ctx, *meta.StatsField)
	if err != nil {
		return ContainerStats{}, err
	}

	ct, v, err := c.queryContainerExtraByName(ctx, name, meta.StatsField.Name, fieldExpr, "", nil)
	if err != nil {
		return ContainerStats{}, err
	}

	return ContainerStats{
		ID:    ct.ID,
		Name:  ct.Name,
		Stats: v,
	}, nil
}

func (c *Client) GetContainerLogsByName(ctx context.Context, name string, tail int) (ContainerLogs, error) {
	tail = clampInt(tail, 1, 200)
	meta, err := c.getContainerInspectMeta(ctx)
	if err != nil {
		return ContainerLogs{}, err
	}
	if meta.ContainerTypeName == "" || meta.LogsField == nil {
		return ContainerLogs{}, errors.New("当前 Unraid GraphQL API 未发现可用的日志查询字段（可能需要升级 Unraid Connect 插件或更换实现路径）")
	}

	fieldExpr, varDef, vars, extractPath, err := c.buildLogsFieldExpr(ctx, *meta.LogsField, tail)
	if err != nil {
		return ContainerLogs{}, err
	}

	ct, v, err := c.queryContainerExtraByName(ctx, name, meta.LogsField.Name, fieldExpr, varDef, vars)
	if err != nil {
		return ContainerLogs{}, err
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

type containerInspectMeta struct {
	DockerQueryTypeName string
	ContainerTypeName   string
	LogsField           *gqlFieldMeta
	StatsField          *gqlFieldMeta
}

func (c *Client) getContainerInspectMeta(ctx context.Context) (containerInspectMeta, error) {
	c.mu.Lock()
	if c.inspectMetaOK && time.Now().Before(c.inspectMetaExpAt) {
		meta := c.inspectMeta
		c.mu.Unlock()
		return meta, nil
	}
	c.mu.Unlock()

	meta, err := c.detectContainerInspectMeta(ctx)
	if err != nil {
		return containerInspectMeta{}, err
	}

	c.mu.Lock()
	c.inspectMeta = meta
	c.inspectMetaOK = true
	c.inspectMetaExpAt = time.Now().Add(10 * time.Minute)
	c.mu.Unlock()
	return meta, nil
}

func (c *Client) detectContainerInspectMeta(ctx context.Context) (containerInspectMeta, error) {
	dockerTypeName, err := c.lookupDockerQueryTypeName(ctx)
	if err != nil {
		return containerInspectMeta{}, err
	}
	if dockerTypeName == "" {
		return containerInspectMeta{}, nil
	}

	fields, err := c.lookupTypeFieldsMeta(ctx, dockerTypeName)
	if err != nil {
		return containerInspectMeta{}, err
	}

	containersField, ok := fields["containers"]
	if !ok {
		return containerInspectMeta{}, nil
	}

	containerTypeName := containersField.Type.NamedTypeName()
	if containerTypeName == "" {
		return containerInspectMeta{}, nil
	}

	containerFields, err := c.lookupTypeFieldsMeta(ctx, containerTypeName)
	if err != nil {
		return containerInspectMeta{}, err
	}

	var logsField *gqlFieldMeta
	if f, ok := pickField(containerFields, []string{
		"logs",
		"log",
		"containerLogs",
		"dockerLogs",
	}); ok {
		ff := f
		logsField = &ff
	} else if f, ok := pickFieldByContains(containerFields, []string{"log"}); ok {
		ff := f
		logsField = &ff
	}

	var statsField *gqlFieldMeta
	if f, ok := pickField(containerFields, []string{
		"stats",
		"stat",
		"metrics",
	}); ok {
		ff := f
		statsField = &ff
	} else if f, ok := pickFieldByContains(containerFields, []string{"stat", "metric"}); ok {
		ff := f
		statsField = &ff
	}

	return containerInspectMeta{
		DockerQueryTypeName: dockerTypeName,
		ContainerTypeName:   containerTypeName,
		LogsField:           logsField,
		StatsField:          statsField,
	}, nil
}

func (c *Client) lookupDockerQueryTypeName(ctx context.Context) (string, error) {
	const q = `query { __schema { queryType { fields { name type { name kind ofType { name kind ofType { name kind } } } } } } }`
	var resp struct {
		Schema struct {
			QueryType struct {
				Fields []struct {
					Name string `json:"name"`
					Type struct {
						Name   string `json:"name"`
						Kind   string `json:"kind"`
						OfType *struct {
							Name   string `json:"name"`
							Kind   string `json:"kind"`
							OfType *struct {
								Name string `json:"name"`
								Kind string `json:"kind"`
							} `json:"ofType"`
						} `json:"ofType"`
					} `json:"type"`
				} `json:"fields"`
			} `json:"queryType"`
		} `json:"__schema"`
	}
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return "", err
	}

	for _, f := range resp.Schema.QueryType.Fields {
		if f.Name != "docker" {
			continue
		}
		if f.Type.Name != "" {
			return f.Type.Name, nil
		}
		if f.Type.OfType != nil && f.Type.OfType.Name != "" {
			return f.Type.OfType.Name, nil
		}
	}
	return "", nil
}

func (c *Client) queryContainerExtraByName(
	ctx context.Context,
	name string,
	fieldName string,
	fieldExpr string,
	varDef string,
	vars map[string]interface{},
) (containerInfo, interface{}, error) {
	varDef = strings.TrimSpace(varDef)
	header := "query"
	if varDef != "" {
		header += varDef
	}
	q := fmt.Sprintf(`%s { docker { containers { id names state status %s } } }`, header, fieldExpr)

	var raw map[string]interface{}
	if err := c.do(ctx, q, vars, &raw); err != nil {
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

func (c *Client) buildStatsFieldExpr(ctx context.Context, field gqlFieldMeta) (string, error) {
	for _, a := range field.Args {
		if a.Type.Kind == "NON_NULL" {
			return "", fmt.Errorf("资源统计字段需要必填参数，暂不支持: %s.%s", c.inspectMeta.ContainerTypeName, field.Name)
		}
	}

	if !field.Type.RequiresSelectionSet() {
		return field.Name, nil
	}

	typeName := field.Type.NamedTypeName()
	if typeName == "" {
		return field.Name + ` { __typename }`, nil
	}

	fields, err := c.lookupTypeFieldsMeta(ctx, typeName)
	if err != nil {
		return field.Name + ` { __typename }`, nil
	}

	var picks []string
	for name, meta := range fields {
		if meta.Type.RequiresSelectionSet() {
			continue
		}
		picks = append(picks, name)
	}
	sort.Strings(picks)
	if len(picks) == 0 {
		picks = []string{"__typename"}
	}
	return field.Name + " { " + strings.Join(picks, " ") + " }", nil
}

func (c *Client) buildLogsFieldExpr(ctx context.Context, field gqlFieldMeta, tail int) (fieldExpr string, varDef string, vars map[string]interface{}, extractPath []string, err error) {
	limitArg, ok := pickLogsLimitArg(field.Args)
	if ok {
		varDef = fmt.Sprintf("($tail: %s)", limitArg.Type.String())
		vars = map[string]interface{}{"tail": tail}
		fieldExpr = fmt.Sprintf("%s(%s: $tail)", field.Name, limitArg.Name)
	} else {
		fieldExpr = field.Name
	}

	for _, a := range field.Args {
		if a.Type.Kind != "NON_NULL" {
			continue
		}
		if ok && a.Name == limitArg.Name {
			continue
		}
		return "", "", nil, nil, fmt.Errorf("日志字段需要必填参数，暂不支持: %s.%s", c.inspectMeta.ContainerTypeName, field.Name)
	}

	if !field.Type.RequiresSelectionSet() {
		return fieldExpr, varDef, vars, nil, nil
	}

	typeName := field.Type.NamedTypeName()
	if typeName == "" {
		return "", "", nil, nil, fmt.Errorf("日志字段返回类型未知，暂不支持: %s.%s", c.inspectMeta.ContainerTypeName, field.Name)
	}

	fields, err2 := c.lookupTypeFieldsMeta(ctx, typeName)
	if err2 != nil {
		return "", "", nil, nil, fmt.Errorf("日志字段返回类型 introspection 失败: %v", err2)
	}

	payload, payloadPath, ok := pickLogsPayloadField(fields)
	if !ok {
		return "", "", nil, nil, fmt.Errorf("日志字段返回类型结构复杂，暂不支持: %s", typeName)
	}

	if payload.Type.RequiresSelectionSet() {
		return "", "", nil, nil, fmt.Errorf("日志字段返回类型结构复杂，暂不支持: %s.%s", typeName, payload.Name)
	}

	fieldExpr = fieldExpr + fmt.Sprintf(" { %s }", payload.Name)
	return fieldExpr, varDef, vars, payloadPath, nil
}

func pickLogsLimitArg(args []gqlArgMeta) (gqlArgMeta, bool) {
	prefer := []string{"tail", "lines", "limit", "count", "n"}
	for _, name := range prefer {
		for _, a := range args {
			if a.Name == name {
				return a, true
			}
		}
	}
	for _, a := range args {
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, "tail") || strings.Contains(lower, "line") || strings.Contains(lower, "limit") || strings.Contains(lower, "count") {
			return a, true
		}
	}
	return gqlArgMeta{}, false
}

func pickLogsPayloadField(fields map[string]gqlFieldMeta) (gqlFieldMeta, []string, bool) {
	prefer := []string{"lines", "content", "text", "message", "log", "logs", "value", "raw", "data"}
	for _, name := range prefer {
		if f, ok := fields[name]; ok {
			return f, []string{name}, true
		}
	}
	for name, f := range fields {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "line") || strings.Contains(lower, "content") || strings.Contains(lower, "text") || strings.Contains(lower, "message") {
			return f, []string{name}, true
		}
	}
	return gqlFieldMeta{}, nil, false
}

func pickField(fields map[string]gqlFieldMeta, candidates []string) (gqlFieldMeta, bool) {
	for _, name := range candidates {
		if f, ok := fields[name]; ok {
			return f, true
		}
	}
	return gqlFieldMeta{}, false
}

func pickFieldByContains(fields map[string]gqlFieldMeta, keywords []string) (gqlFieldMeta, bool) {
	var names []string
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		lower := strings.ToLower(name)
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return fields[name], true
			}
		}
	}
	return gqlFieldMeta{}, false
}

func (t gqlTypeRef) NamedTypeName() string {
	if t.Name != "" {
		return t.Name
	}
	if t.OfType != nil {
		return t.OfType.NamedTypeName()
	}
	return ""
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

type dockerMutationMeta struct {
	FieldName            string
	ArgName              string
	ArgType              string
	ReturnNeedsSelection bool
}

func (c *Client) detectDockerForceUpdateMutation(ctx context.Context) (dockerMutationMeta, bool, error) {
	dockerTypeName, err := c.lookupDockerMutationTypeName(ctx)
	if err != nil {
		return dockerMutationMeta{}, false, err
	}
	if dockerTypeName == "" {
		return dockerMutationMeta{}, false, nil
	}

	fields, err := c.lookupTypeFieldsMeta(ctx, dockerTypeName)
	if err != nil {
		return dockerMutationMeta{}, false, err
	}

	if c.cfg.ForceUpdateMutation != "" {
		f, ok := fields[c.cfg.ForceUpdateMutation]
		if !ok {
			return dockerMutationMeta{}, false, fmt.Errorf("未找到配置的 unraid.force_update_mutation: %s", c.cfg.ForceUpdateMutation)
		}
		argName, argType, ok := pickIDArg(f.Args)
		if !ok {
			return dockerMutationMeta{}, false, fmt.Errorf("unraid.force_update_mutation 参数不支持 id/dockerId: %s", c.cfg.ForceUpdateMutation)
		}
		return dockerMutationMeta{
			FieldName:            c.cfg.ForceUpdateMutation,
			ArgName:              argName,
			ArgType:              argType,
			ReturnNeedsSelection: f.Type.RequiresSelectionSet(),
		}, true, nil
	}

	candidates := []string{
		"forceUpdate",
		"forceUpdateDocker",
		"force_update",
		"update",
		"updateContainer",
		"updateDocker",
		"update_container",
		"recreate",
		"pull",
	}
	for _, name := range candidates {
		f, ok := fields[name]
		if !ok {
			continue
		}

		argName, argType, ok := pickIDArg(f.Args)
		if !ok {
			continue
		}

		meta := dockerMutationMeta{
			FieldName:            name,
			ArgName:              argName,
			ArgType:              argType,
			ReturnNeedsSelection: f.Type.RequiresSelectionSet(),
		}
		return meta, true, nil
	}

	var names []string
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "update") && !strings.Contains(lower, "pull") && !strings.Contains(lower, "recreate") {
			continue
		}

		f, ok := fields[name]
		if !ok {
			continue
		}
		argName, argType, ok := pickIDArg(f.Args)
		if !ok {
			continue
		}
		meta := dockerMutationMeta{
			FieldName:            name,
			ArgName:              argName,
			ArgType:              argType,
			ReturnNeedsSelection: f.Type.RequiresSelectionSet(),
		}
		return meta, true, nil
	}

	return dockerMutationMeta{}, false, nil
}

func (c *Client) lookupDockerMutationTypeName(ctx context.Context) (string, error) {
	const q = `query { __schema { mutationType { fields { name type { name kind ofType { name kind ofType { name kind } } } } } } }`
	var resp struct {
		Schema struct {
			MutationType struct {
				Fields []struct {
					Name string `json:"name"`
					Type struct {
						Name   string `json:"name"`
						Kind   string `json:"kind"`
						OfType *struct {
							Name   string `json:"name"`
							Kind   string `json:"kind"`
							OfType *struct {
								Name string `json:"name"`
								Kind string `json:"kind"`
							} `json:"ofType"`
						} `json:"ofType"`
					} `json:"type"`
				} `json:"fields"`
			} `json:"mutationType"`
		} `json:"__schema"`
	}
	if err := c.do(ctx, q, nil, &resp); err != nil {
		return "", err
	}

	for _, f := range resp.Schema.MutationType.Fields {
		if f.Name != "docker" {
			continue
		}
		if f.Type.Name != "" {
			return f.Type.Name, nil
		}
		if f.Type.OfType != nil && f.Type.OfType.Name != "" {
			return f.Type.OfType.Name, nil
		}
	}
	return "", nil
}

type gqlTypeRef struct {
	Kind   string      `json:"kind"`
	Name   string      `json:"name"`
	OfType *gqlTypeRef `json:"ofType"`
}

func (t gqlTypeRef) String() string {
	switch t.Kind {
	case "NON_NULL":
		if t.OfType == nil {
			return "String!"
		}
		return t.OfType.String() + "!"
	case "LIST":
		if t.OfType == nil {
			return "[String]"
		}
		return "[" + t.OfType.String() + "]"
	default:
		if t.Name != "" {
			return t.Name
		}
		if t.OfType != nil {
			return t.OfType.String()
		}
		return "String"
	}
}

func (t gqlTypeRef) RequiresSelectionSet() bool {
	base := t.baseKind()
	return base == "OBJECT" || base == "INTERFACE" || base == "UNION"
}

func (t gqlTypeRef) baseKind() string {
	switch t.Kind {
	case "NON_NULL", "LIST":
		if t.OfType == nil {
			return t.Kind
		}
		return t.OfType.baseKind()
	default:
		return t.Kind
	}
}

type gqlArgMeta struct {
	Name string     `json:"name"`
	Type gqlTypeRef `json:"type"`
}

type gqlFieldMeta struct {
	Name string       `json:"name"`
	Args []gqlArgMeta `json:"args"`
	Type gqlTypeRef   `json:"type"`
}

func (c *Client) lookupTypeFieldsMeta(ctx context.Context, typeName string) (map[string]gqlFieldMeta, error) {
	const q = `query($name: String!) { __type(name: $name) { fields { name args { name type { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } type { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } }`
	var resp struct {
		Type struct {
			Fields []gqlFieldMeta `json:"fields"`
		} `json:"__type"`
	}
	if err := c.do(ctx, q, map[string]interface{}{"name": typeName}, &resp); err != nil {
		return nil, err
	}
	ret := make(map[string]gqlFieldMeta, len(resp.Type.Fields))
	for _, f := range resp.Type.Fields {
		if f.Name == "" {
			continue
		}
		ret[f.Name] = f
	}
	return ret, nil
}

func pickIDArg(args []gqlArgMeta) (string, string, bool) {
	for _, a := range args {
		if a.Name == "id" {
			return "id", a.Type.String(), true
		}
	}
	for _, a := range args {
		if a.Name == "dockerId" {
			return "dockerId", a.Type.String(), true
		}
	}
	if len(args) == 1 {
		return args[0].Name, args[0].Type.String(), true
	}
	return "", "", false
}

func (c *Client) callDockerForceUpdateMutation(ctx context.Context, meta dockerMutationMeta, id string) error {
	selection := ""
	if meta.ReturnNeedsSelection {
		selection = ` { __typename }`
	}
	q := fmt.Sprintf(`mutation ForceUpdate($v: %s) { docker { %s(%s: $v)%s } }`, meta.ArgType, meta.FieldName, meta.ArgName, selection)
	var raw map[string]interface{}
	if err := c.do(ctx, q, map[string]interface{}{"v": id}, &raw); err != nil {
		return err
	}
	return nil
}
