package ui

// Los textos de la salida de audio (outputui.go), en los veintiún idiomas.
func init() {
	for lang, v := range map[string][5]string{
		// set_output, out_auto, out_builtin, out_sub, out_playing
		"es":    {"Salida de audio", "Automático", "Integrada", "Automático pasa a un DAC USB al conectarlo", "Sonando"},
		"en":    {"Audio output", "Automatic", "Built-in", "Automatic switches to a USB DAC when you plug it in", "Playing"},
		"pt":    {"Saída de áudio", "Automático", "Integrada", "Automático muda para um DAC USB ao conectá-lo", "Tocando"},
		"fr":    {"Sortie audio", "Automatique", "Intégrée", "Automatique passe à un DAC USB dès qu'il est branché", "En lecture"},
		"it":    {"Uscita audio", "Automatica", "Integrata", "Automatica passa a un DAC USB quando lo colleghi", "In riproduzione"},
		"de":    {"Audioausgang", "Automatisch", "Eingebaut", "Automatisch wechselt zu einem USB-DAC, sobald er eingesteckt wird", "Läuft"},
		"nl":    {"Audio-uitgang", "Automatisch", "Ingebouwd", "Automatisch schakelt over naar een USB-DAC zodra je die aansluit", "Speelt"},
		"sv":    {"Ljudutgång", "Automatisk", "Inbyggd", "Automatisk byter till en USB-DAC när du ansluter den", "Spelar"},
		"pl":    {"Wyjście audio", "Automatycznie", "Wbudowane", "Automatycznie przełącza na DAC USB po podłączeniu", "Odtwarzane"},
		"tr":    {"Ses çıkışı", "Otomatik", "Dahili", "Otomatik, takıldığında USB DAC'a geçer", "Çalıyor"},
		"ru":    {"Аудиовыход", "Автоматически", "Встроенный", "Автоматически переключается на USB-ЦАП при подключении", "Играет"},
		"uk":    {"Аудіовихід", "Автоматично", "Вбудований", "Автоматично перемикається на USB-ЦАП під час підключення", "Грає"},
		"id":    {"Output audio", "Otomatis", "Bawaan", "Otomatis pindah ke DAC USB saat dicolokkan", "Diputar"},
		"vi":    {"Đầu ra âm thanh", "Tự động", "Tích hợp", "Tự động chuyển sang DAC USB khi cắm vào", "Đang phát"},
		"ja":    {"音声出力", "自動", "本体", "自動ではUSB DACをつなぐとそちらに切り替えます", "再生中"},
		"zh":    {"音频输出", "自动", "内置", "自动：插入 USB DAC 时切换过去", "正在播放"},
		"zh-TW": {"音訊輸出", "自動", "內建", "自動：插上 USB DAC 時切換過去", "播放中"},
		"ko":    {"오디오 출력", "자동", "내장", "자동은 USB DAC를 연결하면 그쪽으로 전환합니다", "재생 중"},
		"th":    {"เอาต์พุตเสียง", "อัตโนมัติ", "ในตัว", "อัตโนมัติจะสลับไปที่ DAC USB เมื่อเสียบ", "กำลังเล่น"},
		"hi":    {"ऑडियो आउटपुट", "स्वचालित", "बिल्ट-इन", "स्वचालित: USB DAC लगाते ही उस पर बदल जाता है", "चल रहा है"},
		"ar":    {"مخرج الصوت", "تلقائي", "المدمج", "التلقائي ينتقل إلى DAC عبر USB عند توصيله", "قيد التشغيل"},
	} {
		if strs[lang] == nil {
			continue
		}
		strs[lang]["set_output"] = v[0]
		strs[lang]["out_auto"] = v[1]
		strs[lang]["out_builtin"] = v[2]
		strs[lang]["out_sub"] = v[3]
		strs[lang]["out_playing"] = v[4]
	}
}
