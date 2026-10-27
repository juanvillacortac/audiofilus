package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// flexoki es el colors.toml de un tema oscuro de Omarchy (Flexoki Dark).
const flexoki = `accent = "#4385BE"
cursor = "#CECDC3"
foreground = "#CECDC3"
background = "#100F0F"
selection_background = "#403E3C"

color0 = "#100F0F"
color8 = "#6F6E69"
color9 = "#D14D41"
color10 = "#879A39"
color11 = "#D0A215"
color12 = "#4385BE"
color13 = "#CE5D97"
color14 = "#3AA99F"
`

// withOmarchy deja un tema de Omarchy en un HOME de prueba (o ninguno, con
// raw vacío) y olvida el que se había leído.
func withOmarchy(t *testing.T, raw string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir en Windows
	sysCache.checked, sysCache.raw, sysCache.ok = sysCache.checked.AddDate(-1, 0, 0), nil, false
	t.Cleanup(func() { sysCache.checked, sysCache.raw, sysCache.ok = sysCache.checked.AddDate(-1, 0, 0), nil, false })
	if raw == "" {
		return
	}
	dir := filepath.Join(home, ".local/state/omarchy/current/theme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "colors.toml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Un tema oscuro de Omarchy da toda la interfaz: su fondo, su texto, su acento
// y, de segundo tono, el color vivo más lejano al acento.
func TestOmarchyDarkTheme(t *testing.T) {
	th, ok := omarchyTheme(parseColors([]byte(flexoki)))
	if !ok {
		t.Fatal("no theme from a valid colors.toml")
	}
	rgb := func(n uint32) color.RGBA { return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 0xff} }
	if th.Bg != rgb(0x100f0f) || th.Text != rgb(0xcecdc3) || th.Accent != rgb(0x4385be) {
		t.Errorf("theme = %+v, want Flexoki's background, foreground and accent", th)
	}
	if th.Accent2 == th.Accent || th.Accent2 == rgb(0x3aa99f) {
		t.Errorf("second tone %v is too close to the blue accent", th.Accent2)
	}
}

// Un tema claro también da toda la interfaz: su fondo claro, su texto oscuro
// y una sombra apenas más oscura que el fondo (no negra), para que el texto
// se lea en las barras.
func TestOmarchyLightTheme(t *testing.T) {
	th, ok := omarchyTheme(parseColors([]byte(`accent = "#DE6A41"
foreground = "#242424"
background = "#d0d0c8"
color9 = "#db0030"
`)))
	if !ok {
		t.Fatal("no theme from a light colors.toml")
	}
	if th.Bg != (color.RGBA{0xd0, 0xd0, 0xc8, 0xff}) || th.Text != (color.RGBA{0x24, 0x24, 0x24, 0xff}) {
		t.Errorf("light theme gave background %v and text %v, want the theme's", th.Bg, th.Text)
	}
	if luma(th.Shadow) < 0.5 {
		t.Errorf("light theme shadow %v is dark, want a light one", th.Shadow)
	}
}

// Sistema solo se ofrece en Omarchy, detrás de Del disco; fuera, elegido en
// otra máquina, se vuelve al color del disco.
func TestSystemChoiceOnlyOnOmarchy(t *testing.T) {
	withOmarchy(t, "")
	if cs := themeChoices(); len(cs) != len(themeColors) {
		t.Errorf("without Omarchy there are %d choices, want %d", len(cs), len(themeColors))
	}
	if _, ok := fixedTheme("system"); ok {
		t.Error("system theme without Omarchy")
	}

	withOmarchy(t, flexoki)
	cs := themeChoices()
	if len(cs) != len(themeColors)+1 || cs[0].name != "album" || cs[1].name != "system" {
		t.Errorf("choices on Omarchy start with %q, %q; want album, system", cs[0].name, cs[1].name)
	}
	if th, ok := fixedTheme("system"); !ok || th.Bg != (color.RGBA{0x10, 0x0f, 0x0f, 0xff}) {
		t.Errorf("system theme = %+v, %v; want Flexoki's", th, ok)
	}
}

// Sin color elegido, en Omarchy se usa el del sistema y fuera el del disco;
// "album" elegido a propósito se respeta, y las capturas no dependen de la
// máquina.
func TestSystemIsDefaultOnOmarchy(t *testing.T) {
	a := &App{}
	withOmarchy(t, "")
	if c := a.themeColor(); c != "album" {
		t.Errorf("without Omarchy the default color is %q, want album", c)
	}
	withOmarchy(t, flexoki)
	if c := a.themeColor(); c != "system" {
		t.Errorf("on Omarchy the default color is %q, want system", c)
	}
	a.cfg.UI.ThemeColor = "album"
	if c := a.themeColor(); c != "album" {
		t.Errorf("chosen album became %q", c)
	}
	a.cfg.UI.ThemeColor, a.offline = "", true
	if c := a.themeColor(); c != "album" {
		t.Errorf("screenshots use %q, want album", c)
	}
}
