package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testToken = "123456:TEST-token"

// signInitData собирает initData так же, как Telegram.
func signInitData(t *testing.T, token string, fields map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + fields[k]
	}
	secret := hmacSHA256([]byte("WebAppData"), []byte(token))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(hmacSHA256(secret, []byte(strings.Join(lines, "\n")))))
	return v.Encode()
}

func TestValidateInitData(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	fields := map[string]string{
		"auth_date": strconv.FormatInt(now.Add(-time.Minute).Unix(), 10),
		"query_id":  "AAH",
		"user":      `{"id":42,"first_name":"Анна","username":"anna","photo_url":"https://t.me/i/userpic/320/a.jpg"}`,
		"signature": "sig",
	}
	good := signInitData(t, testToken, fields)

	u, err := validateInitData(good, testToken, time.Hour, now)
	if err != nil {
		t.Fatalf("valid initData rejected: %v", err)
	}
	if u.ID != 42 || u.FirstName != "Анна" || u.Username != "anna" {
		t.Fatalf("unexpected user: %+v", u)
	}

	if _, err := validateInitData(good, "999:other", time.Hour, now); !errors.Is(err, errInitDataSignature) {
		t.Fatalf("wrong token: want signature error, got %v", err)
	}

	tampered := strings.Replace(good, "anna", "evil", 1)
	if _, err := validateInitData(tampered, testToken, time.Hour, now); !errors.Is(err, errInitDataSignature) {
		t.Fatalf("tampered: want signature error, got %v", err)
	}

	if _, err := validateInitData(good, testToken, time.Hour, now.Add(2*time.Hour)); !errors.Is(err, errInitDataExpired) {
		t.Fatalf("old: want expired, got %v", err)
	}

	if _, err := validateInitData("user=%7B%7D", testToken, time.Hour, now); !errors.Is(err, errInitDataMalformed) {
		t.Fatalf("no hash: want malformed, got %v", err)
	}
}

func TestEloDelta(t *testing.T) {
	if d := eloDelta(1000, botRating, true, 10); d <= 0 || d > 32 {
		t.Fatalf("win vs stronger bot: got %d", d)
	}
	if d := eloDelta(1000, botRating, false, 10); d >= 0 {
		t.Fatalf("loss: got %d", d)
	}
	if eloDelta(1000, botRating, true, 15) <= eloDelta(1000, botRating, true, 5) {
		t.Fatal("longer match must weigh more")
	}
}

func TestValidateMatch(t *testing.T) {
	ok := matchInput{Mode: "bot", Target: 10, MyScore: 10, OppScore: 7, DurationMs: 180000, Shots: 50, Hits: 12, Kills: 8}
	if err := validateMatch(ok); err != nil {
		t.Fatalf("valid match rejected: %v", err)
	}
	bad := []matchInput{
		{Mode: "pvp", Target: 10, MyScore: 10, DurationMs: 180000},
		{Mode: "bot", Target: 7, MyScore: 7, DurationMs: 180000},
		{Mode: "bot", Target: 10, MyScore: 9, OppScore: 3, DurationMs: 180000},
		{Mode: "bot", Target: 10, MyScore: 10, DurationMs: 3000},
		{Mode: "bot", Target: 10, MyScore: 10, DurationMs: 180000, Shots: 1, Hits: 5},
	}
	for i, m := range bad {
		if validateMatch(m) == nil {
			t.Errorf("case %d: invalid match accepted: %+v", i, m)
		}
	}
}

func TestLoginFlowAndMatch(t *testing.T) {
	ctx := context.Background()
	s, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	now := time.Unix(1_800_000_000, 0)

	if err := s.createLogin(ctx, "n1", sha256Hex("sec"), now, loginTTL); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := s.consumeLogin(ctx, "n1", sha256Hex("sec"), now); st != loginPending {
		t.Fatalf("want pending, got %v", st)
	}
	if err := s.upsertUser(ctx, tgUser{ID: 7, FirstName: "Пётр"}, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.confirmLogin(ctx, "n1", 7, now); err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	if ok, _ := s.confirmLogin(ctx, "n1", 8, now); ok {
		t.Fatal("second confirm must fail")
	}
	if st, _, _ := s.consumeLogin(ctx, "n1", sha256Hex("wrong"), now); st != loginGone {
		t.Fatal("wrong secret must not log in")
	}
	st, id, err := s.consumeLogin(ctx, "n1", sha256Hex("sec"), now)
	if err != nil || st != loginConfirmed || id != 7 {
		t.Fatalf("consume: %v %d %v", st, id, err)
	}
	if st, _, _ := s.consumeLogin(ctx, "n1", sha256Hex("sec"), now); st != loginGone {
		t.Fatal("login must be single-use")
	}

	m := matchInput{Mode: "bot", Target: 10, MyScore: 10, OppScore: 4, DurationMs: 200000}
	res, err := s.saveMatch(ctx, 7, m, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Rated || res.RatingAfter <= 1000 || res.Rank != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if _, err := s.saveMatch(ctx, 7, m, now.Add(time.Second)); !errors.Is(err, errTooSoon) {
		t.Fatalf("want too soon, got %v", err)
	}
	board, err := s.leaderboard(ctx, 7, 10)
	if err != nil || len(board) != 1 || !board[0].Me || board[0].Name != "Пётр" {
		t.Fatalf("board: %+v %v", board, err)
	}
}
