package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// CreateMenu 创建/覆盖企业微信自建应用的自定义菜单。
//
// 官方文档（SSOT）：创建菜单
// https://developer.work.weixin.qq.com/document/path/90231
func (c *Client) CreateMenu(ctx context.Context, menu Menu) error {
	start := time.Now()

	token, err := c.getAccessToken(ctx)
	if err != nil {
		slog.Error("wecom menu/create 获取 access_token 失败", "error", err)
		return err
	}

	u := c.cfg.APIBaseURL +
		"/menu/create?access_token=" + url.QueryEscape(token) +
		"&agentid=" + strconv.Itoa(c.cfg.AgentID)
	body, err := json.Marshal(menu)
	if err != nil {
		slog.Error("wecom menu/create 编码 payload 失败", "error", err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		slog.Error("wecom menu/create 创建请求失败", "error", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("wecom menu/create HTTP 请求失败",
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}
	defer res.Body.Close()

	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		slog.Error("wecom menu/create 解析响应失败",
			"error", err,
			"status_code", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}

	attrs := []any{
		"status_code", res.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
		"errcode", out.ErrCode,
		"errmsg", out.ErrMsg,
		"agentid", c.cfg.AgentID,
		"top_buttons", len(menu.Buttons),
	}

	if out.ErrCode != 0 {
		apiErr := fmt.Errorf("wecom api error: %d %s", out.ErrCode, out.ErrMsg)
		slog.Error("wecom menu/create 返回错误", append(attrs, "error", apiErr)...)
		return apiErr
	}

	slog.Info("wecom menu/create 成功", attrs...)
	return nil
}

// UpdateTemplateCardButton 将模板卡片的按钮更新为不可点击状态（替换按钮文案）。
//
// 官方文档（SSOT）：更新模版卡片消息
// https://developer.work.weixin.qq.com/document/90000/90135/94888
func (c *Client) UpdateTemplateCardButton(ctx context.Context, responseCode string, replaceName string) error {
	if responseCode == "" {
		return errors.New("wecom update_template_card: response_code 为空")
	}
	if replaceName == "" {
		replaceName = "已处理"
	}

	payload := map[string]interface{}{
		"agentid":       c.cfg.AgentID,
		"response_code": responseCode,
		"button": map[string]interface{}{
			"replace_name": replaceName,
		},
	}
	return c.updateTemplateCard(ctx, payload)
}

func (c *Client) sendMessage(ctx context.Context, payload map[string]interface{}) error {
	start := time.Now()
	toUser, _ := payload["touser"].(string)
	msgType, _ := payload["msgtype"].(string)
	contentLen := 0
	if textBody, ok := payload["text"].(map[string]interface{}); ok {
		if content, ok := textBody["content"].(string); ok {
			contentLen = len(content)
		}
	}
	cardType := ""
	taskID := ""
	if cardBody, ok := payload["template_card"].(map[string]interface{}); ok {
		if v, ok := cardBody["card_type"].(string); ok {
			cardType = v
		}
		if v, ok := cardBody["task_id"].(string); ok {
			taskID = v
		}
	}

	token, err := c.getAccessToken(ctx)
	if err != nil {
		slog.Error("wecom message/send 获取 access_token 失败",
			"error", err,
			"to_user", toUser,
			"msgtype", msgType,
		)
		return err
	}

	u := c.cfg.APIBaseURL + "/message/send?access_token=" + url.QueryEscape(token)
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("wecom message/send 编码 payload 失败",
			"error", err,
			"to_user", toUser,
			"msgtype", msgType,
		)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		slog.Error("wecom message/send 创建请求失败",
			"error", err,
			"to_user", toUser,
			"msgtype", msgType,
		)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("wecom message/send HTTP 请求失败",
			"error", err,
			"to_user", toUser,
			"msgtype", msgType,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}
	defer res.Body.Close()

	var out struct {
		ErrCode        int    `json:"errcode"`
		ErrMsg         string `json:"errmsg"`
		MsgID          string `json:"msgid"`
		InvalidUser    string `json:"invaliduser"`
		InvalidParty   string `json:"invalidparty"`
		InvalidTag     string `json:"invalidtag"`
		UnlicensedUser string `json:"unlicenseduser"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		slog.Error("wecom message/send 解析响应失败",
			"error", err,
			"to_user", toUser,
			"msgtype", msgType,
			"status_code", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}

	attrs := []any{
		"to_user", toUser,
		"msgtype", msgType,
		"content_len", contentLen,
		"card_type", cardType,
		"task_id", taskID,
		"status_code", res.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
		"errcode", out.ErrCode,
		"errmsg", out.ErrMsg,
		"msgid", out.MsgID,
		"invaliduser", out.InvalidUser,
		"invalidparty", out.InvalidParty,
		"invalidtag", out.InvalidTag,
		"unlicenseduser", out.UnlicensedUser,
	}

	if out.ErrCode != 0 {
		apiErr := fmt.Errorf("wecom api error: %d %s", out.ErrCode, out.ErrMsg)
		slog.Error("wecom message/send 返回错误", append(attrs, "error", apiErr)...)
		return apiErr
	}

	if out.InvalidUser != "" || out.InvalidParty != "" || out.InvalidTag != "" || out.UnlicensedUser != "" {
		apiErr := fmt.Errorf("wecom message/send 部分失败: invaliduser=%s invalidparty=%s invalidtag=%s unlicenseduser=%s",
			out.InvalidUser, out.InvalidParty, out.InvalidTag, out.UnlicensedUser,
		)
		slog.Warn("wecom message/send 部分失败", append(attrs, "error", apiErr)...)
		return apiErr
	}

	slog.Info("wecom message/send 成功", attrs...)
	return nil
}

func (c *Client) updateTemplateCard(ctx context.Context, payload map[string]interface{}) error {
	start := time.Now()
	responseCodeLen := 0
	if v, ok := payload["response_code"].(string); ok {
		responseCodeLen = len(v)
	}

	token, err := c.getAccessToken(ctx)
	if err != nil {
		slog.Error("wecom message/update_template_card 获取 access_token 失败",
			"error", err,
			"response_code_len", responseCodeLen,
		)
		return err
	}

	u := c.cfg.APIBaseURL + "/message/update_template_card?access_token=" + url.QueryEscape(token)
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("wecom message/update_template_card 编码 payload 失败",
			"error", err,
			"response_code_len", responseCodeLen,
		)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		slog.Error("wecom message/update_template_card 创建请求失败",
			"error", err,
			"response_code_len", responseCodeLen,
		)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("wecom message/update_template_card HTTP 请求失败",
			"error", err,
			"response_code_len", responseCodeLen,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}
	defer res.Body.Close()

	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		slog.Error("wecom message/update_template_card 解析响应失败",
			"error", err,
			"status_code", res.StatusCode,
			"response_code_len", responseCodeLen,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return err
	}

	attrs := []any{
		"status_code", res.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
		"errcode", out.ErrCode,
		"errmsg", out.ErrMsg,
		"response_code_len", responseCodeLen,
	}

	if out.ErrCode != 0 {
		apiErr := fmt.Errorf("wecom api error: %d %s", out.ErrCode, out.ErrMsg)
		slog.Error("wecom message/update_template_card 返回错误", append(attrs, "error", apiErr)...)
		return apiErr
	}
	slog.Info("wecom message/update_template_card 成功", attrs...)
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
	start := time.Now()
	u := c.cfg.APIBaseURL + "/gettoken?corpid=" + url.QueryEscape(c.cfg.CorpID) + "&corpsecret=" + url.QueryEscape(c.cfg.Secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		slog.Error("wecom gettoken 创建请求失败", "error", err)
		return "", time.Time{}, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("wecom gettoken HTTP 请求失败",
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
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
		slog.Error("wecom gettoken 解析响应失败",
			"error", err,
			"status_code", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return "", time.Time{}, err
	}
	if out.ErrCode != 0 {
		apiErr := fmt.Errorf("wecom gettoken error: %d %s", out.ErrCode, out.ErrMsg)
		slog.Error("wecom gettoken 返回错误",
			"error", apiErr,
			"status_code", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"errcode", out.ErrCode,
			"errmsg", out.ErrMsg,
		)
		return "", time.Time{}, apiErr
	}
	if out.AccessToken == "" || out.ExpiresIn == 0 {
		apiErr := errors.New("wecom gettoken 返回为空")
		slog.Error("wecom gettoken 返回为空",
			"error", apiErr,
			"status_code", res.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return "", time.Time{}, apiErr
	}

	slog.Info("wecom gettoken 成功",
		"status_code", res.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
		"expires_in", out.ExpiresIn,
	)
	return out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn) * time.Second), nil
}
