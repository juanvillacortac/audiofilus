package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// La próxima vez que suena: hoy si aún no pasó la hora, si no mañana; con
// días, el siguiente de ellos; apagada, nunca.
func TestAlarmNext(t *testing.T) {
	loc := time.Local
	wed := time.Date(2026, 10, 21, 8, 0, 0, 0, loc) // miércoles 8:00
	cases := []struct {
		a    Alarm
		want time.Time
	}{
		{Alarm{On: true, Hour: 9, Min: 30}, time.Date(2026, 10, 21, 9, 30, 0, 0, loc)},
		{Alarm{On: true, Hour: 7}, time.Date(2026, 10, 22, 7, 0, 0, 0, loc)},
		{Alarm{On: true, Hour: 7, Days: []int{0, 6}}, time.Date(2026, 10, 24, 7, 0, 0, 0, loc)}, // el sábado
		{Alarm{On: true, Hour: 7, Days: []int{1}, Once: true}, time.Date(2026, 10, 22, 7, 0, 0, 0, loc)},
		{Alarm{Hour: 9}, time.Time{}},
	}
	for _, c := range cases {
		if got := c.a.next(wed); !got.Equal(c.want) {
			t.Errorf("%+v: next %v, want %v", c.a, got, c.want)
		}
	}
}

// "alarm set" la guarda y calcula la próxima; "alarm off" la apaga; al
// arrancar se lee la guardada.
func TestAlarmCommand(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{paths: config.Paths{Data: dir}, plat: platform.Detect(platform.Options{})}
	if _, err := d.alarmCommand([]string{"set", `{"on":true,"hour":6,"min":45}`}); err != nil {
		t.Fatal(err)
	}
	if d.alarm.Next.IsZero() || d.alarm.Next.Hour() != 6 || d.alarm.Next.Minute() != 45 {
		t.Errorf("next %v, want 6:45", d.alarm.Next)
	}
	if _, err := os.Stat(filepath.Join(dir, "alarm.json")); err != nil {
		t.Errorf("not saved: %v", err)
	}
	d2 := &Daemon{paths: config.Paths{Data: dir}, plat: d.plat}
	d2.loadAlarm()
	if !d2.alarm.On || d2.alarm.Hour != 6 {
		t.Errorf("loaded %+v", d2.alarm)
	}
	if _, err := d.alarmCommand([]string{"off"}); err != nil || d.alarm.On || !d.alarm.Next.IsZero() {
		t.Errorf("off: %+v %v", d.alarm, err)
	}
}
