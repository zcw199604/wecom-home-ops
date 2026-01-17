package pve

// client.go 封装 PVE API（api2/json）调用，支持 API Token 鉴权。
import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ClientConfig struct {
	BaseURL            string
	APIToken           string
	InsecureSkipVerify bool
}

type Client struct {
	cfg        ClientConfig
	baseURL    string
	httpClient *http.Client
}

func NewClient(cfg ClientConfig, httpClient *http.Client) (*Client, error) {
	baseURL, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	cfg.BaseURL = baseURL

	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, errors.New("pve api_token 不能为空")
	}

	c := &Client{
		cfg:     cfg,
		baseURL: cfg.BaseURL,
	}

	if httpClient != nil {
		c.httpClient = httpClient
	} else {
		c.httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	if cfg.InsecureSkipVerify {
		c.httpClient = cloneHTTPClientWithInsecureSkipVerify(c.httpClient)
	}

	return c, nil
}

func (c *Client) GetVersion(ctx context.Context) (VersionInfo, error) {
	var out VersionInfo
	if err := c.do(ctx, http.MethodGet, "/version", nil, nil, &out); err != nil {
		return VersionInfo{}, err
	}
	return out, nil
}

func (c *Client) ListClusterResources(ctx context.Context, resourceType string) ([]ClusterResource, error) {
	q := url.Values{}
	if strings.TrimSpace(resourceType) != "" {
		q.Set("type", strings.TrimSpace(resourceType))
	}
	var out []ClusterResource
	if err := c.do(ctx, http.MethodGet, "/cluster/resources", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GuestAction(ctx context.Context, node string, guestType GuestType, vmid int, action GuestAction) (string, error) {
	node = strings.TrimSpace(node)
	if node == "" {
		return "", errors.New("node 不能为空")
	}
	if !guestType.IsValid() {
		return "", errors.New("guestType 不合法")
	}
	if vmid <= 0 {
		return "", errors.New("vmid 不合法")
	}
	if !action.IsValid() {
		return "", errors.New("action 不合法")
	}

	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", url.PathEscape(node), guestType.String(), vmid, action.String())
	var upid string
	if err := c.do(ctx, http.MethodPost, path, nil, nil, &upid); err != nil {
		return "", err
	}
	return upid, nil
}

func (c *Client) GetTaskStatus(ctx context.Context, node string, upid string) (TaskStatus, error) {
	node = strings.TrimSpace(node)
	upid = strings.TrimSpace(upid)
	if node == "" {
		return TaskStatus{}, errors.New("node 不能为空")
	}
	if upid == "" {
		return TaskStatus{}, errors.New("upid 不能为空")
	}

	path := fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid))
	var out TaskStatus
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return TaskStatus{}, err
	}
	return out, nil
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	form url.Values,
	out interface{},
) error {
	if ctx == nil {
		ctx = context.Background()
	}

	u := joinBaseURL(c.baseURL, path)
	if query != nil && len(query) > 0 {
		u = u + "?" + query.Encode()
	}

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}

	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	req.Header.Set("Authorization", strings.TrimSpace(c.cfg.APIToken))

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = res.Status
		}
		return fmt.Errorf("pve api %s %s: status=%d: %s", method, path, res.StatusCode, msg)
	}

	if out == nil {
		return nil
	}

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return err
	}
	if len(env.Data) == 0 {
		return errors.New("pve api: 响应 data 为空")
	}
	return json.Unmarshal(env.Data, out)
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("pve base_url 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("pve base_url 不合法: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("pve base_url 不合法（缺少 scheme/host）")
	}
	u.Fragment = ""
	u.RawQuery = ""

	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api2/json") {
		u.Path = u.Path + "/api2/json"
	}
	if u.Path == "/api2/json/api2/json" {
		u.Path = "/api2/json"
	}

	return u.String(), nil
}

func joinBaseURL(base string, p string) string {
	base = strings.TrimRight(base, "/")
	p = "/" + strings.TrimLeft(p, "/")
	return base + p
}

func cloneHTTPClientWithInsecureSkipVerify(in *http.Client) *http.Client {
	timeout := 15 * time.Second
	if in != nil && in.Timeout > 0 {
		timeout = in.Timeout
	}

	var transport *http.Transport
	if in != nil && in.Transport != nil {
		if t, ok := in.Transport.(*http.Transport); ok {
			transport = t.Clone()
		}
	}
	if transport == nil {
		if t, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = t.Clone()
		} else {
			transport = (&http.Transport{})
		}
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.InsecureSkipVerify = true

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

