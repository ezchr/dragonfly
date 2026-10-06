package javasession

import (
	v777 "github.com/ezchr/go-mcjava/v777"
)

// ViewTime sets the time of day. 26.3 keeps time in world clocks: set_time carries the game time
// and a map from minecraft:world_clock entries to (total ticks, partial tick, rate).
func (s *Session) ViewTime(t int) {
	s.timeMu.Lock()
	s.time = t
	cycle := !s.timeStopped
	s.timeMu.Unlock()
	rate := float32(1)
	if !cycle {
		rate = 0
	}
	w := s.packet()
	w.Int64(int64(t)) // game time
	w.VarInt(1)
	w.VarInt(s.ver.RegistryID("minecraft:world_clock", "minecraft:overworld"))
	w.VarLong(int64(t))
	w.Float32(0)
	w.Float32(rate)
	s.queue(v777.ClientboundPlaySetTime, w)
}

// ViewTimeCycle turns the daylight cycle on or off.
func (s *Session) ViewTimeCycle(doCycle bool) {
	s.timeMu.Lock()
	s.timeStopped = !doCycle
	t := s.time
	s.timeMu.Unlock()
	s.ViewTime(t)
}

// resendLevelInfo sends the time and weather again, for a client that made a new level.
func (s *Session) resendLevelInfo() {
	s.timeMu.Lock()
	t, raining, thunder := s.time, s.raining, s.thunder
	s.timeMu.Unlock()
	s.ViewTime(t)
	s.ViewWeather(raining, thunder)
}

// ViewWeather shows rain and thunder.
func (s *Session) ViewWeather(raining, thunder bool) {
	s.timeMu.Lock()
	s.raining, s.thunder = raining, thunder
	s.timeMu.Unlock()
	event := func(id byte, v float32) {
		w := s.packet()
		w.Byte(id)
		w.Float32(v)
		s.queue(v777.ClientboundPlayGameEvent, w)
	}
	if raining {
		event(1, 0) // start raining
		event(7, 1) // rain level
	} else {
		event(2, 0) // stop raining
		event(7, 0)
	}
	if thunder {
		event(8, 1) // thunder level
	} else {
		event(8, 0)
	}
}
