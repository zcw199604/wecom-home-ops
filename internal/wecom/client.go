package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type ClientConfig struct {
	APIBaseURL string
	CorpID     string
	AgentID    int
	Secret     string
}

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client

	mu             sync.Mutex
	accessToken    string
	accessTokenExp time.Time

	tokenSF singleflight.Group
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (c *Client) SendText(ctx context.Context, msg TextMessage) error {
	payload := map[string]interface{}{
		"touser":  msg.ToUser,
		"msgtype": "text",
		"agentid": c.cfg.AgentID,
		"text": map[string]interface{}{
			"content": msg.Content,
		},
	}
	return c.sendMessage(ctx, payload)
}

func (c *Client) SendTemplateCard(ctx context.Context, msg TemplateCardMessage) error {
	if _, ok := msg.Card["task_id"]; !ok {
		msg.Card["task_id"] = "wecom-home-ops-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	payload := map[string]interface{}{
		"touser":        msg.ToUser,
		"msgtype":       "template_card",
		"agentid":       c.cfg.AgentID,
		"template_card": msg.Card,
	}
	return c.sendMessage(ctx, payload)
}

func (c *Client) sendMessage(ctx context.Context, payload map[string]interface{}) error {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	u := c.cfg.APIBaseURL + "/message/send?access_token=" + url.QueryEscape(token)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return err
	}
	if out.ErrCode != 0 {
		return fmt.Errorf("wecom api error: %d %s", out.ErrCode, out.ErrMsg)
	}
	return nil
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	if token, ok := c.peekAccessToken(); ok {
		return token, nil
	}

	v, err, _ := c.tokenSF.Do("access_token", func() (interface{}, error) {
		if token, ok := c.peekAccessToken(); ok {
			return token, nil
		}

		token, exp, err := c.fetchAccessToken(ctx)
		if err != nil {
			return "", err
		}

		c.mu.Lock()
		c.accessToken = token
		c.accessTokenExp = exp
		c.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	token, _ := v.(string)
	if token == "" {
		return "", errors.New("wecom gettoken 返回为空")
	}
	return token, nil
}

func (c *Client) peekAccessToken() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken == "" {
		return "", false
	}
	if time.Now().After(c.accessTokenExp.Add(-2 * time.Minute)) {
		return "", false
	}
	return c.accessToken, true
}

func (c *Client) fetchAccessToken(ctx context.Context) (string, time.Time, error) {
	u := c.cfg.APIBaseURL + "/gettoken?corpid=" + url.QueryEscape(c.cfg.CorpID) + "&corpsecret=" + url.QueryEscape(c.cfg.Secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", time.Time{}, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer res.Body.Close()

	var out struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", time.Time{}, err
	}
	if out.ErrCode != 0 {
		return "", time.Time{}, fmt.Errorf("wecom gettoken error: %d %s", out.ErrCode, out.ErrMsg)
	}
	if out.AccessToken == "" || out.ExpiresIn == 0 {
		return "", time.Time{}, errors.New("wecom gettoken 返回为空")
	}

	return out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn) * time.Second), nil
}
