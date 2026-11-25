package ui

// Los textos de varias carpetas de música (explorador desde Ajustes), en los
// veintiún idiomas.
func init() {
	for lang, v := range map[string][5]string{
		// h_add, h_remove, pick_hint_multi, pick_in_lib, pick_n
		"es":    {"AGREGAR", "QUITAR", "Entra en una carpeta y pulsa %s para agregarla o quitarla", "En la biblioteca", "%d carpetas"},
		"en":    {"ADD", "REMOVE", "Open a folder and press %s to add or remove it", "In the library", "%d folders"},
		"pt":    {"ADICIONAR", "REMOVER", "Abra uma pasta: %s adiciona ou remove", "Na biblioteca", "%d pastas"},
		"fr":    {"AJOUTER", "RETIRER", "Ouvrez un dossier : %s l'ajoute ou le retire", "Dans la bibliothèque", "%d dossiers"},
		"it":    {"AGGIUNGI", "TOGLI", "Apri una cartella: %s la aggiunge o la toglie", "Nella libreria", "%d cartelle"},
		"de":    {"HINZUFÜGEN", "ENTFERNEN", "Öffne einen Ordner: %s fügt ihn hinzu oder entfernt ihn", "In der Mediathek", "%d Ordner"},
		"nl":    {"TOEVOEGEN", "VERWIJDEREN", "Open een map: %s voegt hem toe of haalt hem weg", "In de bibliotheek", "%d mappen"},
		"sv":    {"LÄGG TILL", "TA BORT", "Öppna en mapp: %s lägger till eller tar bort den", "I biblioteket", "%d mappar"},
		"pl":    {"DODAJ", "USUŃ", "Otwórz folder: %s go dodaje lub usuwa", "W bibliotece", "Folderów: %d"},
		"tr":    {"EKLE", "ÇIKAR", "Bir klasör aç: %s ekler ya da çıkarır", "Kitaplıkta", "%d klasör"},
		"ru":    {"ДОБАВИТЬ", "УБРАТЬ", "Откройте папку: %s добавит или уберёт её", "В медиатеке", "Папок: %d"},
		"uk":    {"ДОДАТИ", "ПРИБРАТИ", "Відкрийте теку: %s додасть або прибере її", "У медіатеці", "Тек: %d"},
		"id":    {"TAMBAH", "HAPUS", "Buka folder: %s menambah atau menghapusnya", "Di pustaka", "%d folder"},
		"vi":    {"THÊM", "BỎ", "Mở thư mục: %s để thêm hoặc bỏ", "Trong thư viện", "%d thư mục"},
		"ja":    {"追加", "外す", "フォルダを開いて%sで追加・解除", "ライブラリ内", "%dフォルダ"},
		"zh":    {"添加", "移除", "打开文件夹，按 %s 添加或移除", "已在资料库中", "%d 个文件夹"},
		"zh-TW": {"加入", "移除", "打開資料夾，按 %s 加入或移除", "已在資料庫中", "%d 個資料夾"},
		"ko":    {"추가", "빼기", "폴더를 열고 %s 버튼으로 추가하거나 빼세요", "라이브러리에 있음", "폴더 %d개"},
		"th":    {"เพิ่ม", "เอาออก", "เปิดโฟลเดอร์แล้วกด %s เพื่อเพิ่มหรือเอาออก", "อยู่ในคลังเพลง", "%d โฟลเดอร์"},
		"hi":    {"जोड़ें", "हटाएँ", "फ़ोल्डर खोलें: %s से जोड़ें या हटाएँ", "लाइब्रेरी में", "%d फ़ोल्डर"},
		"ar":    {"إضافة", "إزالة", "افتح مجلدًا: %s للإضافة أو الإزالة", "في المكتبة", "%d مجلدات"},
	} {
		if strs[lang] != nil {
			for i, k := range []string{"h_add", "h_remove", "pick_hint_multi", "pick_in_lib", "pick_n"} {
				strs[lang][k] = v[i]
			}
		}
	}
}
