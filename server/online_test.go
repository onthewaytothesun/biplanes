package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsMsg map[string]any

func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, want string, ok func(wsMsg) bool) wsMsg {
	t.Helper()
	for {
		var m wsMsg
		if err := wsjson.Read(ctx, c, &m); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if m["t"] == want && (ok == nil || ok(m)) {
			return m
		}
	}
}

func TestOnlineQuickMatchAndForfeit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a := &app{db: s, bot: "TestBot"}
	a.hub = newHub(a)
	srv := httptest.NewServer(a.routes())
	defer srv.Close()

	now := time.Now()
	for _, id := range []int64{1, 2} {
		if err := s.upsertUser(ctx, tgUser{ID: id, FirstName: map[int64]string{1: "Анна", 2: "Борис"}[id]}, now); err != nil {
			t.Fatal(err)
		}
		if err := s.createSession(ctx, sha256Hex(map[int64]string{1: "tok1", 2: "tok2"}[id]), id, now, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	dial := func(tok string) *websocket.Conn {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Write(ctx, c, wsMsg{"t": "auth", "token": tok}); err != nil {
			t.Fatal(err)
		}
		readUntil(t, ctx, c, "hello", nil)
		return c
	}

	// неверный токен — отказ
	bad, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = wsjson.Write(ctx, bad, wsMsg{"t": "auth", "token": "nope"})
	if m := readUntil(t, ctx, bad, "error", nil); m["code"] != "auth" {
		t.Fatalf("bad token: %v", m)
	}

	c1, c2 := dial("tok1"), dial("tok2")
	defer c1.CloseNow()
	defer c2.CloseNow()

	_ = wsjson.Write(ctx, c1, wsMsg{"t": "quick"})
	readUntil(t, ctx, c1, "waiting", nil)
	_ = wsjson.Write(ctx, c2, wsMsg{"t": "quick"})
	s1 := readUntil(t, ctx, c1, "start", nil)
	s2 := readUntil(t, ctx, c2, "start", nil)
	you1, you2 := int(s1["you"].(float64)), int(s2["you"].(float64))
	if you1 == you2 {
		t.Fatalf("players must be on different teams: %v %v", s1, s2)
	}

	// газ + взлёт первым игроком; после отсчёта мир должен двигаться
	_ = wsjson.Write(ctx, c1, wsMsg{"t": "in", "k": 4})
	snap := readUntil(t, ctx, c2, "s", func(m wsMsg) bool { return m["tm"].(float64) > 0.5 })
	if len(snap["pl"].([]any)) != 2 {
		t.Fatalf("want 2 planes in snapshot: %v", snap["pl"])
	}

	// второй сдаётся — первый побеждает техническим
	_ = wsjson.Write(ctx, c2, wsMsg{"t": "leave"})
	end := readUntil(t, ctx, c1, "end", nil)
	if int(end["winner"].(float64)) != you1 || end["reason"] != "forfeit" || end["saved"] != true {
		t.Fatalf("unexpected end: %v", end)
	}
	u1, _ := s.getUser(ctx, 1)
	u2, _ := s.getUser(ctx, 2)
	if u1.PvPRating <= 1000 || u2.PvPRating >= 1000 || u1.PvPRating+u2.PvPRating != 2000 {
		t.Fatalf("pvp ratings: %d %d", u1.PvPRating, u2.PvPRating)
	}
	if u1.Rating != 1000 {
		t.Fatal("pvp must not touch bot rating")
	}
	board, err := s.pvpLeaderboard(ctx, 1, 10)
	if err != nil || len(board) != 2 || board[0].Name != "Анна" || !board[0].Me {
		t.Fatalf("pvp board: %+v %v", board, err)
	}

	// после матча можно сразу создать комнату, и второй игрок в неё заходит
	_ = wsjson.Write(ctx, c1, wsMsg{"t": "create", "target": 5})
	room := readUntil(t, ctx, c1, "room", nil)
	if !strings.Contains(room["link"].(string), "t.me/TestBot?start=room_") {
		t.Fatalf("room link: %v", room)
	}
	_ = wsjson.Write(ctx, c2, wsMsg{"t": "join", "code": strings.ToLower(room["code"].(string))})
	if m := readUntil(t, ctx, c2, "start", nil); int(m["target"].(float64)) != 5 || m["kind"] != "invite" {
		t.Fatalf("invite start: %v", m)
	}
}
