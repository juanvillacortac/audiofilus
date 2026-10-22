package ui

// Los textos de la biblioteca que no estaban traducidos: el nombre del
// "disco" de las canciones sin álbum (library.go), en los trece idiomas.
func init() {
	for lang, v := range map[string]string{
		"es": "Sin álbum",
		"en": "No album",
		"tr": "Albümsüz",
		"de": "Kein Album",
		"fr": "Sans album",
		"it": "Senza album",
		"pt": "Sem álbum",
		"pl": "Bez albumu",
		"ru": "Без альбома",
		"id": "Tanpa album",
		"ja": "アルバムなし",
		"zh": "无专辑",
		"ko": "앨범 없음",
	} {
		strs[lang]["no_album"] = v
	}
}
