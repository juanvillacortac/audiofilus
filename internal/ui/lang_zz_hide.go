package ui

// Los textos de lo que se puede quitar de la vista (hide.go) y de los
// álbumes por carpeta, en los veintiún idiomas.
func init() {
	for lang, v := range map[string][10]string{
		// set_screens, screens_sub, set_tabs, tabs_sub, set_albums_folder,
		// set_formats, formats_sub, fmt_all, fmt_some, various
		"es":    {"Pantallas", "Las que salen en la barra de Select", "Secciones de la biblioteca", "Buscar siempre está", "Álbumes por carpeta", "Formatos por sección", "Los apagados no salen en esa sección", "Todos", "%d ocultos", "Varios artistas"},
		"en":    {"Screens", "The ones shown in the Select bar", "Library tabs", "Search is always there", "Albums by folder", "Formats per tab", "Formats turned off don't show in that tab", "All", "%d hidden", "Various artists"},
		"pt":    {"Telas", "As que aparecem na barra do Select", "Seções da biblioteca", "Buscar sempre aparece", "Álbuns por pasta", "Formatos por seção", "Os desligados não aparecem nessa seção", "Todos", "%d ocultos", "Vários artistas"},
		"fr":    {"Écrans", "Ceux affichés dans la barre de Select", "Onglets de la bibliothèque", "Rechercher est toujours là", "Albums par dossier", "Formats par onglet", "Les formats désactivés n'apparaissent pas dans cet onglet", "Tous", "%d masqués", "Artistes variés"},
		"it":    {"Schermate", "Quelle mostrate nella barra di Select", "Sezioni della libreria", "Cerca c'è sempre", "Album per cartella", "Formati per sezione", "I formati disattivati non compaiono in quella sezione", "Tutti", "%d nascosti", "Artisti vari"},
		"de":    {"Bildschirme", "Die in der Select-Leiste angezeigten", "Bibliotheks-Tabs", "Suche ist immer da", "Alben nach Ordner", "Formate pro Tab", "Ausgeschaltete Formate erscheinen in diesem Tab nicht", "Alle", "%d ausgeblendet", "Verschiedene Interpreten"},
		"nl":    {"Schermen", "Die in de Select-balk staan", "Bibliotheektabbladen", "Zoeken is er altijd", "Albums per map", "Formaten per tabblad", "Uitgeschakelde formaten verschijnen niet in dat tabblad", "Alle", "%d verborgen", "Diverse artiesten"},
		"sv":    {"Skärmar", "De som visas i Select-fältet", "Bibliotekets flikar", "Sök finns alltid", "Album per mapp", "Format per flik", "Avstängda format visas inte i den fliken", "Alla", "%d dolda", "Olika artister"},
		"pl":    {"Ekrany", "Te widoczne na pasku Select", "Sekcje biblioteki", "Szukaj jest zawsze", "Albumy według folderów", "Formaty w sekcjach", "Wyłączone formaty nie pojawiają się w tej sekcji", "Wszystkie", "Ukryte: %d", "Różni wykonawcy"},
		"tr":    {"Ekranlar", "Select çubuğunda görünenler", "Kitaplık sekmeleri", "Ara her zaman vardır", "Klasöre göre albümler", "Sekmeye göre biçimler", "Kapatılan biçimler o sekmede görünmez", "Tümü", "%d gizli", "Çeşitli sanatçılar"},
		"ru":    {"Экраны", "Те, что видны на панели Select", "Разделы библиотеки", "Поиск есть всегда", "Альбомы по папкам", "Форматы по разделам", "Выключенные форматы не показываются в этом разделе", "Все", "Скрыто: %d", "Разные исполнители"},
		"uk":    {"Екрани", "Ті, що видно на панелі Select", "Розділи бібліотеки", "Пошук є завжди", "Альбоми за папками", "Формати за розділами", "Вимкнені формати не показуються в цьому розділі", "Усі", "Приховано: %d", "Різні виконавці"},
		"id":    {"Layar", "Yang tampil di bilah Select", "Tab pustaka", "Cari selalu ada", "Album per folder", "Format per tab", "Format yang dimatikan tidak tampil di tab itu", "Semua", "%d disembunyikan", "Berbagai artis"},
		"vi":    {"Màn hình", "Những màn hình hiện trên thanh Select", "Các mục thư viện", "Tìm kiếm luôn có", "Album theo thư mục", "Định dạng theo mục", "Định dạng bị tắt sẽ không hiện trong mục đó", "Tất cả", "Ẩn %d", "Nhiều nghệ sĩ"},
		"ja":    {"画面", "Selectバーに表示する画面", "ライブラリのタブ", "検索は常に表示されます", "フォルダごとのアルバム", "タブごとの形式", "オフにした形式はそのタブに表示されません", "すべて", "%d個非表示", "さまざまなアーティスト"},
		"zh":    {"屏幕", "在 Select 栏中显示的屏幕", "媒体库标签", "搜索始终显示", "按文件夹分专辑", "各标签的格式", "关闭的格式不会在该标签中显示", "全部", "已隐藏 %d 个", "群星"},
		"zh-TW": {"畫面", "在 Select 列中顯示的畫面", "媒體庫分頁", "搜尋一律顯示", "依資料夾分專輯", "各分頁的格式", "關閉的格式不會在該分頁顯示", "全部", "已隱藏 %d 個", "群星"},
		"ko":    {"화면", "Select 바에 보이는 화면", "라이브러리 탭", "검색은 항상 있습니다", "폴더별 앨범", "탭별 형식", "끈 형식은 그 탭에 나오지 않습니다", "모두", "%d개 숨김", "여러 아티스트"},
		"th":    {"หน้าจอ", "หน้าจอที่แสดงในแถบ Select", "แท็บคลังเพลง", "ค้นหาจะแสดงเสมอ", "อัลบั้มตามโฟลเดอร์", "รูปแบบไฟล์ต่อแท็บ", "รูปแบบที่ปิดไว้จะไม่แสดงในแท็บนั้น", "ทั้งหมด", "ซ่อน %d", "ศิลปินหลายคน"},
		"hi":    {"स्क्रीन", "Select बार में दिखने वाली स्क्रीन", "लाइब्रेरी टैब", "खोज हमेशा रहती है", "फ़ोल्डर के अनुसार एल्बम", "हर टैब के फ़ॉर्मैट", "बंद किए गए फ़ॉर्मैट उस टैब में नहीं दिखते", "सभी", "%d छिपे", "विभिन्न कलाकार"},
		"ar":    {"الشاشات", "الشاشات الظاهرة في شريط Select", "أقسام المكتبة", "البحث موجود دائمًا", "الألبومات حسب المجلد", "الصيغ لكل قسم", "الصيغ المطفأة لا تظهر في ذلك القسم", "الكل", "%d مخفية", "فنانون متنوعون"},
	} {
		if strs[lang] == nil {
			continue
		}
		for i, k := range []string{"set_screens", "screens_sub", "set_tabs", "tabs_sub", "set_albums_folder",
			"set_formats", "formats_sub", "fmt_all", "fmt_some", "various"} {
			strs[lang][k] = v[i]
		}
	}
}
