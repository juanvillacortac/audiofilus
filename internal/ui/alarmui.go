package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/radio"
)

// La alarma en Ajustes (la hace sonar el servicio: internal/daemon/alarm.go).
// La baldosa dice a qué hora y qué días; su menú la enciende, cambia la hora,
// los días y qué suena (lo último, una lista o una emisora favorita), y la
// prueba.

// alarmDays son las opciones de días, en orden: todos, entre semana, fin de
// semana y una vez.
var alarmDays = []struct {
	key  string
	days []int
	once bool
}{
	{"al_every", nil, false},
	{"al_weekdays", []int{1, 2, 3, 4, 5}, false},
	{"al_weekend", []int{0, 6}, false},
	{"al_once", nil, true},
}

// alarm es la alarma que dice el servicio.
func (a *App) alarm() daemon.Alarm { return a.status.Alarm }

// sendAlarm la cambia en el servicio (y aquí, para verla ya).
func (a *App) sendAlarm(al daemon.Alarm) {
	al.Next = a.status.Alarm.Next
	if b, err := json.Marshal(al); err == nil {
		a.send("alarm", "set", string(b))
	}
	a.status.Alarm = al
}

// alarmDaysIndex es cuál de alarmDays tiene al.
func alarmDaysIndex(al daemon.Alarm) int {
	for i, d := range alarmDays {
		if d.once == al.Once && slices.Equal(d.days, al.Days) {
			return i
		}
	}
	return 0
}

// alarmLabel es la alarma en corto: "07:30 · Entre semana" o apagada.
func (a *App) alarmLabel() string {
	al := a.alarm()
	if !al.On {
		return onOffLabel(false)
	}
	return fmt.Sprintf("%02d:%02d · %s", al.Hour, al.Min, T(alarmDays[alarmDaysIndex(al)].key))
}

// alarmSourceLabel es qué suena, en palabras.
func (a *App) alarmSourceLabel() string {
	src := a.alarm().Source
	switch {
	case src == "":
		return T("al_last")
	case radio.IsRadio(src):
		for _, f := range a.radio.favs {
			if f.Path() == src {
				return f.Name
			}
		}
		return T("al_station")
	}
	if a.local != nil {
		for _, pl := range a.local.Playlists {
			if pl.Path == src {
				return pl.Name
			}
		}
	}
	return src
}

// openAlarm es el menú de la alarma.
func (a *App) openAlarm() {
	items := []menuItem{
		{label: T("al_title"), icon: "clock", active: func() bool { return a.alarm().On },
			value: func() string { return onOffLabel(a.alarm().On) },
			do: func() {
				al := a.alarm()
				al.On = !al.On
				a.sendAlarm(al)
			}},
		{label: T("al_time"), icon: "clock", value: func() string { return fmt.Sprintf("%02d:%02d", a.alarm().Hour, a.alarm().Min) },
			do: func() {
				al := a.alarm()
				a.openRawPrompt(T("al_time_q"), fmt.Sprintf("%02d:%02d", al.Hour, al.Min), false, func(s string) {
					h, m, ok := parseClock(s)
					if !ok {
						a.flash(T("al_bad_time"))
						return
					}
					al := a.alarm()
					al.Hour, al.Min, al.On = h, m, true
					a.sendAlarm(al)
					a.openAlarm()
				})
			}},
		{label: T("al_days"), icon: "list", value: func() string { return T(alarmDays[alarmDaysIndex(a.alarm())].key) },
			do: func() {
				al := a.alarm()
				d := alarmDays[(alarmDaysIndex(al)+1)%len(alarmDays)]
				al.Days, al.Once = d.days, d.once
				a.sendAlarm(al)
			}},
		{label: T("al_source"), icon: "note", value: a.alarmSourceLabel, do: a.openAlarmSource},
		{label: T("al_test"), icon: "play", close: true, do: func() { a.send("alarm", "test") }},
	}
	a.menu = &menu{title: T("al_title"), subtitle: a.alarmLabel(), items: items}
}

// openAlarmSource elige qué suena: lo último, una lista o una emisora.
func (a *App) openAlarmSource() {
	set := func(src string) func() {
		return func() {
			al := a.alarm()
			al.Source = src
			a.sendAlarm(al)
			a.openAlarm()
		}
	}
	items := []menuItem{{label: T("al_last"), icon: "play", do: set("")}}
	if a.local != nil {
		for _, pl := range a.local.Playlists {
			if pl.Path != "" && !pl.Remote {
				items = append(items, menuItem{label: pl.Name, icon: "list", do: set(pl.Path)})
			}
		}
	}
	for _, f := range a.radio.favs {
		items = append(items, menuItem{label: f.Name, icon: "radio", do: set(f.Path())})
	}
	a.menu = &menu{title: T("al_source"), items: items, cols: 1}
}

// parseClock lee una hora "7:30", "07:30" o "730".
func parseClock(s string) (h, m int, ok bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ".", ":"))
	hs, ms, found := strings.Cut(s, ":")
	if !found {
		if len(s) < 3 || len(s) > 4 {
			return 0, 0, false
		}
		hs, ms = s[:len(s)-2], s[len(s)-2:]
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}
