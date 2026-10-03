// Package sim — серверная физика «Бипланов» для онлайн-матчей.
// Это построчный перенос updatePlane / updatePilot / step из game.js:
// при правке физики менять оба места, иначе онлайн и офлайн разъедутся.
package sim

import "math"

const (
	W      = 320.0
	H      = 200.0
	Ground = 180.0
	BarnX  = 160.0

	thrust  = 95.0
	drag    = 0.9
	gAlong  = 70.0
	maxS    = 125.0
	stall   = 30.0
	takeoff = 42.0
	turnR   = 2.7
	grav    = 140.0
	bulletV = 210.0
	bLife   = 0.75
	fireCD  = 0.8 // не чаще 1.25 выстрела в секунду
	respawn = 2.2

	MaxHP       = 3   // два попадания самолёт держит (дым, потом огонь), третье сбивает
	shieldGrace = 1.0 // столько ещё длится защита после отрыва от земли
)

// Rules — правила комнаты.
type Rules struct {
	Shield bool `json:"shield"` // неуязвимость (мигание) на стартовом взлёте
	Ram    bool `json:"ram"`    // таран: столкновение взрывает оба самолёта
}

func DefaultRules() Rules { return Rules{Shield: true} }

var Hangar = [2]float64{24, 296}

var teamName = [2]string{"Красные", "Синие"}

type PlaneState uint8

const (
	OnGround PlaneState = iota
	InAir
	Stalled
)

type PilotState uint8

const (
	Falling PilotState = iota
	Chute
	Walking
)

type Input struct {
	Left, Right, Up, Down, Fire bool
	Eject                       bool // фронт нажатия: true только в одном тике
}

type Plane struct {
	ID                  int
	Team                int
	X, Y, A, Speed, Thr float64
	VX, VY              float64
	State               PlaneState
	HP                  int
	Pilot               bool
	CD, Repair          float64 // CD — перезарядка пулемёта
	Dead                bool
	Shield              bool    // неуязвим: только что из ангара и ещё не взлетел
	Lift                float64 // сколько секунд в воздухе под защитой
}

type Pilot struct {
	ID, Team     int
	X, Y, VX, VY float64
	State        PilotState
	T, Step      float64
	Dir          int
	Dead         bool
}

type Bullet struct {
	ID     int
	Team   int
	X, Y   float64
	VX, VY float64
	t      float64
}

type Stats struct {
	Shots, Hits, Kills, Deaths, Ejects int
}

type Player struct {
	Plane   *Plane
	Pilot   *Pilot
	Respawn float64
	Score   int
	Stats   Stats
}

// Event — то, что клиент превращает в частицы, звук и всплывающие сообщения.
type Event struct {
	Kind string  `json:"k"`
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	Team int     `json:"t"`
	Msg  string  `json:"m,omitempty"`
}

type World struct {
	Players [2]*Player
	Planes  []*Plane
	Pilots  []*Pilot
	Bullets []*Bullet
	Events  []Event
	Time    float64
	Target  int
	Rules   Rules
	Winner  int // -1 пока матч идёт
	nextID  int
}

func New(target int, rules Rules) *World {
	w := &World{Target: target, Rules: rules, Winner: -1}
	for team := 0; team < 2; team++ {
		pl := w.newPlane(team)
		w.Planes = append(w.Planes, pl)
		w.Players[team] = &Player{Plane: pl}
	}
	return w
}

func (w *World) id() int { w.nextID++; return w.nextID }

func (w *World) newPlane(team int) *Plane {
	a := 0.0
	if team == 1 {
		a = math.Pi
	}
	return &Plane{ID: w.id(), Team: team, X: Hangar[team], Y: Ground - 5, A: a, State: OnGround, HP: MaxHP, Pilot: true, Shield: w.Rules.Shield}
}

func (w *World) emit(e Event) { w.Events = append(w.Events, e) }

// TakeEvents отдаёт накопленные события и очищает очередь.
func (w *World) TakeEvents() []Event {
	ev := w.Events
	w.Events = nil
	return ev
}

// ---------- math helpers (как в game.js) ----------

func wrapX(x float64) float64 { return math.Mod(math.Mod(x, W)+W, W) }

func wrapDx(from, to float64) float64 {
	return math.Mod(math.Mod(to-from, W)+W*1.5, W) - W/2
}

func angDiff(a, b float64) float64 {
	d := a - b
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d < -math.Pi {
		d += 2 * math.Pi
	}
	return d
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func dist(ax, ay, bx, by float64) float64 { return math.Hypot(wrapDx(ax, bx), by-ay) }

func sign(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func hitBarn(x, y, r float64) bool {
	return math.Abs(wrapDx(BarnX, x)) < 8+r && y+r >= 147 && y-r < Ground
}

// ---------- scoring ----------

func (w *World) pilotDied(team int, msg string, byTeam int) {
	w.Players[team].Stats.Deaths++
	if byTeam == 1-team {
		w.Players[byTeam].Stats.Kills++
	}
	p := w.Players[team]
	p.Pilot = nil
	if p.Plane != nil && p.Plane.Dead {
		p.Plane = nil
	}
	p.Respawn = respawn
	other := w.Players[1-team]
	other.Score++
	w.emit(Event{Kind: "point", Team: 1 - team, Msg: msg})
	if other.Score >= w.Target && w.Winner < 0 {
		w.Winner = 1 - team
	}
}

func (w *World) destroyPlane(pl *Plane, byTeam int) {
	if pl.Dead {
		return
	}
	pl.Dead = true
	w.emit(Event{Kind: "boom", X: pl.X, Y: pl.Y, Team: pl.Team})
	owner := w.Players[pl.Team]
	if owner.Plane == pl {
		owner.Plane = nil
	}
	if pl.Pilot {
		msg := teamName[pl.Team] + " разбились"
		if byTeam == 1-pl.Team {
			msg = teamName[byTeam] + " сбили " + map[int]string{0: "красных", 1: "синих"}[pl.Team]
		}
		w.pilotDied(pl.Team, msg, byTeam)
	}
}

func (w *World) killPilot(pt *Pilot, msg string, byTeam int) {
	if pt.Dead {
		return
	}
	pt.Dead = true
	w.emit(Event{Kind: "pdie", X: pt.X, Y: pt.Y, Team: pt.Team})
	w.pilotDied(pt.Team, msg, byTeam)
}

// ---------- physics ----------

func (w *World) updatePlane(pl *Plane, in *Input, dt float64) {
	var I Input
	if in != nil {
		I = *in
	}
	pl.CD -= dt
	if !pl.Pilot {
		pl.Thr = math.Max(0, pl.Thr-0.35*dt)
		if pl.State == InAir {
			if math.Cos(pl.A) >= 0 {
				pl.A += 0.6 * dt
			} else {
				pl.A -= 0.6 * dt
			}
		}
		I = Input{}
	}
	if I.Up {
		pl.Thr = math.Min(1, pl.Thr+dt*1.1)
	}
	if I.Down {
		pl.Thr = math.Max(0, pl.Thr-dt*1.1)
	}
	turn := sign(I.Right) - sign(I.Left)

	switch pl.State {
	case OnGround:
		face := -1.0
		if math.Cos(pl.A) > 0 {
			face = 1
		}
		brake := 0.0
		if pl.Thr < 0.05 {
			brake = 14
		}
		pl.Speed += (thrust*pl.Thr - 1.2*pl.Speed - brake) * dt
		if pl.Speed < 0 {
			pl.Speed = 0
		}
		pl.VX, pl.VY = face*pl.Speed, 0
		pl.X += pl.VX * dt
		pl.Y = Ground - 5
		noseUp := (face > 0 && turn < 0) || (face < 0 && turn > 0)
		if noseUp && pl.Speed > takeoff {
			pl.State = InAir
			pl.A += turn * 0.12
			pl.Y -= 1
		}
		if pl.Pilot && pl.HP < MaxHP && pl.Speed < 8 && math.Abs(wrapDx(pl.X, Hangar[pl.Team])) < 10 {
			pl.Repair += dt
			if pl.Repair > 1.2 {
				pl.HP = MaxHP
				pl.Repair = 0
				w.emit(Event{Kind: "fix", Team: pl.Team})
			}
		} else {
			pl.Repair = 0
		}
	case InAir:
		pl.A += turn * turnR * dt
		pl.Speed += (thrust*pl.Thr - drag*pl.Speed + gAlong*math.Sin(pl.A)) * dt
		pl.Speed = math.Min(pl.Speed, maxS)
		pl.VX, pl.VY = math.Cos(pl.A)*pl.Speed, math.Sin(pl.A)*pl.Speed
		pl.X += pl.VX * dt
		pl.Y += pl.VY * dt
		if pl.Speed < stall || pl.Y < 6 {
			pl.State = Stalled
			if pl.Y < 6 {
				pl.VY = math.Max(pl.VY, 10)
			}
		}
	case Stalled:
		pl.VY += grav * dt
		pl.VX *= 1 - 0.4*dt
		pl.VX += math.Cos(pl.A) * thrust * pl.Thr * 0.4 * dt
		pl.VY += math.Sin(pl.A) * thrust * pl.Thr * 0.4 * dt
		target := math.Atan2(pl.VY, pl.VX)
		pl.A += clamp(angDiff(target, pl.A), -2.5*dt, 2.5*dt) + turn*turnR*0.35*dt
		pl.X += pl.VX * dt
		pl.Y += pl.VY * dt
		pl.Speed = math.Hypot(pl.VX, pl.VY)
		if pl.Speed > stall*1.5 && math.Sin(pl.A) > 0.25 && pl.Y > 12 {
			pl.State = InAir
		}
	}
	pl.A = angDiff(pl.A, 0)
	pl.X = wrapX(pl.X)
	if pl.Shield && pl.State != OnGround {
		pl.Lift += dt
		if pl.Lift >= shieldGrace {
			pl.Shield = false
		}
	}

	if I.Fire && pl.Pilot && pl.CD <= 0 {
		pl.CD = fireCD
		ca, sa := math.Cos(pl.A), math.Sin(pl.A)
		w.Bullets = append(w.Bullets, &Bullet{
			ID: w.id(), Team: pl.Team, X: wrapX(pl.X + ca*10), Y: pl.Y + sa*10,
			VX: ca*bulletV + pl.VX, VY: sa*bulletV + pl.VY, t: bLife,
		})
		w.Players[pl.Team].Stats.Shots++
		w.emit(Event{Kind: "shot", Team: pl.Team})
	}

	if pl.State != OnGround && pl.Y+4 >= Ground {
		soft := pl.Pilot && math.Abs(math.Sin(pl.A)) < 0.28 && pl.VY < 55 && pl.Speed < 80
		if soft {
			pl.State = OnGround
			if math.Cos(pl.A) > 0 {
				pl.A = 0
			} else {
				pl.A = math.Pi
			}
			pl.Y = Ground - 5
			pl.Speed = math.Abs(pl.VX)
			w.emit(Event{Kind: "land", Team: pl.Team})
		} else {
			w.destroyPlane(pl, -1)
		}
	}
	if !pl.Dead && hitBarn(pl.X, pl.Y, 4) {
		w.destroyPlane(pl, -1)
	}
}

func (w *World) eject(team int) {
	p := w.Players[team]
	pl := p.Plane
	if pl == nil || !pl.Pilot || pl.State == OnGround {
		return
	}
	pl.Pilot = false
	pt := &Pilot{ID: w.id(), Team: team, X: pl.X, Y: pl.Y - 4, VX: pl.VX * 0.5, VY: pl.VY*0.5 - 55, State: Falling, Dir: 1}
	w.Pilots = append(w.Pilots, pt)
	p.Plane, p.Pilot = nil, pt
	p.Stats.Ejects++
	w.emit(Event{Kind: "eject", Team: team})
}

func (w *World) updatePilot(team int, I Input, dt float64) {
	p := w.Players[team]
	pt := p.Pilot
	pt.T += dt
	mv := sign(I.Right) - sign(I.Left)
	switch pt.State {
	case Falling:
		pt.VY += grav * dt
		pt.VX *= 1 - 0.5*dt
		if I.Eject && pt.T > 0.15 {
			pt.State = Chute
			w.emit(Event{Kind: "chute", Team: team})
		}
	case Chute:
		pt.VY += (22 - pt.VY) * 3 * dt
		pt.VX += (mv*28 - pt.VX) * 2 * dt
	case Walking:
		pt.VX, pt.VY = mv*18, 0
		pt.Y = Ground - 3
		if mv != 0 {
			pt.Dir = int(mv)
			pt.Step += dt
		}
	}
	pt.X = wrapX(pt.X + pt.VX*dt)
	pt.Y += pt.VY * dt
	if pt.Y < 2 {
		pt.Y = 2
		pt.VY = math.Max(0, pt.VY)
	}
	if pt.State != Walking && pt.Y+2 >= Ground {
		if pt.State == Falling && pt.VY > 75 {
			w.killPilot(pt, teamName[team]+": парашют не раскрылся", -1)
			return
		}
		pt.State = Walking
		pt.Y = Ground - 3
		pt.VX = 0
		w.emit(Event{Kind: "land", Team: team})
	}
	if pt.State == Walking && math.Abs(wrapDx(pt.X, Hangar[team])) < 3 {
		pt.Dead = true
		p.Pilot = nil
		p.Respawn = 0.5
		w.emit(Event{Kind: "home", Team: team})
	}
}

// Step продвигает мир на dt. После победы мир продолжает жить (для взрыва на экране),
// но очки больше не меняют Winner.
func (w *World) Step(dt float64, inputs [2]Input) {
	w.Time += dt
	for team, p := range w.Players {
		if p.Plane != nil && inputs[team].Eject && p.Plane.Pilot && p.Plane.State != OnGround {
			w.eject(team)
			inputs[team].Eject = false
		}
	}
	for _, pl := range w.Planes {
		var in *Input
		if w.Players[pl.Team].Plane == pl {
			in = &inputs[pl.Team]
		}
		w.updatePlane(pl, in, dt)
	}
	for team, p := range w.Players {
		if p.Pilot != nil && !p.Pilot.Dead {
			w.updatePilot(team, inputs[team], dt)
		}
	}

	for _, b := range w.Bullets {
		b.X = wrapX(b.X + b.VX*dt)
		b.Y += b.VY * dt
		b.t -= dt
		if b.Y >= Ground {
			b.t = 0
			w.emit(Event{Kind: "dust", X: b.X, Y: Ground - 1})
			continue
		}
		if hitBarn(b.X, b.Y, 0) {
			b.t = 0
			w.emit(Event{Kind: "dust", X: b.X, Y: b.Y})
			continue
		}
		for _, pl := range w.Planes {
			if pl.Dead || pl.Team == b.Team || pl.Shield {
				continue
			}
			if dist(pl.X, pl.Y, b.X, b.Y) < 6 {
				b.t = 0
				w.emit(Event{Kind: "hit", X: b.X, Y: b.Y, Team: b.Team})
				w.Players[b.Team].Stats.Hits++
				pl.HP--
				if pl.HP <= 0 {
					w.destroyPlane(pl, b.Team)
				}
				break
			}
		}
		if b.t <= 0 {
			continue
		}
		for _, pt := range w.Pilots {
			if pt.Dead || pt.Team == b.Team {
				continue
			}
			py := pt.Y
			if pt.State == Chute {
				py -= 2
			}
			if math.Hypot(wrapDx(pt.X, b.X), b.Y-py) < 3.5 {
				b.t = 0
				w.Players[b.Team].Stats.Hits++
				w.killPilot(pt, teamName[b.Team]+" подстрелили пилота", b.Team)
				break
			}
		}
	}

	for i := 0; i < len(w.Planes) && w.Rules.Ram; i++ {
		for j := i + 1; j < len(w.Planes); j++ {
			a, b := w.Planes[i], w.Planes[j]
			if a.Dead || b.Dead || a.Shield || b.Shield || (a.State == OnGround && b.State == OnGround) {
				continue
			}
			if dist(a.X, a.Y, b.X, b.Y) < 9 {
				w.destroyPlane(a, -1)
				w.destroyPlane(b, -1)
			}
		}
	}
	for _, pl := range w.Planes {
		for _, pt := range w.Pilots {
			if pl.Dead || pt.Dead || pl.Team == pt.Team {
				continue
			}
			if dist(pl.X, pl.Y, pt.X, pt.Y) < 6 {
				w.killPilot(pt, teamName[pl.Team]+" задели пилота винтом", pl.Team)
			}
		}
	}

	w.Bullets = filter(w.Bullets, func(b *Bullet) bool { return b.t > 0 })
	w.Planes = filter(w.Planes, func(p *Plane) bool { return !p.Dead })
	w.Pilots = filter(w.Pilots, func(p *Pilot) bool { return !p.Dead })
	for _, p := range w.Players {
		if p.Plane != nil && p.Plane.Dead {
			p.Plane = nil
		}
		if p.Pilot != nil && p.Pilot.Dead {
			p.Pilot = nil
		}
	}

	for team, p := range w.Players {
		if p.Plane != nil || p.Pilot != nil {
			continue
		}
		p.Respawn -= dt
		if p.Respawn <= 0 {
			pl := w.newPlane(team)
			w.Planes = append(w.Planes, pl)
			p.Plane = pl
		}
	}
}

func filter[T any](s []T, keep func(T) bool) []T {
	out := s[:0]
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}
