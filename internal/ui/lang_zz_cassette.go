package ui

// The texts of the player's cassette (Settings → Cassette, cassette.go):
// its section, its tiles and their choices, in all twenty-one languages, in
// the order of languages.
func init() {
	for key, vals := range map[string][21]string{
		"set_cassette": {"Casete", "Cassette", "Cassete", "Cassette", "Cassetta", "Kassette", "Cassette", "Kassett", "Kaseta", "Kaset",
			"Кассета", "Касета", "Kaset", "Băng cassette", "カセット", "磁带", "卡帶", "카세트", "เทปคาสเซ็ต", "कैसेट", "كاسيت"},
		"sec_cassette": {"Casete", "Cassette", "Cassete", "Cassette", "Cassetta", "Kassette", "Cassette", "Kassett", "Kaseta", "Kaset",
			"Кассета", "Касета", "Kaset", "Băng cassette", "カセット", "磁带", "卡帶", "카세트", "เทปคาสเซ็ต", "कैसेट", "كاسيت"},
		"set_tape_skin": {"Aspecto", "Skin", "Visual", "Habillage", "Tema", "Design", "Uiterlijk", "Utseende", "Skórka", "Görünüm",
			"Оформление", "Оформлення", "Tampilan", "Giao diện", "スキン", "外观", "外觀", "스킨", "สกิน", "स्किन", "المظهر"},
		"set_tape_label": {"Etiqueta", "Label", "Etiqueta", "Étiquette", "Etichetta", "Etikett", "Etiket", "Etikett", "Etykieta", "Etiket",
			"Наклейка", "Наліпка", "Label", "Nhãn", "ラベル", "标签", "標籤", "라벨", "ฉลาก", "लेबल", "الملصق"},
		"set_tape_paper": {"Papel", "Paper", "Papel", "Papier", "Carta", "Papier", "Papier", "Papper", "Tło napisu", "Kâğıt",
			"Бумага", "Папір", "Kertas", "Giấy", "紙の色", "纸张", "紙張", "종이", "กระดาษ", "कागज़", "الورق"},
		"set_tape_font": {"Letra", "Handwriting", "Letra", "Écriture", "Scrittura", "Schrift", "Handschrift", "Handstil", "Pismo", "El yazısı",
			"Почерк", "Почерк", "Tulisan", "Chữ viết", "手書き", "字迹", "字跡", "글씨", "ลายมือ", "लिखावट", "الخط"},
		"set_tape_hand": {"Tamaño de letra", "Writing size", "Tamanho da letra", "Taille de l'écriture", "Dimensione scrittura", "Schriftgröße", "Tekstgrootte", "Textstorlek", "Rozmiar pisma", "Yazı boyutu",
			"Размер почерка", "Розмір почерку", "Ukuran tulisan", "Cỡ chữ", "文字の大きさ", "字号", "字級", "글씨 크기", "ขนาดตัวอักษร", "लिखावट का आकार", "حجم الخط"},
		"set_tape_glow": {"Resplandor", "Glow", "Brilho", "Lueur", "Bagliore", "Leuchten", "Gloed", "Glöd", "Poświata", "Parıltı",
			"Свечение", "Сяйво", "Pendar", "Ánh sáng", "光", "光晕", "光暈", "빛", "แสงเรือง", "चमक", "التوهج"},
		"set_tape_anim": {"Animación del resplandor", "Glow animation", "Animação do brilho", "Animation de la lueur", "Animazione bagliore", "Leucht-Animation", "Gloedanimatie", "Glödanimering", "Animacja poświaty", "Parıltı animasyonu",
			"Анимация свечения", "Анімація сяйва", "Animasi pendar", "Hiệu ứng ánh sáng", "光の動き", "光晕动画", "光暈動畫", "빛 애니메이션", "แสงเคลื่อนไหว", "चमक का एनिमेशन", "حركة التوهج"},
		"set_tape_dust": {"Tamaño de las motas", "Speck size", "Tamanho das partículas", "Taille des particules", "Dimensione granelli", "Staubkorngröße", "Stofjesgrootte", "Dammkornsstorlek", "Wielkość pyłków", "Toz zerresi boyutu",
			"Размер пылинок", "Розмір порошинок", "Ukuran debu", "Cỡ hạt bụi", "ちりの大きさ", "尘粒大小", "塵粒大小", "먼지 크기", "ขนาดละอองฝุ่น", "धूलकण का आकार", "حجم ذرات الغبار"},
		"set_tape_reflect": {"Reflejo", "Reflection", "Reflexo", "Reflet", "Riflesso", "Spiegelung", "Weerspiegeling", "Spegling", "Odbicie", "Yansıma",
			"Отражение", "Відображення", "Pantulan", "Phản chiếu", "反射", "倒影", "倒影", "반사", "เงาสะท้อน", "प्रतिबिंब", "الانعكاس"},
		"tape_skin_black": {"Negro", "Black", "Preto", "Noir", "Nero", "Schwarz", "Zwart", "Svart", "Czarna", "Siyah",
			"Чёрная", "Чорна", "Hitam", "Đen", "ブラック", "黑色", "黑色", "블랙", "ดำ", "काला", "أسود"},
		"tape_skin_ocean": {"Océano", "Ocean", "Oceano", "Océan", "Oceano", "Ozean", "Oceaan", "Hav", "Ocean", "Okyanus",
			"Океан", "Океан", "Samudra", "Đại dương", "オーシャン", "海洋", "海洋", "오션", "มหาสมุทร", "सागर", "محيط"},
		"tape_skin_lava": {"Lava", "Lava", "Lava", "Lave", "Lava", "Lava", "Lava", "Lava", "Lawa", "Lav",
			"Лава", "Лава", "Lava", "Dung nham", "ラヴァ", "熔岩", "熔岩", "용암", "ลาวา", "लावा", "حمم"},
		"tape_skin_toxic": {"Tóxico", "Toxic", "Tóxico", "Toxique", "Tossico", "Toxisch", "Giftig", "Giftig", "Toksyna", "Zehirli",
			"Токсичная", "Токсична", "Beracun", "Độc tố", "トキシック", "毒绿", "毒綠", "톡식", "พิษ", "ज़हरीला", "سام"},
		"tape_skin_chrome": {"Cromo", "Chrome", "Cromo", "Chrome", "Cromo", "Chrom", "Chroom", "Krom", "Chrom", "Krom",
			"Хром", "Хром", "Krom", "Crôm", "クローム", "铬银", "鉻銀", "크롬", "โครม", "क्रोम", "كروم"},
		"tape_skin_cover": {"De la portada", "From the cover", "Da capa", "De la pochette", "Dalla copertina", "Vom Cover", "Van de hoes", "Från omslaget", "Z okładki", "Kapaktan",
			"С обложки", "З обкладинки", "Dari sampul", "Theo bìa", "ジャケットから", "取自封面", "取自封面", "커버에서", "จากปก", "कवर से", "من الغلاف"},
		"tape_label_cover": {"Portada", "Cover", "Capa", "Pochette", "Copertina", "Cover", "Hoes", "Omslag", "Okładka", "Kapak",
			"Обложка", "Обкладинка", "Sampul", "Bìa", "ジャケット", "封面", "封面", "커버", "ปก", "कवर", "الغلاف"},
		"tape_label_hand": {"Escrito a mano", "Handwritten", "Escrito à mão", "Manuscrite", "A mano", "Handschrift", "Handgeschreven", "Handskrivet", "Napis odręczny", "El yazısı",
			"От руки", "Від руки", "Tulisan tangan", "Viết tay", "手書き", "手写", "手寫", "손글씨", "ลายมือ", "हाथ से लिखा", "بخط اليد"},
		"tape_paper_skin": {"Del aspecto", "From the skin", "Do visual", "De l'habillage", "Dal tema", "Vom Design", "Van uiterlijk", "Från utseendet", "Ze skórki", "Görünümden",
			"Из оформления", "З оформлення", "Dari tampilan", "Theo giao diện", "スキンに合わせる", "跟随外观", "跟隨外觀", "스킨대로", "ตามสกิน", "स्किन से", "من المظهر"},
		"tape_paper_white": {"Blanco", "White", "Branco", "Blanc", "Bianco", "Weiß", "Wit", "Vit", "Biała", "Beyaz",
			"Белая", "Біла", "Putih", "Trắng", "白", "白色", "白色", "흰색", "ขาว", "सफ़ेद", "أبيض"},
		"tape_paper_yellow": {"Amarillo", "Yellow", "Amarelo", "Jaune", "Giallo", "Gelb", "Geel", "Gul", "Żółta", "Sarı",
			"Жёлтая", "Жовта", "Kuning", "Vàng", "黄色", "黄色", "黃色", "노란색", "เหลือง", "पीला", "أصفر"},
		"tape_paper_navy": {"Azul marino", "Navy", "Azul-marinho", "Bleu marine", "Blu navy", "Marineblau", "Marineblauw", "Marinblå", "Granatowa", "Lacivert",
			"Тёмно-синяя", "Темно-синя", "Biru tua", "Xanh navy", "紺", "藏青", "藏青", "남색", "กรมท่า", "गहरा नीला", "كحلي"},
		"tape_paper_pink": {"Rosa", "Pink", "Rosa", "Rose", "Rosa", "Rosa", "Roze", "Rosa", "Różowa", "Pembe",
			"Розовая", "Рожева", "Merah muda", "Hồng", "ピンク", "粉色", "粉紅", "분홍색", "ชมพู", "गुलाबी", "وردي"},
		"tape_paper_purple": {"Morado", "Purple", "Roxo", "Violet", "Viola", "Lila", "Paars", "Lila", "Fioletowa", "Mor",
			"Фиолетовая", "Фіолетова", "Ungu", "Tím", "紫", "紫色", "紫色", "보라색", "ม่วง", "बैंगनी", "بنفسجي"},
		"tape_paper_green": {"Verde", "Green", "Verde", "Vert", "Verde", "Grün", "Groen", "Grön", "Zielona", "Yeşil",
			"Зелёная", "Зелена", "Hijau", "Xanh lá", "緑", "绿色", "綠色", "초록색", "เขียว", "हरा", "أخضر"},
		"tape_paper_orange": {"Naranja", "Orange", "Laranja", "Orange", "Arancione", "Orange", "Oranje", "Orange", "Pomarańczowa", "Turuncu",
			"Оранжевая", "Помаранчева", "Oranye", "Cam", "オレンジ", "橙色", "橘色", "주황색", "ส้ม", "नारंगी", "برتقالي"},
		"tape_paper_red": {"Rojo", "Red", "Vermelho", "Rouge", "Rosso", "Rot", "Rood", "Röd", "Czerwona", "Kırmızı",
			"Красная", "Червона", "Merah", "Đỏ", "赤", "红色", "紅色", "빨간색", "แดง", "लाल", "أحمر"},
		"tape_font_bold": {"Negrita", "Bold", "Negrito", "Gras", "Grassetto", "Fett", "Vet", "Fet", "Pogrubione", "Kalın",
			"Жирный", "Жирний", "Tebal", "Đậm", "太字", "粗体", "粗體", "굵게", "ตัวหนา", "मोटा", "عريض"},
		"tape_font_thin": {"Normal", "Regular", "Normal", "Normal", "Normale", "Normal", "Normaal", "Normal", "Normalne", "Normal",
			"Обычный", "Звичайний", "Biasa", "Thường", "標準", "常规", "一般", "보통", "ปกติ", "सामान्य", "عادي"},
		"tape_glow_dust": {"Motas", "Specks", "Partículas", "Particules", "Granelli", "Staub", "Stofjes", "Dammkorn", "Pyłki", "Toz",
			"Пылинки", "Порошинки", "Debu", "Hạt bụi", "ちり", "尘粒", "塵粒", "먼지", "ละอองฝุ่น", "धूलकण", "ذرات"},
		"tape_glow_static": {"Fijo", "Static", "Fixo", "Fixe", "Fisso", "Statisch", "Vast", "Fast", "Statyczna", "Sabit",
			"Статичное", "Статичне", "Diam", "Cố định", "固定", "静态", "靜態", "고정", "คงที่", "स्थिर", "ثابت"},
		"tape_anim_none": {"Ninguna", "None", "Nenhuma", "Aucune", "Nessuna", "Keine", "Geen", "Ingen", "Brak", "Yok",
			"Нет", "Немає", "Tidak ada", "Không", "なし", "无", "無", "없음", "ไม่มี", "कोई नहीं", "لا شيء"},
		"tape_anim_breathe": {"Respirar", "Breathe", "Respirar", "Respiration", "Respiro", "Atmen", "Ademen", "Andas", "Oddech", "Nefes",
			"Дыхание", "Дихання", "Napas", "Nhịp thở", "呼吸", "呼吸", "呼吸", "호흡", "หายใจ", "साँस", "تنفس"},
		"tape_anim_orbit": {"Vuelta", "Orbit", "Órbita", "Orbite", "Orbita", "Umlauf", "Rondgang", "Varv", "Obieg", "Tur",
			"По кругу", "По колу", "Keliling", "Chạy vòng", "周回", "环绕", "環繞", "회전", "วนรอบ", "परिक्रमा", "دوران"},
		"tape_anim_beat": {"Ritmo", "Beat", "Ritmo", "Rythme", "Ritmo", "Takt", "Ritme", "Takt", "Rytm", "Ritim",
			"Ритм", "Ритм", "Irama", "Nhịp", "ビート", "节拍", "節拍", "비트", "จังหวะ", "ताल", "الإيقاع"},
	} {
		for i, l := range languages {
			if strs[l] != nil {
				strs[l][key] = vals[i]
			}
		}
	}
	// The skins named after a style, the same in every language.
	for key, v := range map[string]string{"tape_skin_neon": "Neon", "tape_skin_cyber": "Cyberpunk", "tape_skin_vapor": "Vaporwave"} {
		for _, l := range languages {
			if strs[l] != nil {
				strs[l][key] = v
			}
		}
	}
}
