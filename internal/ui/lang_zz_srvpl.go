package ui

// Los textos de las listas de los servidores (serverplaylists.go). Los demás
// ("Renombrar", "Borrar lista", "Subir"...) son los de las listas de aquí.
// El archivo se llama lang_zz_* para que su init vaya después de los de cada
// idioma (lang_zh.go crea strs["zh"]): Go los corre en orden de nombre.
func init() {
	for lang, m := range map[string]map[string]string{
		"es": {"spl_new": "Nueva lista en %s"},
		"en": {"spl_new": "New playlist on %s"},
		"tr": {"spl_new": "%s üzerinde yeni liste"},
		"de": {"spl_new": "Neue Playlist auf %s"},
		"fr": {"spl_new": "Nouvelle playlist sur %s"},
		"it": {"spl_new": "Nuova playlist su %s"},
		"pt": {"spl_new": "Nova playlist em %s"},
		"pl": {"spl_new": "Nowa playlista na %s"},
		"ru": {"spl_new": "Новый плейлист на %s"},
		"id": {"spl_new": "Playlist baru di %s"},
		"ja": {"spl_new": "%s に新しいリスト"},
		"zh": {"spl_new": "在 %s 新建歌单"},
		"ko": {"spl_new": "%s에 새 재생목록"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
