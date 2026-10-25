package ui

// Los textos de recibir música de otro aparato (Reproduciendo → Inicio), en
// los trece idiomas.
func init() {
	for lang, m := range map[string]map[string]string{
		"es": {"recv_stop": "Dejar de recibir", "recv_mpd_off": "Apagar servidor MPD", "recv_stopped": "Ya no recibe música", "cast_released": "%s dejó de recibir"},
		"en": {"recv_stop": "Stop receiving", "recv_mpd_off": "Turn off MPD server", "recv_stopped": "No longer receiving music", "cast_released": "%s stopped receiving"},
		"tr": {"recv_stop": "Almayı durdur", "recv_mpd_off": "MPD sunucusunu kapat", "recv_stopped": "Artık müzik alınmıyor", "cast_released": "%s almayı durdurdu"},
		"de": {"recv_stop": "Empfang beenden", "recv_mpd_off": "MPD-Server ausschalten", "recv_stopped": "Empfängt keine Musik mehr", "cast_released": "%s empfängt nicht mehr"},
		"fr": {"recv_stop": "Arrêter la réception", "recv_mpd_off": "Éteindre le serveur MPD", "recv_stopped": "Ne reçoit plus de musique", "cast_released": "%s a arrêté la réception"},
		"it": {"recv_stop": "Smetti di ricevere", "recv_mpd_off": "Spegni il server MPD", "recv_stopped": "Non riceve più musica", "cast_released": "%s ha smesso di ricevere"},
		"pt": {"recv_stop": "Parar de receber", "recv_mpd_off": "Desligar servidor MPD", "recv_stopped": "Não recebe mais música", "cast_released": "%s parou de receber"},
		"pl": {"recv_stop": "Przestań odbierać", "recv_mpd_off": "Wyłącz serwer MPD", "recv_stopped": "Już nie odbiera muzyki", "cast_released": "%s przestał odbierać"},
		"ru": {"recv_stop": "Перестать принимать", "recv_mpd_off": "Выключить сервер MPD", "recv_stopped": "Музыка больше не принимается", "cast_released": "%s перестал принимать"},
		"id": {"recv_stop": "Berhenti menerima", "recv_mpd_off": "Matikan server MPD", "recv_stopped": "Tidak lagi menerima musik", "cast_released": "%s berhenti menerima"},
		"ja": {"recv_stop": "受信をやめる", "recv_mpd_off": "MPDサーバーをオフ", "recv_stopped": "音楽の受信を終了しました", "cast_released": "%s が受信をやめました"},
		"zh": {"recv_stop": "停止接收", "recv_mpd_off": "关闭 MPD 服务器", "recv_stopped": "已停止接收音乐", "cast_released": "%s 已停止接收"},
		"ko": {"recv_stop": "수신 중지", "recv_mpd_off": "MPD 서버 끄기", "recv_stopped": "더 이상 음악을 받지 않음", "cast_released": "%s 수신 중지됨"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
