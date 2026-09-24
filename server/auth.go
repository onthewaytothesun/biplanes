package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	sessionTTL      = 90 * 24 * time.Hour
	loginTTL        = 5 * time.Minute
	initDataMaxAge  = 24 * time.Hour
	botRating       = 1200
	eloK            = 32.0
	loginStartParam = "login_"

	minSecondsBetweenMatches = 10
)

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
}

var (
	errInitDataMalformed = errors.New("initData malformed")
	errInitDataSignature = errors.New("initData signature mismatch")
	errInitDataExpired   = errors.New("initData expired")
)

// validateInitData проверяет подпись Telegram Mini App по алгоритму из
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func validateInitData(initData, botToken string, maxAge time.Duration, now time.Time) (tgUser, error) {
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return tgUser{}, errInitDataMalformed
	}
	hash := vals.Get("hash")
	if hash == "" {
		return tgUser{}, errInitDataMalformed
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + vals.Get(k)
	}
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	expected := hex.EncodeToString(hmacSHA256(secret, []byte(strings.Join(lines, "\n"))))
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(hash))) {
		return tgUser{}, errInitDataSignature
	}
	authDate, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil {
		return tgUser{}, errInitDataMalformed
	}
	if now.Sub(time.Unix(authDate, 0)) > maxAge {
		return tgUser{}, errInitDataExpired
	}
	var u tgUser
	if err := json.Unmarshal([]byte(vals.Get("user")), &u); err != nil || u.ID == 0 {
		return tgUser{}, errInitDataMalformed
	}
	return u, nil
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// randomToken — криптостойкая строка, годится и для start-параметра Telegram (A-Za-z0-9_-).
func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand не падает на поддерживаемых ОС
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// eloDelta — изменение рейтинга игрока против бота с фиксированным рейтингом.
// Длинные матчи весят больше: матч до 10 очков — базовый K.
func eloDelta(player, opponent int, won bool, target int) int {
	expected := 1 / (1 + math.Pow(10, float64(opponent-player)/400))
	score := 0.0
	if won {
		score = 1
	}
	k := eloK * float64(target) / 10
	return int(math.Round(k * (score - expected)))
}

// validateMatch отсекает невозможные результаты. Клиенту всё равно приходится доверять,
// но хотя бы без счёта 99:0 за три секунды.
func validateMatch(m matchInput) error {
	switch {
	case m.Mode != "bot" && m.Mode != "duo":
		return errors.New("mode must be bot or duo")
	case m.Target != 5 && m.Target != 10 && m.Target != 15:
		return errors.New("target must be 5, 10 or 15")
	case m.MyScore < 0 || m.OppScore < 0 || m.MyScore > m.Target || m.OppScore > m.Target:
		return errors.New("score out of range")
	case max(m.MyScore, m.OppScore) != m.Target || m.MyScore == m.OppScore:
		return errors.New("match is not finished")
	case m.DurationMs < int64(m.Target)*4000 || m.DurationMs > 3*3600*1000:
		return errors.New("duration out of range")
	}
	for _, v := range []int{m.Shots, m.Hits, m.Kills, m.Deaths, m.Ejects} {
		if v < 0 || v > 100000 {
			return errors.New("stats out of range")
		}
	}
	if m.Hits > m.Shots || m.Kills > m.MyScore {
		return errors.New("stats inconsistent")
	}
	return nil
}
