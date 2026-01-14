// Package qinglong 封装青龙(QL) OpenAPI 的认证与任务操作能力，供企业微信交互调用。
package qinglong

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type ClientConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time

	tokenSF singleflight.Group
}

func NewClient(cfg ClientConfig, httpClient *http.Client) (*Client, error) {
	baseURL, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	cfg.BaseURL = baseURL

	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("qinglong client_id 不能为空")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("qinglong client_secret 不能为空")
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}, nil
}

type TokenInfo struct {
	Token      string
	Expiration time.Time
}

func (c *Client) GetTokenInfo(ctx context.Context) (TokenInfo, error) {
	token, exp, err := c.getToken(ctx)
	if err != nil {
		return TokenInfo{}, err
	}
	return TokenInfo{Token: token, Expiration: exp}, nil
}

type Cron struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Command    string `json:"command"`
	Schedule   string `json:"schedule"`
	IsDisabled int    `json:"isDisabled"`
	Status     int    `json:"status"`
}

type CronPage struct {
	Data  []Cron `json:"data"`
	Total int    `json:"total"`
}

type ListCronsParams struct {
	SearchValue string
	Page        int
	Size        int
}

func (c *Client) ListCrons(ctx context.Context, params ListCronsParams) (CronPage, error) {
	q := url.Values{}
	if strings.TrimSpace(params.SearchValue) != "" {
		q.Set("searchValue", params.SearchValue)
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.Size > 0 {
		q.Set("size", strconv.Itoa(params.Size))
	}

	var out CronPage
	if err := c.do(ctx, http.MethodGet, "/open/crons", q, nil, &out, true); err != nil {
		return CronPage{}, err
	}
	return out, nil
}

func (c *Client) GetCron(ctx context.Context, id int) (Cron, error) {
	if id <= 0 {
		return Cron{}, errors.New("cron id 不合法")
	}

	var out Cron
	if err := c.do(ctx, http.MethodGet, "/open/crons/"+strconv.Itoa(id), nil, nil, &out, true); err != nil {
		return Cron{}, err
	}
	return out, nil
}

func (c *Client) RunCrons(ctx context.Context, ids []int) error {
	return c.do(ctx, http.MethodPut, "/open/crons/run", nil, ids, nil, true)
}

func (c *Client) EnableCrons(ctx context.Context, ids []int) error {
	return c.do(ctx, http.MethodPut, "/open/crons/enable", nil, ids, nil, true)
}

func (c *Client) DisableCrons(ctx context.Context, ids []int) error {
	return c.do(ctx, http.MethodPut, "/open/crons/disable", nil, ids, nil, true)
}

func (c *Client) GetCronLog(ctx context.Context, id int) (string, error) {
	if id <= 0 {
		return "", errors.New("cron id 不合法")
	}

	var out string
	if err := c.do(ctx, http.MethodGet, "/open/crons/"+strconv.Itoa(id)+"/log", nil, nil, &out, true); err != nil {
		return "", err
	}
	return out, nil
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Errors  []struct {
		Message string `json:"message"`
		Value   string `json:"value"`
	} `json:"errors"`
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body interface{},
	out interface{},
	withAuth bool,
) error {
	u := joinBaseURL(c.cfg.BaseURL, path)
	if query != nil && len(query) > 0 {
		u = u + "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if withAuth {
		token, _, err := c.getToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("qinglong http status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}

	var env apiEnvelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return err
	}
	if env.Code != 200 {
		msg := strings.TrimSpace(env.Message)
		if msg == "" && len(env.Errors) > 0 {
			var parts []string
			for _, e := range env.Errors {
				if strings.TrimSpace(e.Message) == "" {
					continue
				}
				if strings.TrimSpace(e.Value) == "" {
					parts = append(parts, e.Message)
					continue
				}
				parts = append(parts, fmt.Sprintf("%s(%s)", e.Message, e.Value))
			}
			msg = strings.Join(parts, "; ")
		}
		if msg == "" {
			msg = string(env.Data)
		}
		return fmt.Errorf("qinglong api error: %d %s", env.Code, msg)
	}

	if out == nil {
		return nil
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

type tokenResponse struct {
	Token      string `json:"token"`
	TokenType  string `json:"token_type"`
	Expiration int64  `json:"expiration"`
}

func (c *Client) getToken(ctx context.Context) (string, time.Time, error) {
	if token, exp, ok := c.peekToken(); ok {
		return token, exp, nil
	}

	type tokenResult struct {
		Token string
		Exp   time.Time
	}

	v, err, _ := c.tokenSF.Do("token", func() (interface{}, error) {
		if token, exp, ok := c.peekToken(); ok {
			return tokenResult{Token: token, Exp: exp}, nil
		}

		q := url.Values{}
		q.Set("client_id", c.cfg.ClientID)
		q.Set("client_secret", c.cfg.ClientSecret)

		var out tokenResponse
		if err := c.do(ctx, http.MethodGet, "/open/auth/token", q, nil, &out, false); err != nil {
			return tokenResult{}, err
		}
		if strings.TrimSpace(out.Token) == "" || out.Expiration <= 0 {
			return tokenResult{}, errors.New("qinglong auth/token 返回为空")
		}

		exp := time.Unix(out.Expiration, 0)

		c.mu.Lock()
		c.token = out.Token
		c.tokenExp = exp
		c.mu.Unlock()

		return tokenResult{Token: out.Token, Exp: exp}, nil
	})
	if err != nil {
		return "", time.Time{}, err
	}
	tr, _ := v.(tokenResult)
	if strings.TrimSpace(tr.Token) == "" || tr.Exp.IsZero() {
		return "", time.Time{}, errors.New("qinglong auth/token 返回为空")
	}
	return tr.Token, tr.Exp, nil
}

func (c *Client) peekToken() (string, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return "", time.Time{}, false
	}
	if time.Now().After(c.tokenExp.Add(-2 * time.Minute)) {
		return "", time.Time{}, false
	}
	return c.token, c.tokenExp, true
}

func normalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("qinglong base_url 不能为空")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("qinglong base_url 不合法: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("qinglong base_url scheme 仅支持 http/https")
	}
	if u.Host == "" {
		return "", errors.New("qinglong base_url host 不能为空")
	}
	return strings.TrimRight(s, "/"), nil
}

func joinBaseURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
