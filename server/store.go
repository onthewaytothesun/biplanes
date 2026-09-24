package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	tg_id      INTEGER PRIMARY KEY,
	username   TEXT    NOT NULL DEFAULT '',
	first_name TEXT    NOT NULL DEFAULT '',
	last_name  TEXT    NOT NULL DEFAULT '',
	photo_url  TEXT    NOT NULL DEFAULT '',
	rating     INTEGER NOT NULL DEFAULT 1000,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT    PRIMARY KEY,
	tg_id      INTEGER NOT NULL REFERENCES users(tg_id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS login_requests (
	nonce        TEXT    PRIMARY KEY,
	secret_hash  TEXT    NOT NULL,
	tg_id        INTEGER REFERENCES users(tg_id) ON DELETE CASCADE,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	confirmed_at INTEGER,
	consumed     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS matches (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	tg_id         INTEGER NOT NULL REFERENCES users(tg_id) ON DELETE CASCADE,
	mode          TEXT    NOT NULL CHECK (mode IN ('bot', 'duo')),
	target        INTEGER NOT NULL,
	my_score      INTEGER NOT NULL,
	opp_score     INTEGER NOT NULL,
	won           INTEGER NOT NULL,
	duration_ms   INTEGER NOT NULL,
	shots         INTEGER NOT NULL DEFAULT 0,
	hits          INTEGER NOT NULL DEFAULT 0,
	kills         INTEGER NOT NULL DEFAULT 0,
	deaths        INTEGER NOT NULL DEFAULT 0,
	ejects        INTEGER NOT NULL DEFAULT 0,
	rating_before INTEGER NOT NULL,
	rating_after  INTEGER NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS matches_user ON matches(tg_id, created_at);

`

var errNotFound = errors.New("not found")

type store struct{ db *sql.DB }

func openStore(path string) (*store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite пишет в один поток; одно соединение снимает SQLITE_BUSY между своими же запросами.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) close() error { return s.db.Close() }

type user struct {
	TgID      int64  `json:"tgId"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	PhotoURL  string `json:"photoUrl"`
	Rating    int    `json:"rating"`
}

func (s *store) upsertUser(ctx context.Context, u tgUser, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (tg_id, username, first_name, last_name, photo_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tg_id) DO UPDATE SET
			username   = excluded.username,
			first_name = excluded.first_name,
			last_name  = excluded.last_name,
			photo_url  = COALESCE(NULLIF(excluded.photo_url, ''), users.photo_url),
			updated_at = excluded.updated_at`,
		u.ID, u.Username, u.FirstName, u.LastName, u.PhotoURL, now.Unix(), now.Unix())
	return err
}

func (s *store) getUser(ctx context.Context, tgID int64) (user, error) {
	var u user
	err := s.db.QueryRowContext(ctx,
		`SELECT tg_id, username, first_name, last_name, photo_url, rating FROM users WHERE tg_id = ?`, tgID).
		Scan(&u.TgID, &u.Username, &u.FirstName, &u.LastName, &u.PhotoURL, &u.Rating)
	if errors.Is(err, sql.ErrNoRows) {
		return u, errNotFound
	}
	return u, err
}

// ---------- sessions ----------

func (s *store) createSession(ctx context.Context, tokenHash string, tgID int64, now time.Time, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, tg_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, tgID, now.Unix(), now.Add(ttl).Unix())
	return err
}

func (s *store) sessionUser(ctx context.Context, tokenHash string, now time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT tg_id FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotFound
	}
	return id, err
}

func (s *store) deleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// ---------- login via bot deep link ----------

func (s *store) createLogin(ctx context.Context, nonce, secretHash string, now time.Time, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_requests (nonce, secret_hash, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		nonce, secretHash, now.Unix(), now.Add(ttl).Unix())
	return err
}

// loginOpen сообщает, ждёт ли запрос подтверждения (есть, не истёк, не подтверждён).
func (s *store) loginOpen(ctx context.Context, nonce string, now time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM login_requests WHERE nonce = ? AND expires_at > ? AND confirmed_at IS NULL`,
		nonce, now.Unix()).Scan(&n)
	return n > 0, err
}

func (s *store) confirmLogin(ctx context.Context, nonce string, tgID int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE login_requests SET tg_id = ?, confirmed_at = ?
		WHERE nonce = ? AND expires_at > ? AND confirmed_at IS NULL`,
		tgID, now.Unix(), nonce, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

type loginState int

const (
	loginGone loginState = iota // нет, истёк, уже использован или неверный секрет
	loginPending
	loginConfirmed
)

// consumeLogin атомарно забирает подтверждённый запрос: второй раз он уже не отдаст tg_id.
func (s *store) consumeLogin(ctx context.Context, nonce, secretHash string, now time.Time) (loginState, int64, error) {
	var tgID sql.NullInt64
	var confirmed sql.NullInt64
	var consumed int
	err := s.db.QueryRowContext(ctx, `
		SELECT tg_id, confirmed_at, consumed FROM login_requests
		WHERE nonce = ? AND secret_hash = ? AND expires_at > ?`,
		nonce, secretHash, now.Unix()).Scan(&tgID, &confirmed, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return loginGone, 0, nil
	}
	if err != nil {
		return loginGone, 0, err
	}
	if consumed == 1 {
		return loginGone, 0, nil
	}
	if !confirmed.Valid || !tgID.Valid {
		return loginPending, 0, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE login_requests SET consumed = 1 WHERE nonce = ? AND consumed = 0`, nonce)
	if err != nil {
		return loginGone, 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return loginGone, 0, nil
	}
	return loginConfirmed, tgID.Int64, nil
}

func (s *store) cleanup(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_requests WHERE expires_at <= ?`, now.Add(-time.Hour).Unix())
	return err
}

// ---------- matches & rating ----------

type matchInput struct {
	Mode       string `json:"mode"`
	Target     int    `json:"target"`
	MyScore    int    `json:"myScore"`
	OppScore   int    `json:"oppScore"`
	DurationMs int64  `json:"durationMs"`
	Shots      int    `json:"shots"`
	Hits       int    `json:"hits"`
	Kills      int    `json:"kills"`
	Deaths     int    `json:"deaths"`
	Ejects     int    `json:"ejects"`
}

type matchResult struct {
	Rated        bool `json:"rated"`
	RatingBefore int  `json:"ratingBefore"`
	RatingAfter  int  `json:"ratingAfter"`
	Delta        int  `json:"delta"`
	Rank         int  `json:"rank"`
}

var errTooSoon = errors.New("too soon")

func (s *store) saveMatch(ctx context.Context, tgID int64, m matchInput, now time.Time) (matchResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return matchResult{}, err
	}
	defer tx.Rollback()

	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(created_at) FROM matches WHERE tg_id = ?`, tgID).Scan(&last); err != nil {
		return matchResult{}, err
	}
	if last.Valid && now.Unix()-last.Int64 < minSecondsBetweenMatches {
		return matchResult{}, errTooSoon
	}

	var before int
	if err := tx.QueryRowContext(ctx, `SELECT rating FROM users WHERE tg_id = ?`, tgID).Scan(&before); err != nil {
		return matchResult{}, err
	}
	won := m.MyScore > m.OppScore
	after, rated := before, m.Mode == "bot"
	if rated {
		after = before + eloDelta(before, botRating, won, m.Target)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO matches (tg_id, mode, target, my_score, opp_score, won, duration_ms,
			shots, hits, kills, deaths, ejects, rating_before, rating_after, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tgID, m.Mode, m.Target, m.MyScore, m.OppScore, boolInt(won), m.DurationMs,
		m.Shots, m.Hits, m.Kills, m.Deaths, m.Ejects, before, after, now.Unix()); err != nil {
		return matchResult{}, err
	}
	if after != before {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET rating = ? WHERE tg_id = ?`, after, tgID); err != nil {
			return matchResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return matchResult{}, err
	}
	rank, err := s.rank(ctx, tgID)
	if err != nil {
		return matchResult{}, err
	}
	return matchResult{Rated: rated, RatingBefore: before, RatingAfter: after, Delta: after - before, Rank: rank}, nil
}

// rank — место в рейтинге среди тех, кто сыграл хотя бы один матч против бота; 0 — ещё не в рейтинге.
func (s *store) rank(ctx context.Context, tgID int64) (int, error) {
	var played int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM matches WHERE tg_id = ? AND mode = 'bot'`, tgID).Scan(&played); err != nil {
		return 0, err
	}
	if played == 0 {
		return 0, nil
	}
	var r int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) + 1 FROM users u
		WHERE u.rating > (SELECT rating FROM users WHERE tg_id = ?)
		  AND EXISTS (SELECT 1 FROM matches m WHERE m.tg_id = u.tg_id AND m.mode = 'bot')`, tgID).Scan(&r)
	return r, err
}

type stats struct {
	Matches    int `json:"matches"`
	BotMatches int `json:"botMatches"`
	Wins       int `json:"wins"`
	BotWins    int `json:"botWins"`
	Kills      int `json:"kills"`
	Deaths     int `json:"deaths"`
	Shots      int `json:"shots"`
	Hits       int `json:"hits"`
	BestRating int `json:"bestRating"`
}

func (s *store) userStats(ctx context.Context, tgID int64) (stats, error) {
	var st stats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(mode = 'bot'), 0),
		       COALESCE(SUM(won), 0),
		       COALESCE(SUM(won AND mode = 'bot'), 0),
		       COALESCE(SUM(kills), 0), COALESCE(SUM(deaths), 0),
		       COALESCE(SUM(shots), 0), COALESCE(SUM(hits), 0),
		       COALESCE(MAX(rating_after), 0)
		FROM matches WHERE tg_id = ?`, tgID).
		Scan(&st.Matches, &st.BotMatches, &st.Wins, &st.BotWins, &st.Kills, &st.Deaths, &st.Shots, &st.Hits, &st.BestRating)
	return st, err
}

type recentMatch struct {
	Mode        string `json:"mode"`
	Target      int    `json:"target"`
	MyScore     int    `json:"myScore"`
	OppScore    int    `json:"oppScore"`
	Won         bool   `json:"won"`
	RatingDelta int    `json:"ratingDelta"`
	PlayedAt    int64  `json:"playedAt"`
}

func (s *store) recentMatches(ctx context.Context, tgID int64, limit int) ([]recentMatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mode, target, my_score, opp_score, won, rating_after - rating_before, created_at
		FROM matches WHERE tg_id = ? ORDER BY id DESC LIMIT ?`, tgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recentMatch{}
	for rows.Next() {
		var m recentMatch
		if err := rows.Scan(&m.Mode, &m.Target, &m.MyScore, &m.OppScore, &m.Won, &m.RatingDelta, &m.PlayedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type boardRow struct {
	Rank     int    `json:"rank"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Rating   int    `json:"rating"`
	Matches  int    `json:"matches"`
	Wins     int    `json:"wins"`
	Me       bool   `json:"me"`
}

func (s *store) leaderboard(ctx context.Context, me int64, limit int) ([]boardRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.tg_id, u.first_name, u.last_name, u.username, u.rating, COUNT(m.id), COALESCE(SUM(m.won), 0)
		FROM users u JOIN matches m ON m.tg_id = u.tg_id AND m.mode = 'bot'
		GROUP BY u.tg_id
		ORDER BY u.rating DESC, COUNT(m.id) DESC, u.tg_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []boardRow{}
	for rows.Next() {
		var r boardRow
		var id int64
		var first, last string
		if err := rows.Scan(&id, &first, &last, &r.Username, &r.Rating, &r.Matches, &r.Wins); err != nil {
			return nil, err
		}
		r.Rank = len(out) + 1
		r.Name = displayName(first, last, r.Username)
		r.Me = id == me
		out = append(out, r)
	}
	return out, rows.Err()
}

func displayName(first, last, username string) string {
	n := first
	if last != "" {
		n += " " + last
	}
	if n == "" && username != "" {
		n = "@" + username
	}
	if n == "" {
		n = "Пилот"
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
