package tray

import (
	"os"
	"strings"
)

// Los textos del menú de la bandeja, en los veintiún idiomas de la interfaz
// (internal/ui no se puede importar desde el servicio). En orden: nada
// sonando, " (en pausa)", pausar, reproducir, siguiente, anterior,
// aleatorio, abrir y salir.
const (
	sNothing = iota
	sPaused
	sPause
	sPlay
	sNext
	sPrev
	sShuffle
	sOpen
	sQuit
)

var trayStrs = map[string][9]string{
	"es":    {"Nada sonando", " (en pausa)", "Pausar", "Reproducir", "Siguiente", "Anterior", "Aleatorio", "Abrir Audiofilus", "Salir"},
	"en":    {"Nothing playing", " (paused)", "Pause", "Play", "Next", "Previous", "Shuffle", "Open Audiofilus", "Quit"},
	"pt":    {"Nada tocando", " (pausado)", "Pausar", "Tocar", "Próxima", "Anterior", "Aleatório", "Abrir Audiofilus", "Sair"},
	"fr":    {"Rien en lecture", " (en pause)", "Pause", "Lire", "Suivant", "Précédent", "Aléatoire", "Ouvrir Audiofilus", "Quitter"},
	"it":    {"Niente in riproduzione", " (in pausa)", "Pausa", "Riproduci", "Successivo", "Precedente", "Casuale", "Apri Audiofilus", "Esci"},
	"de":    {"Nichts läuft", " (pausiert)", "Pause", "Abspielen", "Weiter", "Zurück", "Zufall", "Audiofilus öffnen", "Beenden"},
	"nl":    {"Niets aan het spelen", " (gepauzeerd)", "Pauzeren", "Afspelen", "Volgende", "Vorige", "Shuffle", "Audiofilus openen", "Afsluiten"},
	"sv":    {"Inget spelas", " (pausad)", "Pausa", "Spela", "Nästa", "Föregående", "Blanda", "Öppna Audiofilus", "Avsluta"},
	"pl":    {"Nic nie gra", " (pauza)", "Pauza", "Graj", "Następny", "Poprzedni", "Losowo", "Otwórz Audiofilus", "Wyjdź"},
	"tr":    {"Çalan yok", " (duraklatıldı)", "Duraklat", "Çal", "Sonraki", "Önceki", "Karışık çal", "Audiofilus'u aç", "Çık"},
	"ru":    {"Ничего не играет", " (пауза)", "Пауза", "Играть", "Следующая", "Предыдущая", "Перемешать", "Открыть Audiofilus", "Выйти"},
	"uk":    {"Нічого не грає", " (пауза)", "Пауза", "Грати", "Наступна", "Попередня", "Впереміш", "Відкрити Audiofilus", "Вийти"},
	"id":    {"Tidak ada yang diputar", " (dijeda)", "Jeda", "Putar", "Berikutnya", "Sebelumnya", "Acak", "Buka Audiofilus", "Keluar"},
	"vi":    {"Không phát gì", " (tạm dừng)", "Tạm dừng", "Phát", "Bài sau", "Bài trước", "Ngẫu nhiên", "Mở Audiofilus", "Thoát"},
	"ja":    {"再生していません", "（一時停止中）", "一時停止", "再生", "次へ", "前へ", "シャッフル", "Audiofilus を開く", "終了"},
	"zh":    {"没有在播放", "（已暂停）", "暂停", "播放", "下一首", "上一首", "随机", "打开 Audiofilus", "退出"},
	"zh-TW": {"沒有在播放", "（已暫停）", "暫停", "播放", "下一首", "上一首", "隨機", "開啟 Audiofilus", "離開"},
	"ko":    {"재생 중인 곡 없음", " (일시정지)", "일시정지", "재생", "다음", "이전", "셔플", "Audiofilus 열기", "종료"},
	"th":    {"ไม่มีเพลงเล่นอยู่", " (หยุดชั่วคราว)", "หยุดชั่วคราว", "เล่น", "ถัดไป", "ก่อนหน้า", "สุ่ม", "เปิด Audiofilus", "ออก"},
	"hi":    {"कुछ नहीं चल रहा", " (रुका हुआ)", "रोकें", "चलाएँ", "अगला", "पिछला", "शफ़ल", "Audiofilus खोलें", "बाहर"},
	"ar":    {"لا شيء قيد التشغيل", " (متوقف مؤقتًا)", "إيقاف مؤقت", "تشغيل", "التالي", "السابق", "عشوائي", "فتح Audiofilus", "خروج"},
}

// trayLang es el idioma del menú: el de la interfaz, o con "" el de $LANG
// como lo elige ella (ui.SetLanguage); inglés si no hay textos para él.
func trayLang(l string) string {
	if l == "" {
		l = "en"
		for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")} {
			if v != "" {
				if _, ok := trayStrs[v[:min(2, len(v))]]; ok {
					l = v[:2]
				}
				if strings.HasPrefix(v, "zh_TW") || strings.HasPrefix(v, "zh_HK") || strings.HasPrefix(v, "zh_Hant") {
					l = "zh-TW"
				}
				break
			}
		}
	}
	if _, ok := trayStrs[l]; !ok {
		l = "en"
	}
	return l
}
