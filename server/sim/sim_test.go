package sim

import (
	"math"
	"testing"
)

const dt = 1.0 / 60

func run(w *World, seconds float64, in func(t float64) [2]Input) {
	for t := 0.0; t < seconds; t += dt {
		w.Step(dt, in(t))
	}
}

func hasEvent(evs []Event, kind string) bool {
	for _, e := range evs {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func TestTakeoff(t *testing.T) {
	w := New(10, DefaultRules())
	run(w, 4, func(float64) [2]Input {
		pl := w.Players[0].Plane
		return [2]Input{{Up: true, Left: pl != nil && pl.Speed > takeoff+2}}
	})
	pl := w.Players[0].Plane
	if pl == nil || pl.State == OnGround || pl.Y > Ground-20 {
		t.Fatalf("red must be airborne and climbing: %+v", pl)
	}
	if b := w.Players[1].Plane; b.State != OnGround || b.X != Hangar[1] {
		t.Fatalf("idle blue must stay in hangar: %+v", b)
	}
}

func TestShootDown(t *testing.T) {
	w := New(10, DefaultRules())
	red, blue := w.Players[0].Plane, w.Players[1].Plane
	*red = Plane{ID: red.ID, Team: 0, X: 100, Y: 80, A: 0, Speed: 60, Thr: 1, State: InAir, HP: MaxHP, Pilot: true}
	*blue = Plane{ID: blue.ID, Team: 1, X: 150, Y: 80, A: 0, Speed: 60, Thr: 1, State: InAir, HP: MaxHP, Pilot: true}
	var evs []Event
	hpAfter := map[int]int{} // HP синего после каждого попадания
	for i := 0; i < 180 && w.Players[1].Plane != nil; i++ {
		w.Step(dt, [2]Input{{Fire: true}, {}})
		evs = append(evs, w.TakeEvents()...)
		if p := w.Players[1].Plane; p != nil {
			hpAfter[w.Players[0].Stats.Hits] = p.HP
		}
	}
	if w.Players[1].Plane != nil {
		t.Fatal("blue must be shot down within 3s of point-blank fire")
	}
	if hpAfter[1] != 2 || hpAfter[2] != 1 {
		t.Fatalf("first two hits must only damage (smoke, then fire): %v", hpAfter)
	}
	if shots := w.Players[0].Stats.Shots; shots != 3 {
		t.Fatalf("3 shots in 3s at 1.25 shots/s, got %d", shots)
	}
	if w.Players[0].Score != 1 || w.Players[0].Stats.Kills != 1 || w.Players[1].Stats.Deaths != 1 {
		t.Fatalf("score/stats: red=%+v blue=%+v", w.Players[0], w.Players[1])
	}
	if w.Players[0].Stats.Hits != 3 || !hasEvent(evs, "boom") || !hasEvent(evs, "point") {
		t.Fatalf("want 3 hits, boom and point events; hits=%d", w.Players[0].Stats.Hits)
	}
	// через 2.2 с синий получает новый самолёт в ангаре
	run(w, 2.5, func(float64) [2]Input { return [2]Input{} })
	if p := w.Players[1].Plane; p == nil || p.State != OnGround || math.Abs(p.X-Hangar[1]) > 0.01 {
		t.Fatalf("blue must respawn in hangar: %+v", p)
	}
}

func TestEjectChuteWalkHome(t *testing.T) {
	w := New(10, DefaultRules())
	red := w.Players[0].Plane
	red.X, red.Y, red.State, red.Speed, red.A = 60, 60, InAir, 60, 0
	w.Step(dt, [2]Input{{Eject: true}, {}})
	pt := w.Players[0].Pilot
	if pt == nil || w.Players[0].Plane != nil || red.Pilot {
		t.Fatal("eject must move pilot out of the plane")
	}
	run(w, 0.3, func(float64) [2]Input { return [2]Input{} })
	w.Step(dt, [2]Input{{Eject: true}, {}}) // парашют
	if pt.State != Chute {
		t.Fatalf("second eject press must open chute, state=%v", pt.State)
	}
	var evs []Event
	for i := 0; i < 60*40 && w.Players[0].Pilot != nil; i++ {
		// идём к своему ангару (он левее)
		w.Step(dt, [2]Input{{Left: true}, {}})
		evs = append(evs, w.TakeEvents()...)
	}
	if !hasEvent(evs, "home") {
		t.Fatalf("pilot must walk home; pilot=%+v", w.Players[0].Pilot)
	}
	if w.Players[1].Score != 0 {
		t.Fatal("safe eject must not give points")
	}
}

func TestSplatWithoutChute(t *testing.T) {
	w := New(10, DefaultRules())
	red := w.Players[0].Plane
	red.X, red.Y, red.State, red.Speed, red.A = 60, 30, InAir, 60, 0
	w.Step(dt, [2]Input{{Eject: true}, {}})
	run(w, 5, func(float64) [2]Input { return [2]Input{} })
	if w.Players[1].Score != 1 {
		t.Fatalf("falling without chute must kill the pilot, blue score=%d", w.Players[1].Score)
	}
}

func TestWinner(t *testing.T) {
	w := New(5, DefaultRules())
	for i := 0; i < 5; i++ {
		w.Players[1].Pilot = nil
		w.pilotDied(1, "test", 0)
	}
	if w.Winner != 0 {
		t.Fatalf("red must win at 5, winner=%d", w.Winner)
	}
	w.pilotDied(0, "late", 1)
	if w.Winner != 0 {
		t.Fatal("winner must not change after the match is decided")
	}
}

func TestFireRate(t *testing.T) {
	w := New(10, DefaultRules())
	red := w.Players[0].Plane
	red.X, red.Y, red.State, red.Speed, red.A = 60, 60, InAir, 80, 0
	run(w, 3.9, func(float64) [2]Input { return [2]Input{{Fire: true}, {}} })
	// 0, 0.8, 1.6, 2.4, 3.2 — пять выстрелов, не больше 1.25 в секунду
	if n := w.Players[0].Stats.Shots; n != 5 {
		t.Fatalf("want 5 shots in 3.9s, got %d", n)
	}
}

// Самолёт из ангара неуязвим, пока не взлетит (и ещё shieldGrace после отрыва).
func TestSpawnShield(t *testing.T) {
	w := New(10, DefaultRules())
	blue := w.Players[1].Plane
	if !blue.Shield {
		t.Fatal("fresh plane must be shielded by default")
	}
	red := w.Players[0].Plane
	*red = Plane{ID: red.ID, Team: 0, X: blue.X - 40, Y: blue.Y, A: 0, Speed: 0, State: OnGround, HP: MaxHP, Pilot: true}
	run(w, 3, func(float64) [2]Input { return [2]Input{{Fire: true}, {}} })
	if blue.HP != MaxHP || w.Players[0].Stats.Hits != 0 {
		t.Fatalf("shielded plane must not take damage: hp=%d hits=%d", blue.HP, w.Players[0].Stats.Hits)
	}
	// взлёт: щит держится shieldGrace после отрыва, потом пропадает
	var lift float64 = -1
	for i := 0; i < 60*6 && blue.Shield; i++ {
		w.Step(dt, [2]Input{{}, {Up: true, Right: blue.Speed > takeoff+2}})
		if lift < 0 && blue.State != OnGround {
			lift = w.Time
		}
	}
	if blue.Shield || lift < 0 || blue.State == OnGround {
		t.Fatalf("shield must drop after takeoff: %+v", blue)
	}
	if got := w.Time - lift; got < shieldGrace-0.05 || got > shieldGrace+0.05 {
		t.Fatalf("shield must last %.1fs after liftoff, lasted %.2fs", shieldGrace, got)
	}

	off := New(10, Rules{})
	if off.Players[0].Plane.Shield {
		t.Fatal("shield rule off: plane must be vulnerable from the start")
	}
}

func headOn(rules Rules) *World {
	w := New(10, rules)
	red, blue := w.Players[0].Plane, w.Players[1].Plane
	*red = Plane{ID: red.ID, Team: 0, X: 140, Y: 80, A: 0, Speed: 60, Thr: 1, State: InAir, HP: MaxHP, Pilot: true}
	*blue = Plane{ID: blue.ID, Team: 1, X: 180, Y: 80, A: math.Pi, Speed: 60, Thr: 1, State: InAir, HP: MaxHP, Pilot: true}
	run(w, 0.6, func(float64) [2]Input { return [2]Input{} })
	return w
}

func TestNoRamByDefault(t *testing.T) {
	w := headOn(DefaultRules())
	if w.Players[0].Plane == nil || w.Players[1].Plane == nil {
		t.Fatal("without ram rule planes must pass through each other")
	}
}

func TestRam(t *testing.T) {
	w := headOn(Rules{Ram: true})
	if w.Players[0].Plane != nil || w.Players[1].Plane != nil {
		t.Fatal("ram rule: head-on collision must destroy both planes")
	}
	if w.Players[0].Score != 1 || w.Players[1].Score != 1 {
		t.Fatalf("both pilots die: score %d:%d", w.Players[0].Score, w.Players[1].Score)
	}
}
