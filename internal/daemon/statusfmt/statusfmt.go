// Package statusfmt pinta el estado del daemon para la terminal.
package statusfmt

import (
	"fmt"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Format pinta el estado en unas pocas líneas legibles.
func Format(es engine.Status, sleep, awake bool, plat, pad string) string {
	var b strings.Builder
	title := es.Track.Title
	if title == "" {
		title = "-"
	}
	fmt.Fprintf(&b, "%s  %s", stateIcon(es.State), title)
	if es.Track.Artist != "" {
		fmt.Fprintf(&b, " — %s", es.Track.Artist)
	}
	if es.Track.Album != "" {
		fmt.Fprintf(&b, " (%s)", es.Track.Album)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "   %s / %s   track %d/%d   shuffle %s   repeat %s   vol %.0f%%\n",
		clock(es.Position), clock(es.Duration), es.Index+1, es.Count, onOff(es.Shuffle), es.Repeat, es.Volume*100)
	if plat != "" {
		fmt.Fprintf(&b, "   platform %s   sleep %s   awake %s\n", plat, onOff(sleep), onOff(awake))
	}
	if pad != "" {
		fmt.Fprintf(&b, "   pad %s\n", pad)
	}
	return b.String()
}

// stateIcon es el símbolo de cada estado.
func stateIcon(s string) string {
	switch s {
	case "playing":
		return "▶"
	case "paused":
		return "⏸"
	}
	return "■"
}

// clock formatea segundos como m:ss.
func clock(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	t := int(sec)
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// onOff escribe un booleano como on/off.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
