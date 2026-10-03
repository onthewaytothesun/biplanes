package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/onthewaytothesun/biplanes/server/sim"
)

// Онлайн: сервер считает физику (sim), клиенты присылают только нажатия.
// Транспорт — WebSocket, JSON. Снимок мира уходит каждые 2 тика (30 Гц) целиком,
// поэтому потерянный/опоздавший кадр просто заменяется следующим.

const (
	tickRate       = 60
	snapshotEvery  = 2
	countdown      = 3 * time.Second
	reconnectGrace = 15 * time.Second
	maxMatchTime   = 20 * 60.0 // секунд игрового времени
	endDelay       = 1500 * time.Millisecond
	quickTarget    = 10
	idleReadLimit  = 30 * time.Second
)

type hub struct {
	app     *app
	mu      sync.Mutex
	clients map[int64]*client
	queue   *client
	rooms   map[string]*room
	matches map[int64]*match
}

type room struct {
	code   string
	host   *client
	target int
	rules  sim.Rules
}

type client struct {
	id        int64
	name      string
	pvpRating int
	send      chan []byte
	ctx       context.Context
	cancel    context.CancelFunc
}

func newHub(a *app) *hub {
	return &hub{app: a, clients: map[int64]*client{}, rooms: map[string]*room{}, matches: map[int64]*match{}}
}

// push отправляет сообщение без блокировки; медленному клиенту снимки просто не доходят.
func (c *client) push(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("ws marshal", "err", err)
		return
	}
	select {
	case c.send <- b:
	default:
	}
}

type wsIn struct {
	T      string     `json:"t"`
	Token  string     `json:"token"`
	S      int        `json:"s"` // номер тика ввода клиента (для предсказания)
	K      int        `json:"k"`
	E      int        `json:"e"`
	Code   string     `json:"code"`
	Target int        `json:"target"`
	Rules  *sim.Rules `json:"rules"` // нет — правила по умолчанию
	C      float64    `json:"c"`
}

func (h *hub) serveWS(w http.ResponseWriter, r *http.Request) {
	// Общие таймауты http.Server рассчитаны на короткие запросы; для сокета снимаем их.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"biplanes.one-way.dev", "127.0.0.1:*", "localhost:*"},
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(4096)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Первое сообщение — токен сессии (в URL его не кладём: попадёт в логи nginx).
	var auth wsIn
	actx, acancel := context.WithTimeout(ctx, 10*time.Second)
	err = wsjson.Read(actx, conn, &auth)
	acancel()
	if err != nil || auth.T != "auth" {
		conn.Close(websocket.StatusPolicyViolation, "auth required")
		return
	}
	uid, err := h.app.db.sessionUser(ctx, sha256Hex(auth.Token), time.Now())
	if err != nil {
		_ = wsjson.Write(ctx, conn, map[string]string{"t": "error", "code": "auth", "msg": "Нужно войти через Telegram"})
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	u, err := h.app.db.getUser(ctx, uid)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	c := &client{id: uid, name: displayName(u.FirstName, u.LastName, u.Username), pvpRating: u.PvPRating,
		send: make(chan []byte, 64), ctx: ctx, cancel: cancel}
	go c.writeLoop(conn)
	c.push(map[string]any{"t": "hello", "name": c.name, "pvpRating": c.pvpRating})
	h.register(c)
	defer h.unregister(c)

	for {
		rctx, rcancel := context.WithTimeout(ctx, idleReadLimit)
		var m wsIn
		err := wsjson.Read(rctx, conn, &m)
		rcancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				conn.Close(websocket.StatusNormalClosure, "")
			}
			return
		}
		switch m.T {
		case "in":
			h.input(c, m.S, m.K, m.E)
		case "ping":
			c.push(map[string]any{"t": "pong", "c": m.C})
		case "quick":
			h.quick(c)
		case "create":
			rules := sim.DefaultRules()
			if m.Rules != nil {
				rules = *m.Rules
			}
			h.createRoom(c, m.Target, rules)
		case "join":
			h.joinRoom(c, m.Code)
		case "cancel":
			h.mu.Lock()
			h.cancelWaitLocked(c)
			h.mu.Unlock()
		case "leave":
			h.leave(c)
		}
	}
}

func (c *client) writeLoop(conn *websocket.Conn) {
	defer c.cancel()
	for {
		select {
		case <-c.ctx.Done():
			conn.Close(websocket.StatusGoingAway, "")
			return
		case b := <-c.send:
			wctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// ---------- lobby ----------

func (h *hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.clients[c.id]; old != nil && old != c {
		h.cancelWaitLocked(old)
		old.push(map[string]string{"t": "error", "code": "replaced", "msg": "Игра открыта в другой вкладке"})
		old.cancel()
	}
	h.clients[c.id] = c
	if m := h.matches[c.id]; m != nil {
		m.attach(c)
	}
}

func (h *hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelWaitLocked(c)
	if h.clients[c.id] == c {
		delete(h.clients, c.id)
		if m := h.matches[c.id]; m != nil {
			m.detach(c)
		}
	}
}

func (h *hub) cancelWaitLocked(c *client) {
	if h.queue != nil && h.queue.id == c.id {
		h.queue = nil
	}
	for code, r := range h.rooms {
		if r.host.id == c.id {
			delete(h.rooms, code)
		}
	}
}

func (h *hub) quick(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.matches[c.id] != nil {
		return
	}
	h.cancelWaitLocked(c)
	if q := h.queue; q != nil && q.id != c.id && q.ctx.Err() == nil {
		h.queue = nil
		// кто красный — случайно, чтобы не было преимущества «кто раньше встал в очередь»
		if coin() {
			h.startLocked(q, c, "quick", quickTarget, sim.DefaultRules())
		} else {
			h.startLocked(c, q, "quick", quickTarget, sim.DefaultRules())
		}
		return
	}
	h.queue = c
	c.push(map[string]string{"t": "waiting", "kind": "quick"})
}

func (h *hub) createRoom(c *client, target int, rules sim.Rules) {
	if target != 5 && target != 10 && target != 15 {
		target = 10
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.matches[c.id] != nil {
		return
	}
	h.cancelWaitLocked(c)
	code := roomCode()
	for h.rooms[code] != nil {
		code = roomCode()
	}
	h.rooms[code] = &room{code: code, host: c, target: target, rules: rules}
	msg := map[string]any{"t": "room", "code": code, "target": target, "rules": rules, "link": "https://t.me/" + h.app.bot + "?start=room_" + code}
	if h.app.cfg.PublicURL != "" {
		msg["web"] = h.app.cfg.PublicURL + "/?room=" + code
	}
	c.push(msg)
}

func (h *hub) joinRoom(c *client, code string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.matches[c.id] != nil {
		return
	}
	r := h.rooms[code]
	switch {
	case r == nil || r.host.ctx.Err() != nil:
		c.push(map[string]string{"t": "error", "code": "room", "msg": "Комната не найдена: друг уже закрыл её или начал другую игру"})
		return
	case r.host.id == c.id:
		c.push(map[string]string{"t": "error", "code": "room", "msg": "Это твоя комната. Отправь ссылку другу"})
		return
	}
	delete(h.rooms, code)
	h.cancelWaitLocked(c)
	h.startLocked(r.host, c, "invite", r.target, r.rules)
}

func (h *hub) input(c *client, seq, keys, eject int) {
	h.mu.Lock()
	m := h.matches[c.id]
	h.mu.Unlock()
	if m != nil {
		m.setInput(c.id, seq, keys, eject)
	}
}

func (h *hub) leave(c *client) {
	h.mu.Lock()
	m := h.matches[c.id]
	h.mu.Unlock()
	if m != nil {
		m.surrender(c.id)
	}
}

const roomAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // без 0/O и 1/I

func roomCode() string {
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(roomAlphabet))))
		b[i] = roomAlphabet[n.Int64()]
	}
	return string(b)
}

func coin() bool {
	n, _ := rand.Int(rand.Reader, big.NewInt(2))
	return n.Int64() == 1
}

// ---------- match ----------

type match struct {
	hub    *hub
	kind   string
	target int
	rules  sim.Rules
	users  [2]int64
	names  [2]string
	rating [2]int

	mu        sync.Mutex
	conns     [2]*client
	keys      [2]int
	queue     [2][]inputMsg // по одному вводу на тик, как клиент их и посчитал
	ack       [2]int        // номер последнего применённого ввода
	eject     [2]int
	lastEject [2]int
	goneAt    [2]time.Time
	surrendr  int // -1 или команда, которая сдалась
	w         *sim.World
}

func (h *hub) startLocked(red, blue *client, kind string, target int, rules sim.Rules) {
	m := &match{
		hub: h, kind: kind, target: target, rules: rules,
		users:    [2]int64{red.id, blue.id},
		names:    [2]string{red.name, blue.name},
		rating:   [2]int{red.pvpRating, blue.pvpRating},
		conns:    [2]*client{red, blue},
		surrendr: -1,
		w:        sim.New(target, rules),
	}
	h.matches[red.id] = m
	h.matches[blue.id] = m
	for team, c := range m.conns {
		c.push(m.startMsg(team, countdown.Seconds()))
	}
	slog.Info("match start", "kind", kind, "red", red.id, "blue", blue.id, "target", target, "shield", rules.Shield, "ram", rules.Ram)
	go m.run()
}

func (m *match) team(uid int64) int {
	if m.users[0] == uid {
		return 0
	}
	if m.users[1] == uid {
		return 1
	}
	return -1
}

func (m *match) startMsg(team int, cd float64) map[string]any {
	return map[string]any{
		"t": "start", "you": team, "target": m.target, "kind": m.kind, "cd": cd, "rules": m.rules,
		"names": m.names, "ratings": m.rating,
	}
}

// attach/detach вызываются под hub.mu.
func (m *match) attach(c *client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.team(c.id)
	m.conns[t] = c
	m.goneAt[t] = time.Time{}
	m.keys[t] = 0
	m.queue[t] = nil
	c.push(m.startMsg(t, 0))
	if o := m.conns[1-t]; o != nil {
		o.push(map[string]any{"t": "opp", "online": true})
	}
}

func (m *match) detach(c *client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.team(c.id)
	if m.conns[t] != c {
		return
	}
	m.conns[t] = nil
	m.goneAt[t] = time.Now()
	m.keys[t] = 0
	m.queue[t] = nil
	if o := m.conns[1-t]; o != nil {
		o.push(map[string]any{"t": "opp", "online": false, "grace": reconnectGrace.Seconds()})
	}
}

type inputMsg struct{ seq, keys int }

const (
	maxQueue  = 30 // больше — клиент явно завис или шлёт мусор
	catchUpAt = 6  // очередь длиннее — отбрасываем старое, чтобы не копить задержку
)

func (m *match) setInput(uid int64, seq, keys, eject int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.team(uid); t >= 0 {
		if seq > 0 {
			if n := len(m.queue[t]); n == 0 || seq > m.queue[t][n-1].seq {
				m.queue[t] = append(m.queue[t], inputMsg{seq, keys})
			}
			if len(m.queue[t]) > maxQueue {
				m.queue[t] = m.queue[t][len(m.queue[t])-maxQueue:]
			}
		} else {
			m.keys[t] = keys // клиент без номеров — просто текущее состояние кнопок
		}
		if eject > m.eject[t] {
			m.eject[t] = eject
		}
	}
}

func (m *match) surrender(uid int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.team(uid); t >= 0 && m.surrendr < 0 {
		m.surrendr = t
	}
}

func (m *match) broadcast(v any) {
	for _, c := range m.conns {
		if c != nil {
			c.push(v)
		}
	}
}

func (m *match) run() {
	start := time.Now()
	for time.Since(start) < countdown {
		time.Sleep(50 * time.Millisecond)
		m.mu.Lock()
		gone := m.surrendr >= 0
		m.mu.Unlock()
		if gone {
			break
		}
	}

	dt := 1.0 / tickRate
	ticker := time.NewTicker(time.Second / tickRate)
	defer ticker.Stop()
	var pending []sim.Event
	var endAt time.Time
	m.mu.Lock()
	m.queue = [2][]inputMsg{} // всё, что пришло во время отсчёта, не считается
	m.mu.Unlock()
	for tick := 0; ; tick++ {
		<-ticker.C
		m.mu.Lock()
		var in [2]sim.Input
		for t := range 2 {
			if q := m.queue[t]; len(q) > 0 {
				if len(q) > catchUpAt {
					q = q[len(q)-3:]
				}
				m.keys[t], m.ack[t] = q[0].keys, q[0].seq
				m.queue[t] = q[1:]
			}
			k := m.keys[t]
			in[t] = sim.Input{Left: k&1 != 0, Right: k&2 != 0, Up: k&4 != 0, Down: k&8 != 0, Fire: k&16 != 0}
			if m.eject[t] > m.lastEject[t] {
				in[t].Eject = true
				m.lastEject[t] = m.eject[t]
			}
		}
		if m.surrendr < 0 {
			m.w.Step(dt, in)
		}
		pending = append(pending, m.w.TakeEvents()...)

		winner, reason := -1, ""
		switch {
		case m.surrendr >= 0:
			winner, reason = 1-m.surrendr, "forfeit"
		case m.w.Winner >= 0:
			if endAt.IsZero() {
				endAt = time.Now().Add(endDelay)
			} else if time.Now().After(endAt) {
				winner, reason = m.w.Winner, "score"
			}
		case m.w.Time > maxMatchTime:
			reason = "timeout"
			if s := m.w.Players; s[0].Score != s[1].Score {
				winner = 0
				if s[1].Score > s[0].Score {
					winner = 1
				}
			}
		}
		for t := range 2 {
			if reason == "" && m.conns[t] == nil && !m.goneAt[t].IsZero() && time.Since(m.goneAt[t]) > reconnectGrace {
				winner, reason = 1-t, "forfeit"
			}
		}
		if tick%snapshotEvery == 0 || reason != "" {
			m.broadcast(m.snapshot(pending))
			pending = nil
		}
		m.mu.Unlock()
		if reason != "" {
			m.finish(winner, reason)
			return
		}
	}
}

func r1(v float64) float64 { return math.Round(v*10) / 10 }

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// snapshot — компактный снимок мира. Строки — массивы чисел, чтобы кадр весил ~0.5 КБ.
func (m *match) snapshot(ev []sim.Event) map[string]any {
	w := m.w
	pl := make([][]float64, 0, len(w.Planes))
	for _, p := range w.Planes {
		pilot, rep, shield := 0.0, 0.0, b2f(p.Shield)
		if p.Pilot {
			pilot = 1
		}
		if p.Repair > 0 {
			rep = 1
		}
		pl = append(pl, []float64{float64(p.ID), float64(p.Team), r1(p.X), r1(p.Y), math.Round(p.A*1000) / 1000,
			math.Round(p.Thr*100) / 100, float64(p.State), float64(p.HP), pilot, rep, r1(p.Speed), shield})
	}
	pt := make([][]float64, 0, len(w.Pilots))
	for _, p := range w.Pilots {
		pt = append(pt, []float64{float64(p.ID), float64(p.Team), r1(p.X), r1(p.Y), float64(p.State), float64(p.Dir), math.Round(p.Step*100) / 100})
	}
	b := make([][]float64, 0, len(w.Bullets))
	for _, x := range w.Bullets {
		b = append(b, []float64{float64(x.ID), r1(x.X), r1(x.Y), float64(x.Team)})
	}
	// Точное состояние «своего» самолёта или пилота каждого игрока — от него клиент
	// переигрывает ещё не подтверждённые нажатия (предсказание на клиенте).
	var own [2]any
	for t, p := range w.Players {
		switch {
		case p.Plane != nil:
			q := p.Plane
			own[t] = map[string][]float64{"pl": {float64(q.ID), q.X, q.Y, q.A, q.Speed, q.Thr, q.VX, q.VY, float64(q.State), float64(q.HP), q.CD, q.Repair, b2f(q.Shield)}}
		case p.Pilot != nil:
			q := p.Pilot
			own[t] = map[string][]float64{"pt": {float64(q.ID), q.X, q.Y, q.VX, q.VY, float64(q.State), q.T, q.Step, float64(q.Dir)}}
		}
	}
	for i := range ev {
		ev[i].X, ev[i].Y = r1(ev[i].X), r1(ev[i].Y)
	}
	return map[string]any{
		"t": "s", "tm": math.Round(w.Time*1000) / 1000,
		"sc": [2]int{w.Players[0].Score, w.Players[1].Score},
		"on": [2]bool{m.conns[0] != nil, m.conns[1] != nil},
		"pl": pl, "pt": pt, "b": b, "ev": ev, "ack": m.ack, "own": own,
	}
}

func (m *match) finish(winner int, reason string) {
	m.mu.Lock()
	res := pvpResult{
		Kind: m.kind, Target: m.target, Users: m.users, Winner: winner, Reason: reason,
		Scores:     [2]int{m.w.Players[0].Score, m.w.Players[1].Score},
		DurationMs: int64(m.w.Time * 1000),
		Stats:      [2]sim.Stats{m.w.Players[0].Stats, m.w.Players[1].Stats},
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before, after, err := m.hub.app.db.savePvP(ctx, res, time.Now())
	if err != nil {
		slog.Error("save pvp", "err", err)
		before, after = m.rating, m.rating
	}
	slog.Info("match end", "kind", m.kind, "winner", winner, "reason", reason, "score", res.Scores)

	m.hub.mu.Lock()
	for t, uid := range m.users {
		if m.hub.matches[uid] == m {
			delete(m.hub.matches, uid)
		}
		if c := m.hub.clients[uid]; c != nil {
			c.pvpRating = after[t]
			c.push(map[string]any{
				"t": "end", "you": t, "winner": winner, "reason": reason, "scores": res.Scores,
				"names": m.names, "saved": err == nil,
				"rating": map[string]int{"before": before[t], "after": after[t], "delta": after[t] - before[t]},
			})
		}
	}
	m.hub.mu.Unlock()
}
