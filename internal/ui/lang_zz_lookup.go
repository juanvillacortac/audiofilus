package ui

// Los textos de buscar más de un artista o un álbum en YouTube Music
// (ytmusic.go) y de buscar los datos de una canción en internet
// (songlookup.go), en los veintiún idiomas.
func init() {
	for lang, v := range map[string][4]string{
		// ytm_more_artist, ytm_more_album, tg_lookup, tg_lookup_none
		"es":    {"Más del artista en YouTube Music", "El álbum en YouTube Music", "Buscar datos de la canción", "No se encontró esta canción"},
		"en":    {"More by the artist on YouTube Music", "This album on YouTube Music", "Look up song details", "Couldn't find this song"},
		"pt":    {"Mais do artista no YouTube Music", "O álbum no YouTube Music", "Buscar dados da música", "Música não encontrada"},
		"fr":    {"Plus de l'artiste sur YouTube Music", "L'album sur YouTube Music", "Chercher les infos du titre", "Titre introuvable"},
		"it":    {"Altro dell'artista su YouTube Music", "L'album su YouTube Music", "Cerca i dati del brano", "Brano non trovato"},
		"de":    {"Mehr vom Künstler auf YouTube Music", "Das Album auf YouTube Music", "Songdaten suchen", "Song nicht gefunden"},
		"nl":    {"Meer van de artiest op YouTube Music", "Het album op YouTube Music", "Nummergegevens zoeken", "Nummer niet gevonden"},
		"sv":    {"Mer av artisten på YouTube Music", "Albumet på YouTube Music", "Sök låtens uppgifter", "Hittade inte låten"},
		"pl":    {"Więcej wykonawcy w YouTube Music", "Album w YouTube Music", "Szukaj danych utworu", "Nie znaleziono utworu"},
		"tr":    {"Sanatçının YouTube Music'teki diğerleri", "Albüm YouTube Music'te", "Şarkı bilgilerini ara", "Şarkı bulunamadı"},
		"ru":    {"Ещё от артиста в YouTube Music", "Альбом в YouTube Music", "Найти данные песни", "Песня не найдена"},
		"uk":    {"Більше від виконавця в YouTube Music", "Альбом у YouTube Music", "Знайти дані пісні", "Пісню не знайдено"},
		"id":    {"Lagu lain artis di YouTube Music", "Album ini di YouTube Music", "Cari info lagu", "Lagu tidak ditemukan"},
		"vi":    {"Thêm của nghệ sĩ trên YouTube Music", "Album này trên YouTube Music", "Tìm thông tin bài hát", "Không tìm thấy bài này"},
		"ja":    {"このアーティストをYouTube Musicで", "このアルバムをYouTube Musicで", "曲の情報を調べる", "曲が見つかりません"},
		"zh":    {"在 YouTube Music 中找更多该艺人", "在 YouTube Music 中找这张专辑", "查找歌曲信息", "找不到这首歌"},
		"zh-TW": {"在 YouTube Music 找更多此歌手", "在 YouTube Music 找這張專輯", "查詢歌曲資料", "找不到這首歌"},
		"ko":    {"YouTube Music에서 아티스트 더 보기", "YouTube Music에서 이 앨범", "곡 정보 찾기", "곡을 찾을 수 없음"},
		"th":    {"เพลงอื่นของศิลปินใน YouTube Music", "อัลบั้มนี้ใน YouTube Music", "ค้นหาข้อมูลเพลง", "ไม่พบเพลงนี้"},
		"hi":    {"YouTube Music पर कलाकार के और गाने", "YouTube Music पर यह ऐल्बम", "गाने की जानकारी खोजें", "यह गाना नहीं मिला"},
		"ar":    {"المزيد للفنان على YouTube Music", "هذا الألبوم على YouTube Music", "البحث عن بيانات الأغنية", "لم يتم العثور على الأغنية"},
	} {
		if strs[lang] == nil {
			continue
		}
		strs[lang]["ytm_more_artist"] = v[0]
		strs[lang]["ytm_more_album"] = v[1]
		strs[lang]["tg_lookup"] = v[2]
		strs[lang]["tg_lookup_none"] = v[3]
	}
}
