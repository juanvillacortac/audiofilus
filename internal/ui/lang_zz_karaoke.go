package ui

// El texto del karaoke de las letras (#11), en los veintiún idiomas.
func init() {
	for lang, v := range map[string]string{
		"es":    "Letras karaoke",
		"en":    "Karaoke lyrics",
		"pt":    "Letras em karaokê",
		"fr":    "Paroles karaoké",
		"it":    "Testi karaoke",
		"de":    "Karaoke-Liedtexte",
		"nl":    "Karaoketeksten",
		"sv":    "Karaoketexter",
		"pl":    "Teksty karaoke",
		"tr":    "Karaoke sözler",
		"ru":    "Караоке в текстах",
		"uk":    "Караоке в текстах",
		"id":    "Lirik karaoke",
		"vi":    "Lời bài hát karaoke",
		"ja":    "カラオケ表示",
		"zh":    "卡拉OK歌词",
		"zh-TW": "卡拉OK歌詞",
		"ko":    "노래방 가사",
		"th":    "เนื้อเพลงคาราโอเกะ",
		"hi":    "कराओके बोल",
		"ar":    "كلمات الكاريوكي",
	} {
		if strs[lang] != nil {
			strs[lang]["set_lyr_karaoke"] = v
		}
	}
}
