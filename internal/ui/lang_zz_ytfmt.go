package ui

// Los textos del formato al descargar de YouTube Music (ytmusic.go): el
// .m4a tal cual o convertido a MP3, en los trece idiomas.
func init() {
	for lang, m := range map[string]map[string]string{
		"es": {"ytm_fmt_keep": "Original (M4A)", "ytm_fmt_mp3": "Convertir a MP3", "ytm_fmt_quality": "Sin perder calidad", "ytm_fmt_recommended": "Recomendado en esta consola", "ytm_fmt_memory": "Usa más memoria y tarda más"},
		"en": {"ytm_fmt_keep": "Original (M4A)", "ytm_fmt_mp3": "Convert to MP3", "ytm_fmt_quality": "No quality loss", "ytm_fmt_recommended": "Recommended on this console", "ytm_fmt_memory": "Uses more memory and takes longer"},
		"tr": {"ytm_fmt_keep": "Orijinal (M4A)", "ytm_fmt_mp3": "MP3'e dönüştür", "ytm_fmt_quality": "Kalite kaybı yok", "ytm_fmt_recommended": "Bu konsolda önerilir", "ytm_fmt_memory": "Daha fazla bellek kullanır, daha uzun sürer"},
		"de": {"ytm_fmt_keep": "Original (M4A)", "ytm_fmt_mp3": "In MP3 umwandeln", "ytm_fmt_quality": "Ohne Qualitätsverlust", "ytm_fmt_recommended": "Auf dieser Konsole empfohlen", "ytm_fmt_memory": "Braucht mehr Speicher und dauert länger"},
		"fr": {"ytm_fmt_keep": "Original (M4A)", "ytm_fmt_mp3": "Convertir en MP3", "ytm_fmt_quality": "Sans perte de qualité", "ytm_fmt_recommended": "Recommandé sur cette console", "ytm_fmt_memory": "Utilise plus de mémoire et prend plus de temps"},
		"it": {"ytm_fmt_keep": "Originale (M4A)", "ytm_fmt_mp3": "Converti in MP3", "ytm_fmt_quality": "Senza perdita di qualità", "ytm_fmt_recommended": "Consigliato su questa console", "ytm_fmt_memory": "Usa più memoria e richiede più tempo"},
		"pt": {"ytm_fmt_keep": "Original (M4A)", "ytm_fmt_mp3": "Converter para MP3", "ytm_fmt_quality": "Sem perder qualidade", "ytm_fmt_recommended": "Recomendado neste console", "ytm_fmt_memory": "Usa mais memória e demora mais"},
		"pl": {"ytm_fmt_keep": "Oryginał (M4A)", "ytm_fmt_mp3": "Konwertuj do MP3", "ytm_fmt_quality": "Bez utraty jakości", "ytm_fmt_recommended": "Zalecane na tej konsoli", "ytm_fmt_memory": "Zużywa więcej pamięci i trwa dłużej"},
		"ru": {"ytm_fmt_keep": "Оригинал (M4A)", "ytm_fmt_mp3": "Конвертировать в MP3", "ytm_fmt_quality": "Без потери качества", "ytm_fmt_recommended": "Рекомендуется на этой консоли", "ytm_fmt_memory": "Требует больше памяти и времени"},
		"id": {"ytm_fmt_keep": "Asli (M4A)", "ytm_fmt_mp3": "Ubah ke MP3", "ytm_fmt_quality": "Tanpa kehilangan kualitas", "ytm_fmt_recommended": "Disarankan di konsol ini", "ytm_fmt_memory": "Memakai lebih banyak memori dan lebih lama"},
		"ja": {"ytm_fmt_keep": "オリジナル (M4A)", "ytm_fmt_mp3": "MP3に変換", "ytm_fmt_quality": "音質を落とさない", "ytm_fmt_recommended": "この機種ではおすすめ", "ytm_fmt_memory": "メモリを多く使い、時間もかかる"},
		"zh": {"ytm_fmt_keep": "原始格式 (M4A)", "ytm_fmt_mp3": "转换为 MP3", "ytm_fmt_quality": "不损失音质", "ytm_fmt_recommended": "本机推荐", "ytm_fmt_memory": "占用更多内存，耗时更长"},
		"ko": {"ytm_fmt_keep": "원본 (M4A)", "ytm_fmt_mp3": "MP3로 변환", "ytm_fmt_quality": "음질 손실 없음", "ytm_fmt_recommended": "이 기기에서 권장", "ytm_fmt_memory": "메모리를 더 쓰고 더 오래 걸림"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
