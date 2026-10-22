package ui

// Los textos de la actualización de la app de Android (update.go): el APK ya
// está bajado y el sistema pide confirmar la instalación. En los trece
// idiomas.
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
}
