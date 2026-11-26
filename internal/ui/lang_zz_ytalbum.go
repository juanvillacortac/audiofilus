package ui

// Los textos de los álbumes de YouTube Music en Buscar (ytmusic.go), en los
// veintiún idiomas.
func init() {
	for lang, v := range map[string][2]string{
		// ytm_album, ytm_download_all
		"es":    {"Álbum", "Descargar todo"},
		"en":    {"Album", "Download all"},
		"pt":    {"Álbum", "Baixar tudo"},
		"fr":    {"Album", "Tout télécharger"},
		"it":    {"Album", "Scarica tutto"},
		"de":    {"Album", "Alle herunterladen"},
		"nl":    {"Album", "Alles downloaden"},
		"sv":    {"Album", "Ladda ner alla"},
		"pl":    {"Album", "Pobierz wszystko"},
		"tr":    {"Albüm", "Tümünü indir"},
		"ru":    {"Альбом", "Скачать всё"},
		"uk":    {"Альбом", "Завантажити все"},
		"id":    {"Album", "Unduh semua"},
		"vi":    {"Album", "Tải tất cả"},
		"ja":    {"アルバム", "すべてダウンロード"},
		"zh":    {"专辑", "全部下载"},
		"zh-TW": {"專輯", "全部下載"},
		"ko":    {"앨범", "모두 다운로드"},
		"th":    {"อัลบั้ม", "ดาวน์โหลดทั้งหมด"},
		"hi":    {"एल्बम", "सब डाउनलोड करें"},
		"ar":    {"ألبوم", "تنزيل الكل"},
	} {
		if strs[lang] != nil {
			strs[lang]["ytm_album"], strs[lang]["ytm_download_all"] = v[0], v[1]
		}
	}
}
