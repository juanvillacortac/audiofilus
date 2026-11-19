package esdata

import (
	"encoding/binary"
	"errors"
	"os"
	"path"
	"strings"
)

// Las traducciones de EmulationStation: los nombres de las opciones de
// es_features.cfg están en inglés y ES los traduce con su catálogo de
// gettext (emulationstation2.mo). Aquí se lee el del idioma de Audiofilus,
// así las opciones salen en ese idioma aunque Knulli esté en otro.

// Catalog son las traducciones de un idioma: el texto en inglés → el suyo.
type Catalog map[string]string

// Tr es s traducido (o s si no hay traducción).
func (c Catalog) Tr(s string) string {
	if t, ok := c[s]; ok && t != "" {
		return t
	}
	return s
}

// localeDirs son las carpetas de locale de cada idioma de Audiofilus, en
// orden de preferencia (Knulli trae unas con país y otras sin).
var localeDirs = map[string][]string{
	"es":    {"es", "es_ES", "es_MX"},
	"pt":    {"pt_BR", "pt", "pt_PT"},
	"fr":    {"fr", "fr_FR"},
	"it":    {"it", "it_IT"},
	"de":    {"de", "de_DE"},
	"pl":    {"pl", "pl_PL"},
	"tr":    {"tr", "tr_TR"},
	"ru":    {"ru", "ru_RU"},
	"id":    {"id_ID", "id"},
	"ja":    {"ja", "ja_JP"},
	"zh":    {"zh_CN", "zh", "zh_TW"},
	"zh-TW": {"zh_TW", "zh"},
	"ko":    {"ko", "ko_KR"},
	"vi":    {"vi", "vi_VN"},
	"th":    {"th", "th_TH"},
	"nl":    {"nl", "nl_NL"},
	"sv":    {"sv", "sv_SE"},
	"uk":    {"uk", "uk_UA"},
	"hi":    {"hi", "hi_IN"},
	"ar":    {"ar", "ar_SA"},
}

// LoadCatalog lee el catálogo de ES del idioma lang ("es", "pt"...): nil
// en inglés o si no hay.
func (r Root) LoadCatalog(lang string) Catalog {
	for _, d := range localeDirs[lang] {
		b, err := os.ReadFile(r.Path(path.Join("/usr/share/locale", d, "LC_MESSAGES", "emulationstation2.mo")))
		if err != nil {
			continue
		}
		if c, err := ParseMO(b); err == nil {
			return c
		}
	}
	return nil
}

// ParseMO lee un catálogo .mo de gettext. De los textos con contexto
// (msgctxt) se guarda solo el texto, y de los plurales, la primera forma.
func ParseMO(b []byte) (Catalog, error) {
	if len(b) < 28 {
		return nil, errors.New("mo file too short")
	}
	var bo binary.ByteOrder
	switch binary.LittleEndian.Uint32(b) {
	case 0x950412de:
		bo = binary.LittleEndian
	case 0xde120495:
		bo = binary.BigEndian
	default:
		return nil, errors.New("not a mo file")
	}
	n := int(bo.Uint32(b[8:]))
	origs, trans := int(bo.Uint32(b[12:])), int(bo.Uint32(b[16:]))
	str := func(table, i int) (string, bool) {
		at := table + 8*i
		if at+8 > len(b) {
			return "", false
		}
		l, off := int(bo.Uint32(b[at:])), int(bo.Uint32(b[at+4:]))
		if off < 0 || l < 0 || off+l > len(b) {
			return "", false
		}
		return string(b[off : off+l]), true
	}
	c := Catalog{}
	for i := 0; i < n; i++ {
		id, ok1 := str(origs, i)
		tr, ok2 := str(trans, i)
		if !ok1 || !ok2 {
			return nil, errors.New("mo file out of range")
		}
		if _, after, ok := strings.Cut(id, "\x04"); ok {
			id = after // el contexto no hace falta aquí
		}
		id, _, _ = strings.Cut(id, "\x00")
		tr, _, _ = strings.Cut(tr, "\x00")
		if id != "" {
			c[id] = tr
		}
	}
	return c, nil
}
