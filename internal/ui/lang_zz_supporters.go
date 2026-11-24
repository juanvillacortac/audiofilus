package ui

// El agradecimiento a quienes apoyaron Audiofilus (supporters.go), en los
// veintiún idiomas.
func init() {
	for lang, v := range map[string]string{
		"es": "Gracias,", "en": "Thank you,", "pt": "Obrigado,", "fr": "Merci,", "it": "Grazie,",
		"de": "Danke,", "nl": "Bedankt,", "sv": "Tack,", "pl": "Dziękuję,", "tr": "Teşekkürler,",
		"ru": "Спасибо,", "uk": "Дякую,", "id": "Terima kasih,", "vi": "Cảm ơn,", "ja": "ありがとう、",
		"zh": "谢谢，", "zh-TW": "謝謝，", "ko": "고마워요,", "th": "ขอบคุณ", "hi": "धन्यवाद,", "ar": "شكرًا،",
	} {
		if strs[lang] != nil {
			strs[lang]["supp_thanks"] = v
		}
	}
}
