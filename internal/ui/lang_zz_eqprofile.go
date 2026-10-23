package ui

// El nombre de "Perfil de EQ" en el menú de Start (eqprofiles.go), en los
// trece idiomas.
func init() {
	for lang, v := range map[string]string{
		"es": "Perfil de EQ", "en": "EQ profile", "tr": "EQ profili", "de": "EQ-Profil",
		"fr": "Profil d'EQ", "it": "Profilo EQ", "pt": "Perfil de EQ", "pl": "Profil EQ",
		"ru": "Профиль EQ", "id": "Profil EQ", "ja": "EQプロファイル", "zh": "EQ 配置", "ko": "EQ 프로필",
	} {
		strs[lang]["eqp_title"] = v
	}
}
