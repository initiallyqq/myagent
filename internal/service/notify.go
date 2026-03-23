package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"myagent/internal/config"
)

// NotifyService sends WeChat miniprogram subscription messages.
type NotifyService struct {
	cfg        *config.WeChatConfig
	http       *http.Client
	tokenMu    sync.Mutex
	accessToken string
	tokenExp   time.Time
}

func NewNotifyService(cfg *config.WeChatConfig) *NotifyService {
	return &NotifyService{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// SendMatchNotification sends a subscription message to a WeChat user.
func (n *NotifyService) SendMatchNotification(ctx context.Context, openid, matchName, destination string) error {
	token, err := n.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("wechat token: %w", err)
	}

	payload := map[string]any{
		"touser":           openid,
		"template_id":      n.cfg.SubscribeTemplateID,
		"miniprogram_state": "formal",
		"lang":             "zh_CN",
		"data": map[string]any{
			"thing1": map[string]string{"value": matchName},       // 旅伴姓名
			"thing2": map[string]string{"value": destination},     // 目的地
			"thing3": map[string]string{"value": "点击查看旅伴详情"},  // 提示语
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/subscribe/send?access_token=%s", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("wechat send: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("wechat parse response: %w", err)
	}
	if result.ErrCode != 0 {
		slog.Warn("wechat notify failed", "errcode", result.ErrCode, "errmsg", result.ErrMsg, "openid", openid)
		return fmt.Errorf("wechat errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

// getAccessToken returns a cached WeChat access token, refreshing when expired.
func (n *NotifyService) getAccessToken(ctx context.Context) (string, error) {
	n.tokenMu.Lock()
	defer n.tokenMu.Unlock()

	if time.Now().Before(n.tokenExp) {
		return n.accessToken, nil
	}

	url := fmt.Sprintf(
		"https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		n.cfg.AppID, n.cfg.AppSecret,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat token errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}

	n.accessToken = result.AccessToken
	n.tokenExp = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	return n.accessToken, nil
}
