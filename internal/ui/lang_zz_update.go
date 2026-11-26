package ui

// Los textos de la actualización (update.go) que vinieron después. El de la
// app de Android: el APK ya está bajado y el sistema pide confirmar la
// instalación. En los trece idiomas.
func init() {
	for lang, v := range map[string]string{
		"es": "Confirma la instalación de %s",
		"en": "Confirm the installation of %s",
		"tr": "%s kurulumunu onaylayın",
		"de": "Bestätige die Installation von %s",
		"fr": "Confirme l'installation de %s",
		"it": "Conferma l'installazione di %s",
		"pt": "Confirme a instalação da %s",
		"pl": "Potwierdź instalację %s",
		"ru": "Подтвердите установку %s",
		"id": "Konfirmasi pemasangan %s",
		"ja": "%s のインストールを確認してください",
		"zh": "请确认安装 %s",
		"ko": "%s 설치를 확인하세요",
	} {
		strs[lang]["upd_confirm"] = v
	}
	// La app de macOS abierta desde el DMG (o trasladada por Gatekeeper) no se
	// puede cambiar: hay que moverla a Aplicaciones, con el nombre que le da
	// el Finder en cada idioma. En los veintiún idiomas, en su orden.
	for i, v := range [21]string{
		"Para actualizar, mueve Audiofilus a Aplicaciones",
		"To update, move Audiofilus to Applications",
		"Para atualizar, mova o Audiofilus para Aplicativos",
		"Pour mettre à jour, déplace Audiofilus dans Applications",
		"Per aggiornare, sposta Audiofilus in Applicazioni",
		"Zum Aktualisieren Audiofilus in den Ordner Programme verschieben",
		"Zet Audiofilus in de map Apps om bij te werken",
		"Flytta Audiofilus till mappen Program för att uppdatera",
		"Aby zaktualizować, przenieś Audiofilus do folderu Programy",
		"Güncellemek için Audiofilus'u Uygulamalar klasörüne taşı",
		"Чтобы обновить, переместите Audiofilus в папку «Программы»",
		"Щоб оновити, перемісти Audiofilus у папку «Програми»",
		"Untuk memperbarui, pindahkan Audiofilus ke folder Aplikasi",
		"Để cập nhật, hãy chuyển Audiofilus vào thư mục Ứng dụng",
		"アップデートするには、Audiofilus を「アプリケーション」フォルダに移動してください",
		"要更新，请将 Audiofilus 移到“应用程序”文件夹",
		"要更新，請將 Audiofilus 移到「應用程式」檔案夾",
		"업데이트하려면 Audiofilus를 응용 프로그램 폴더로 옮기세요",
		"หากต้องการอัปเดต ให้ย้าย Audiofilus ไปไว้ในโฟลเดอร์แอปพลิเคชัน",
		"अपडेट करने के लिए Audiofilus को ऐप्लिकेशन फ़ोल्डर में ले जाएँ",
		"للتحديث، انقل Audiofilus إلى مجلد التطبيقات",
	} {
		if strs[languages[i]] != nil {
			strs[languages[i]]["upd_move"] = v
		}
	}
}
