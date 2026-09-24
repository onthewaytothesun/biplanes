package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type telegram struct {
	token string
	hc    *http.Client
}

func newTelegram(token string) *telegram {
	return &telegram{token: token, hc: &http.Client{Timeout: 15 * time.Second}}
}

// call вызывает Bot API. Ошибки не содержат URL — в нём токен бота.
func (t *telegram) call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %v", method, err)
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: decode: %v", method, err)
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %s", method, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (t *telegram) getMe(ctx context.Context) (tgUser, error) {
	var u tgUser
	err := t.call(ctx, "getMe", struct{}{}, &u)
	return u, err
}

func (t *telegram) setWebhook(ctx context.Context, hookURL, secret string) error {
	return t.call(ctx, "setWebhook", map[string]any{
		"url":             hookURL,
		"secret_token":    secret,
		"allowed_updates": []string{"message", "callback_query"},
		"max_connections": 10,
	}, nil)
}

func (t *telegram) setMenuButton(ctx context.Context, text, appURL string) error {
	return t.call(ctx, "setChatMenuButton", map[string]any{
		"menu_button": map[string]any{"type": "web_app", "text": text, "web_app": map[string]string{"url": appURL}},
	}, nil)
}

type inlineButton struct {
	Text         string            `json:"text"`
	CallbackData string            `json:"callback_data,omitempty"`
	WebApp       map[string]string `json:"web_app,omitempty"`
	URL          string            `json:"url,omitempty"`
}

func keyboard(rows ...[]inlineButton) map[string]any {
	return map[string]any{"inline_keyboard": rows}
}

func (t *telegram) sendMessage(ctx context.Context, chatID int64, text string, markup any) error {
	p := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML"}
	if markup != nil {
		p["reply_markup"] = markup
	}
	return t.call(ctx, "sendMessage", p, nil)
}

func (t *telegram) editMessage(ctx context.Context, chatID, messageID int64, text string, markup any) error {
	p := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text, "parse_mode": "HTML"}
	if markup != nil {
		p["reply_markup"] = markup
	}
	return t.call(ctx, "editMessageText", p, nil)
}

func (t *telegram) answerCallback(ctx context.Context, id, text string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// ---------- updates ----------

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	From      tgUser `json:"from"`
	Chat      tgChat `json:"chat"`
	Text      string `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
	Callback *struct {
		ID      string     `json:"id"`
		From    tgUser     `json:"from"`
		Data    string     `json:"data"`
		Message *tgMessage `json:"message"`
	} `json:"callback_query"`
}
