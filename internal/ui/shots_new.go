package ui

import (
	"fmt"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/autoeq"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// shotsNew son las capturas de los audiolibros, la alarma, AutoEQ y el color
// fijo (writeShots), con datos inventados: la maqueta no tiene libros, red
// ni servicio. Deja todo como estaba.
func (a *App) shotsNew(shot func(string) error) error {
	restore := a.shotBook()
	a.screen = NowPlaying
	if err := shot("audiolibro"); err != nil {
		return err
	}
	a.openNowMenu()
	if err := shot("audiolibro-menu"); err != nil {
		return err
	}
	a.openChapters()
	if err := shot("audiolibro-capitulos"); err != nil {
		return err
	}
	a.menu = nil
	restore()

	// La alarma, entre semana a las 7:30, con una emisora favorita.
	alarm := a.status.Alarm
	a.status.Alarm = daemon.Alarm{On: true, Hour: 7, Min: 30, Days: []int{1, 2, 3, 4, 5}}
	a.screen = Settings
	a.openAlarm()
	if err := shot("alarma"); err != nil {
		return err
	}
	a.menu, a.status.Alarm = nil, alarm

	// AutoEQ: lo que sale al buscar "HD 650" y su perfil ya puesto.
	a.autoEQResults("HD 650", []autoeq.Profile{
		{Name: "Sennheiser HD 650", Source: "oratory1990"},
		{Name: "Sennheiser HD 650", Source: "crinacle on GRAS 43AG-7"},
		{Name: "Sennheiser HD 650", Source: "Rtings"},
		{Name: "Sennheiser HD 650 (2020)", Source: "Kuulokenurkka"},
	}, nil)
	if err := shot("autoeq"); err != nil {
		return err
	}
	a.menu = nil
	bands, pre, err := autoeq.ParseParametric(shotHD650)
	if err != nil {
		return fmt.Errorf("shots: %w", err)
	}
	a.peqEdit = &peqEditor{bands: bands, preamp: pre, row: 1}
	if err := shot("autoeq-aplicado"); err != nil {
		return err
	}
	a.peqEdit = nil

	// Ajustes con un color fijo (el morado de la RG34XX-SP), en su baldosa.
	theme := a.theme
	a.theme, _ = fixedTheme("purple")
	a.cfg.UI.ThemeColor = "purple"
	if tiles, _, _ := settingsLayout(a.w, a.sections()); len(tiles) > 0 {
		for i, t := range tiles {
			if t.tile.id == setThemeColor {
				a.settingsSel = i
			}
		}
	}
	// El desplazamiento de Ajustes llega a la baldosa poco a poco: unos
	// fotogramas sin guardar, para que se vea.
	tmp := ebiten.NewImage(a.w, a.h)
	for range 30 {
		a.drawScreen(tmp, Settings)
	}
	tmp.Deallocate()
	err = shot("color")
	a.theme, a.cfg.UI.ThemeColor, a.settingsSel, a.settingsScroll = theme, "", 0, 0
	a.screen = NowPlaying
	return err
}

// shotHD650 es un perfil de AutoEq (el de oratory1990 para unos HD 650).
const shotHD650 = `Preamp: -6.1 dB
Filter 1: ON LSC Fc 105 Hz Gain 6.4 dB Q 0.70
Filter 2: ON PK Fc 8800 Hz Gain 5.1 dB Q 1.42
Filter 3: ON PK Fc 118 Hz Gain -3.1 dB Q 0.50
Filter 4: ON PK Fc 37 Hz Gain 0.7 dB Q 3.96
Filter 5: ON PK Fc 3169 Hz Gain -1.7 dB Q 3.89
Filter 6: ON HSC Fc 10000 Hz Gain -2.1 dB Q 0.70
Filter 7: ON PK Fc 1227 Hz Gain -1.2 dB Q 2.53
Filter 8: ON PK Fc 2055 Hz Gain 1.2 dB Q 3.23
Filter 9: ON PK Fc 587 Hz Gain 0.4 dB Q 1.19
Filter 10: ON PK Fc 5332 Hz Gain -1.1 dB Q 5.75
`

// shotBook pone a "sonar" un audiolibro inventado (once horas y media, en
// el capítulo 5, a 1,25×, sin portada) para las capturas y la secuencia;
// devuelve lo que lo deja todo como estaba.
func (a *App) shotBook() func() {
	al := a.playing
	song, pos, dur, speed := a.playingSong, a.position, a.duration, a.cfg.UI.BookSpeed
	a.playing = nil
	book := filepath.Join(string(filepath.Separator)+"shots", "Audiobooks", "The Lighthouse Keeper.m4b")
	a.playingSong = Song{Path: book, Title: "The Lighthouse Keeper", Artist: "Mara Quill", Album: "The Lighthouse Keeper"}
	names := []string{"Opening Credits", "The Storm", "A Letter from the Mainland", "The Lamp Room", "Fog", "The Keeper's Log",
		"Low Tide", "The Visitor", "Signals", "Winter", "The Wreck", "What the Sea Kept", "Spring Again", "Epilogue", "End Credits"}
	a.books.chapters, a.books.chFor = nil, book
	for i, n := range names {
		a.books.chapters = append(a.books.chapters, decode.Chapter{Start: float64(i) * 2768, Title: n})
	}
	a.position, a.duration, a.cfg.UI.BookSpeed = 4*2768+1312, 41520, 1.25
	a.shownPos = a.position
	return func() {
		a.playing, a.playingSong, a.position, a.duration, a.cfg.UI.BookSpeed = al, song, pos, dur, speed
		a.books.chapters, a.books.chFor = nil, ""
	}
}
