package ui

// Los textos del servidor MPD (Ajustes → En línea), en los trece idiomas.
func init() {
	for lang, m := range map[string]map[string]string{
		"es": {"mpd_password_for": "Contraseña de %s", "set_mpd": "Servidor MPD", "mpd_sub": "Las apps de MPD y otros audiofilus tocan y controlan la música aquí", "mpd_server": "Servidor", "mpd_password": "Contraseña", "mpd_no_password": "Ninguna", "mpd_address": "Dirección"},
		"en": {"mpd_password_for": "Password for %s", "set_mpd": "MPD server", "mpd_sub": "MPD apps and other audiofilus can play and control music here", "mpd_server": "Server", "mpd_password": "Password", "mpd_no_password": "None", "mpd_address": "Address"},
		"tr": {"mpd_password_for": "%s parolası", "set_mpd": "MPD sunucusu", "mpd_sub": "MPD uygulamaları ve diğer audiofilus'lar burada müzik çalıp kontrol edebilir", "mpd_server": "Sunucu", "mpd_password": "Parola", "mpd_no_password": "Yok", "mpd_address": "Adres"},
		"de": {"mpd_password_for": "Passwort für %s", "set_mpd": "MPD-Server", "mpd_sub": "MPD-Apps und andere audiofilus können hier Musik abspielen und steuern", "mpd_server": "Server", "mpd_password": "Passwort", "mpd_no_password": "Keins", "mpd_address": "Adresse"},
		"fr": {"mpd_password_for": "Mot de passe de %s", "set_mpd": "Serveur MPD", "mpd_sub": "Les apps MPD et d'autres audiofilus peuvent jouer et contrôler la musique ici", "mpd_server": "Serveur", "mpd_password": "Mot de passe", "mpd_no_password": "Aucun", "mpd_address": "Adresse"},
		"it": {"mpd_password_for": "Password di %s", "set_mpd": "Server MPD", "mpd_sub": "Le app MPD e altri audiofilus possono riprodurre e controllare la musica qui", "mpd_server": "Server", "mpd_password": "Password", "mpd_no_password": "Nessuna", "mpd_address": "Indirizzo"},
		"pt": {"mpd_password_for": "Senha de %s", "set_mpd": "Servidor MPD", "mpd_sub": "Apps de MPD e outros audiofilus podem tocar e controlar a música aqui", "mpd_server": "Servidor", "mpd_password": "Senha", "mpd_no_password": "Nenhuma", "mpd_address": "Endereço"},
		"pl": {"mpd_password_for": "Hasło do %s", "set_mpd": "Serwer MPD", "mpd_sub": "Aplikacje MPD i inne audiofilus mogą tu odtwarzać i sterować muzyką", "mpd_server": "Serwer", "mpd_password": "Hasło", "mpd_no_password": "Brak", "mpd_address": "Adres"},
		"ru": {"mpd_password_for": "Пароль для %s", "set_mpd": "Сервер MPD", "mpd_sub": "Приложения MPD и другие audiofilus могут играть и управлять музыкой здесь", "mpd_server": "Сервер", "mpd_password": "Пароль", "mpd_no_password": "Нет", "mpd_address": "Адрес"},
		"id": {"mpd_password_for": "Kata sandi %s", "set_mpd": "Server MPD", "mpd_sub": "Aplikasi MPD dan audiofilus lain bisa memutar dan mengontrol musik di sini", "mpd_server": "Server", "mpd_password": "Kata sandi", "mpd_no_password": "Tidak ada", "mpd_address": "Alamat"},
		"ja": {"mpd_password_for": "%s のパスワード", "set_mpd": "MPDサーバー", "mpd_sub": "MPDアプリや他のaudiofilusからここで音楽を再生・操作できます", "mpd_server": "サーバー", "mpd_password": "パスワード", "mpd_no_password": "なし", "mpd_address": "アドレス"},
		"zh": {"mpd_password_for": "%s 的密码", "set_mpd": "MPD 服务器", "mpd_sub": "MPD 应用和其他 audiofilus 可以在这里播放和控制音乐", "mpd_server": "服务器", "mpd_password": "密码", "mpd_no_password": "无", "mpd_address": "地址"},
		"ko": {"mpd_password_for": "%s 비밀번호", "set_mpd": "MPD 서버", "mpd_sub": "MPD 앱과 다른 audiofilus가 여기서 음악을 재생하고 제어할 수 있습니다", "mpd_server": "서버", "mpd_password": "비밀번호", "mpd_no_password": "없음", "mpd_address": "주소"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
