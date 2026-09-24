package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/webapp", a.handleAuthWebApp)
	mux.HandleFunc("POST /api/auth/login", a.handleLoginStart)
	mux.HandleFunc("POST /api/auth/login/poll", a.handleLoginPoll)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)
	mux.HandleFunc("POST /api/matches", a.handleMatch)
	mux.HandleFunc("GET /api/leaderboard", a.handleLeaderboard)
	mux.HandleFunc("GET /api/ws", a.hub.serveWS)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /tg/webhook", a.handleWebhook)
	if a.cfg.StaticDir != "" {
		mux.Handle("GET /", http.FileServer(http.Dir(a.cfg.StaticDir)))
	}
	return mux
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

// currentUser возвращает tg_id по сессии; 0 — не авторизован.
func (a *app) currentUser(r *http.Request) (int64, error) {
	tok := bearer(r)
	if tok == "" {
		return 0, nil
	}
	id, err := a.db.sessionUser(r.Context(), sha256Hex(tok), time.Now())
	if errors.Is(err, errNotFound) {
		return 0, nil
	}
	return id, err
}

func (a *app) requireUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := a.currentUser(r)
	if err != nil {
		slog.Error("session lookup", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return 0, false
	}
	if id == 0 {
		writeErr(w, http.StatusUnauthorized, "login required")
		return 0, false
	}
	return id, true
}

func (a *app) issueSession(w http.ResponseWriter, r *http.Request, tgID int64) {
	tok := randomToken(32)
	if err := a.db.createSession(r.Context(), sha256Hex(tok), tgID, time.Now(), sessionTTL); err != nil {
		slog.Error("create session", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := a.db.getUser(r.Context(), tgID)
	if err != nil {
		slog.Error("get user", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "user": u})
}

// ---------- auth ----------

func (a *app) handleAuthWebApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"initData"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	now := time.Now()
	tu, err := validateInitData(body.InitData, a.cfg.BotToken, initDataMaxAge, now)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := a.db.upsertUser(r.Context(), tu, now); err != nil {
		slog.Error("upsert user", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("login", "via", "webapp", "tg_id", tu.ID)
	a.issueSession(w, r, tu.ID)
}

func (a *app) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	nonce, secret := randomToken(16), randomToken(24)
	if err := a.db.createLogin(r.Context(), nonce, sha256Hex(secret), time.Now(), loginTTL); err != nil {
		slog.Error("create login", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce":     nonce,
		"secret":    secret,
		"url":       "https://t.me/" + a.bot + "?start=" + loginStartParam + nonce,
		"bot":       a.bot,
		"expiresIn": int(loginTTL.Seconds()),
	})
}

func (a *app) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nonce  string `json:"nonce"`
		Secret string `json:"secret"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	state, tgID, err := a.db.consumeLogin(r.Context(), body.Nonce, sha256Hex(body.Secret), time.Now())
	if err != nil {
		slog.Error("consume login", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch state {
	case loginPending:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
	case loginGone:
		writeErr(w, http.StatusGone, "login request expired")
	case loginConfirmed:
		slog.Info("login", "via", "bot", "tg_id", tgID)
		a.issueSession(w, r, tgID)
	}
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok := bearer(r); tok != "" {
		if err := a.db.deleteSession(r.Context(), sha256Hex(tok)); err != nil {
			slog.Error("delete session", "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- profile, matches, leaderboard ----------

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := a.db.getUser(ctx, id)
	if err != nil {
		slog.Error("get user", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	st, err1 := a.db.userStats(ctx, id)
	rank, err2 := a.db.rank(ctx, id)
	recent, err3 := a.db.recentMatches(ctx, id, 10)
	pvp, err4 := a.db.pvpStats(ctx, id)
	recentPvP, err5 := a.db.recentPvP(ctx, id, 10)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		slog.Error("me", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "stats": st, "rank": rank, "recent": recent, "pvp": pvp, "recentPvp": recentPvP})
}

func (a *app) handleMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var m matchInput
	if !readJSON(w, r, &m) {
		return
	}
	if err := validateMatch(m); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	res, err := a.db.saveMatch(r.Context(), id, m, time.Now())
	if errors.Is(err, errTooSoon) {
		writeErr(w, http.StatusTooManyRequests, "too many matches, slow down")
		return
	}
	if err != nil {
		slog.Error("save match", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("match", "tg_id", id, "mode", m.Mode, "score", [2]int{m.MyScore, m.OppScore}, "rating", res.RatingAfter)
	writeJSON(w, http.StatusOK, res)
}

func (a *app) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	me, _ := a.currentUser(r)
	load := a.db.leaderboard
	if r.URL.Query().Get("kind") == "pvp" {
		load = a.db.pvpLeaderboard
	}
	rows, err := load(r.Context(), me, 50)
	if err != nil {
		slog.Error("leaderboard", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "botRating": botRating})
}

// ---------- Telegram webhook ----------

func (a *app) handleWebhook(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if a.cfg.WebhookSecret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.cfg.WebhookSecret)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var upd tgUpdate
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		w.WriteHeader(http.StatusOK) // битый апдейт повторять бессмысленно
		return
	}
	// Отвечаем Telegram сразу; сама обработка — короткие запросы к БД и Bot API.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	switch {
	case upd.Message != nil:
		a.onMessage(ctx, upd.Message)
	case upd.Callback != nil:
		a.onCallback(ctx, upd.Callback.ID, upd.Callback.From, upd.Callback.Data, upd.Callback.Message)
	}
	w.WriteHeader(http.StatusOK)
}

func (a *app) playKeyboard() any {
	if a.cfg.PublicURL == "" {
		return nil
	}
	return keyboard([]inlineButton{{Text: "✈️ Играть", WebApp: map[string]string{"url": a.cfg.PublicURL}}})
}

func (a *app) onMessage(ctx context.Context, m *tgMessage) {
	if m.Chat.Type != "private" || !strings.HasPrefix(m.Text, "/start") {
		return
	}
	param := strings.TrimSpace(strings.TrimPrefix(m.Text, "/start"))
	if code, ok := strings.CutPrefix(param, "room_"); ok && a.cfg.PublicURL != "" {
		a.send(ctx, m.Chat.ID, "Тебя зовут на дуэль в <b>Бипланах</b>! Жми кнопку, бой начнётся сразу.",
			keyboard([]inlineButton{{Text: "✈️ Принять вызов", WebApp: map[string]string{"url": a.cfg.PublicURL + "/?room=" + url.QueryEscape(code)}}}))
		return
	}
	if nonce, ok := strings.CutPrefix(param, loginStartParam); ok {
		open, err := a.db.loginOpen(ctx, nonce, time.Now())
		if err != nil {
			slog.Error("login open", "err", err)
			return
		}
		if !open {
			a.send(ctx, m.Chat.ID, "Ссылка для входа устарела. Нажми «Войти через Telegram» на сайте ещё раз.", nil)
			return
		}
		a.send(ctx, m.Chat.ID,
			"Войти в <b>Бипланы</b> в браузере?\n\nЕсли ты не нажимал «Войти» на сайте только что, просто проигнорируй это сообщение.",
			keyboard([]inlineButton{{Text: "✅ Войти", CallbackData: "login:" + nonce}}))
		return
	}
	a.send(ctx, m.Chat.ID,
		"Привет, "+html.EscapeString(displayName(m.From.FirstName, m.From.LastName, m.From.Username))+
			"! Это <b>Бипланы</b>: воздушная дуэль с ботом или другом. Результаты матчей идут в общий рейтинг.",
		a.playKeyboard())
}

func (a *app) onCallback(ctx context.Context, id string, from tgUser, data string, msg *tgMessage) {
	nonce, ok := strings.CutPrefix(data, "login:")
	if !ok || from.ID == 0 {
		_ = a.tg.answerCallback(ctx, id, "")
		return
	}
	now := time.Now()
	if err := a.db.upsertUser(ctx, from, now); err != nil {
		slog.Error("upsert user", "err", err)
		_ = a.tg.answerCallback(ctx, id, "Ошибка, попробуй ещё раз")
		return
	}
	confirmed, err := a.db.confirmLogin(ctx, nonce, from.ID, now)
	if err != nil {
		slog.Error("confirm login", "err", err)
		_ = a.tg.answerCallback(ctx, id, "Ошибка, попробуй ещё раз")
		return
	}
	text := "Ссылка для входа устарела или уже использована. Нажми «Войти через Telegram» на сайте ещё раз."
	if confirmed {
		text = "Готово, ты вошёл как <b>" + html.EscapeString(displayName(from.FirstName, from.LastName, from.Username)) +
			"</b>. Возвращайся в браузер."
	}
	_ = a.tg.answerCallback(ctx, id, "")
	if msg != nil {
		if err := a.tg.editMessage(ctx, msg.Chat.ID, msg.MessageID, text, a.playKeyboard()); err != nil {
			slog.Warn("edit message", "err", err)
		}
	}
}

func (a *app) send(ctx context.Context, chatID int64, text string, markup any) {
	if err := a.tg.sendMessage(ctx, chatID, text, markup); err != nil {
		slog.Warn("send message", "err", err)
	}
}
