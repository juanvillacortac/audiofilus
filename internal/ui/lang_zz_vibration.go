package ui

// Los textos de la vibración de las portátiles (Ajustes → Sistema), en los
// trece idiomas.
func init() {
	for lang, m := range map[string]map[string]string{
		"es": {"set_vibration": "Vibración", "vib_off": "No", "vib_low": "Baja", "vib_mid": "Media", "vib_high": "Alta"},
		"en": {"set_vibration": "Vibration", "vib_off": "Off", "vib_low": "Low", "vib_mid": "Medium", "vib_high": "High"},
		"tr": {"set_vibration": "Titreşim", "vib_off": "Kapalı", "vib_low": "Düşük", "vib_mid": "Orta", "vib_high": "Yüksek"},
		"de": {"set_vibration": "Vibration", "vib_off": "Aus", "vib_low": "Schwach", "vib_mid": "Mittel", "vib_high": "Stark"},
		"fr": {"set_vibration": "Vibration", "vib_off": "Non", "vib_low": "Faible", "vib_mid": "Moyenne", "vib_high": "Forte"},
		"it": {"set_vibration": "Vibrazione", "vib_off": "No", "vib_low": "Bassa", "vib_mid": "Media", "vib_high": "Alta"},
		"pt": {"set_vibration": "Vibração", "vib_off": "Não", "vib_low": "Baixa", "vib_mid": "Média", "vib_high": "Alta"},
		"pl": {"set_vibration": "Wibracje", "vib_off": "Wył.", "vib_low": "Słabe", "vib_mid": "Średnie", "vib_high": "Mocne"},
		"ru": {"set_vibration": "Вибрация", "vib_off": "Выкл", "vib_low": "Слабая", "vib_mid": "Средняя", "vib_high": "Сильная"},
		"id": {"set_vibration": "Getaran", "vib_off": "Mati", "vib_low": "Lemah", "vib_mid": "Sedang", "vib_high": "Kuat"},
		"ja": {"set_vibration": "振動", "vib_off": "オフ", "vib_low": "弱", "vib_mid": "中", "vib_high": "強"},
		"zh": {"set_vibration": "振动", "vib_off": "关", "vib_low": "弱", "vib_mid": "中", "vib_high": "强"},
		"ko": {"set_vibration": "진동", "vib_off": "끔", "vib_low": "약하게", "vib_mid": "보통", "vib_high": "강하게"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
