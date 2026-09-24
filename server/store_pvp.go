package main

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/onthewaytothesun/biplanes/server/sim"
)

type pvpResult struct {
	Kind       string
	Target     int
	Users      [2]int64 // [красные, синие]
	Scores     [2]int
	Winner     int // 0, 1 или -1 (ничья)
	Reason     string
	DurationMs int64
	Stats      [2]sim.Stats
}

// pvpElo — изменение рейтинга красных; у синих ровно противоположное (игра с нулевой суммой).
func pvpElo(red, blue, winner, target int) int {
	expected := 1 / (1 + math.Pow(10, float64(blue-red)/400))
	score := 0.5
	switch winner {
	case 0:
		score = 1
	case 1:
		score = 0
	}
	return int(math.Round(eloK * float64(target) / 10 * (score - expected)))
}

// savePvP пишет матч и обновляет PvP-рейтинги обоих. Возвращает рейтинги до и после.
func (s *store) savePvP(ctx context.Context, r pvpResult, now time.Time) (before, after [2]int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return before, after, err
	}
	defer tx.Rollback()
	for i, id := range r.Users {
		if err = tx.QueryRowContext(ctx, `SELECT pvp_rating FROM users WHERE tg_id = ?`, id).Scan(&before[i]); err != nil {
			return before, after, err
		}
	}
	d := pvpElo(before[0], before[1], r.Winner, r.Target)
	after = [2]int{before[0] + d, before[1] - d}
	st0, _ := json.Marshal(r.Stats[0])
	st1, _ := json.Marshal(r.Stats[1])
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO pvp_matches (kind, target, red_id, blue_id, red_score, blue_score, winner, reason, duration_ms,
			red_stats, blue_stats, red_rating_before, red_rating_after, blue_rating_before, blue_rating_after, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Kind, r.Target, r.Users[0], r.Users[1], r.Scores[0], r.Scores[1], r.Winner, r.Reason, r.DurationMs,
		string(st0), string(st1), before[0], after[0], before[1], after[1], now.Unix()); err != nil {
		return before, after, err
	}
	for i, id := range r.Users {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET pvp_rating = ? WHERE tg_id = ?`, after[i], id); err != nil {
			return before, after, err
		}
	}
	return before, after, tx.Commit()
}

// Все PvP-партии игрока «со своей стороны»: my_score / opp_score / won.
const pvpGamesCTE = `
WITH g AS (
	SELECT id, red_id AS uid, blue_id AS opp, red_score AS my, blue_score AS their, winner = 0 AS won,
	       red_rating_after - red_rating_before AS delta, target, created_at FROM pvp_matches
	UNION ALL
	SELECT id, blue_id, red_id, blue_score, red_score, winner = 1,
	       blue_rating_after - blue_rating_before, target, created_at FROM pvp_matches
)`

type pvpStats struct {
	Rating  int `json:"rating"`
	Rank    int `json:"rank"`
	Matches int `json:"matches"`
	Wins    int `json:"wins"`
}

func (s *store) pvpStats(ctx context.Context, tgID int64) (pvpStats, error) {
	var st pvpStats
	err := s.db.QueryRowContext(ctx, pvpGamesCTE+`
		SELECT (SELECT pvp_rating FROM users WHERE tg_id = ?), COUNT(*), COALESCE(SUM(won), 0) FROM g WHERE uid = ?`,
		tgID, tgID).Scan(&st.Rating, &st.Matches, &st.Wins)
	if err != nil || st.Matches == 0 {
		return st, err
	}
	err = s.db.QueryRowContext(ctx, pvpGamesCTE+`
		SELECT COUNT(DISTINCT u.tg_id) + 1 FROM users u JOIN g ON g.uid = u.tg_id
		WHERE u.pvp_rating > ?`, st.Rating).Scan(&st.Rank)
	return st, err
}

type recentPvP struct {
	Opponent    string `json:"opponent"`
	Target      int    `json:"target"`
	MyScore     int    `json:"myScore"`
	OppScore    int    `json:"oppScore"`
	Won         bool   `json:"won"`
	RatingDelta int    `json:"ratingDelta"`
	PlayedAt    int64  `json:"playedAt"`
}

func (s *store) recentPvP(ctx context.Context, tgID int64, limit int) ([]recentPvP, error) {
	rows, err := s.db.QueryContext(ctx, pvpGamesCTE+`
		SELECT u.first_name, u.last_name, u.username, g.target, g.my, g.their, g.won, g.delta, g.created_at
		FROM g JOIN users u ON u.tg_id = g.opp WHERE g.uid = ? ORDER BY g.id DESC LIMIT ?`, tgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recentPvP{}
	for rows.Next() {
		var m recentPvP
		var first, last, username string
		if err := rows.Scan(&first, &last, &username, &m.Target, &m.MyScore, &m.OppScore, &m.Won, &m.RatingDelta, &m.PlayedAt); err != nil {
			return nil, err
		}
		m.Opponent = displayName(first, last, username)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *store) pvpLeaderboard(ctx context.Context, me int64, limit int) ([]boardRow, error) {
	rows, err := s.db.QueryContext(ctx, pvpGamesCTE+`
		SELECT u.tg_id, u.first_name, u.last_name, u.username, u.pvp_rating, COUNT(*), COALESCE(SUM(g.won), 0)
		FROM users u JOIN g ON g.uid = u.tg_id
		GROUP BY u.tg_id
		ORDER BY u.pvp_rating DESC, COUNT(*) DESC, u.tg_id
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
