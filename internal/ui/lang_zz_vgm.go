package ui

// Los textos de la emulación precisa de los VGM (Ajustes > Sonido), en los
// veintiún idiomas.
func init() {
	for lang, v := range map[string][2]string{
		// set_vgm_acc, vgm_acc_on
		"es":    {"VGM precisos (Nuked)", "Sí · más CPU"},
		"en":    {"Accurate VGM (Nuked)", "On · more CPU"},
		"pt":    {"VGM precisos (Nuked)", "Sim · mais CPU"},
		"fr":    {"VGM précis (Nuked)", "Oui · plus de CPU"},
		"it":    {"VGM precisi (Nuked)", "Sì · più CPU"},
		"de":    {"Genaue VGM (Nuked)", "An · mehr CPU"},
		"nl":    {"Nauwkeurige VGM (Nuked)", "Aan · meer CPU"},
		"sv":    {"Exakta VGM (Nuked)", "På · mer CPU"},
		"pl":    {"Dokładne VGM (Nuked)", "Tak · więcej CPU"},
		"tr":    {"Hassas VGM (Nuked)", "Açık · daha çok CPU"},
		"ru":    {"Точные VGM (Nuked)", "Вкл · больше CPU"},
		"uk":    {"Точні VGM (Nuked)", "Увімк · більше CPU"},
		"id":    {"VGM akurat (Nuked)", "Aktif · CPU lebih banyak"},
		"vi":    {"VGM chính xác (Nuked)", "Bật · tốn CPU hơn"},
		"ja":    {"正確なVGM (Nuked)", "オン・CPU多め"},
		"zh":    {"精确 VGM (Nuked)", "开 · 更耗 CPU"},
		"zh-TW": {"精確 VGM (Nuked)", "開 · 更耗 CPU"},
		"ko":    {"정확한 VGM (Nuked)", "켬 · CPU 더 사용"},
		"th":    {"VGM แม่นยำ (Nuked)", "เปิด · ใช้ CPU มากขึ้น"},
		"hi":    {"सटीक VGM (Nuked)", "चालू · ज़्यादा CPU"},
		"ar":    {"VGM دقيق (Nuked)", "مفعّل · معالج أكثر"},
	} {
		if strs[lang] != nil {
			strs[lang]["set_vgm_acc"], strs[lang]["vgm_acc_on"] = v[0], v[1]
		}
	}
}
