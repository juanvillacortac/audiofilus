package ui

// Los textos de los perfiles de EQ (eqprofiles.go): "Perfil de EQ" en el
// menú de Start y borrar uno, en los trece idiomas.
func init() {
	for lang, v := range map[string]string{
		"es": "Perfil de EQ", "en": "EQ profile", "tr": "EQ profili", "de": "EQ-Profil",
		"fr": "Profil d'EQ", "it": "Profilo EQ", "pt": "Perfil de EQ", "pl": "Profil EQ",
		"ru": "Профиль EQ", "id": "Profil EQ", "ja": "EQプロファイル", "zh": "EQ 配置", "ko": "EQ 프로필",
	} {
		strs[lang]["eqp_title"] = v
	}
	for lang, m := range map[string]map[string]string{
		"es": {"h_delete": "BORRAR", "eqp_delete": "Borrar un perfil", "eqp_delete_q": "¿Borrar el perfil %s?", "eqp_delete_yes": "Borrar", "eqp_deleted": "Perfil %s borrado"},
		"en": {"h_delete": "DELETE", "eqp_delete": "Delete a profile", "eqp_delete_q": "Delete the profile %s?", "eqp_delete_yes": "Delete", "eqp_deleted": "Profile %s deleted"},
		"tr": {"h_delete": "SİL", "eqp_delete": "Profil sil", "eqp_delete_q": "%s profili silinsin mi?", "eqp_delete_yes": "Sil", "eqp_deleted": "%s profili silindi"},
		"de": {"h_delete": "LÖSCHEN", "eqp_delete": "Profil löschen", "eqp_delete_q": "Profil %s löschen?", "eqp_delete_yes": "Löschen", "eqp_deleted": "Profil %s gelöscht"},
		"fr": {"h_delete": "SUPPRIMER", "eqp_delete": "Supprimer un profil", "eqp_delete_q": "Supprimer le profil %s ?", "eqp_delete_yes": "Supprimer", "eqp_deleted": "Profil %s supprimé"},
		"it": {"h_delete": "ELIMINA", "eqp_delete": "Elimina un profilo", "eqp_delete_q": "Eliminare il profilo %s?", "eqp_delete_yes": "Elimina", "eqp_deleted": "Profilo %s eliminato"},
		"pt": {"h_delete": "APAGAR", "eqp_delete": "Apagar um perfil", "eqp_delete_q": "Apagar o perfil %s?", "eqp_delete_yes": "Apagar", "eqp_deleted": "Perfil %s apagado"},
		"pl": {"h_delete": "USUŃ", "eqp_delete": "Usuń profil", "eqp_delete_q": "Usunąć profil %s?", "eqp_delete_yes": "Usuń", "eqp_deleted": "Usunięto profil %s"},
		"ru": {"h_delete": "УДАЛИТЬ", "eqp_delete": "Удалить профиль", "eqp_delete_q": "Удалить профиль %s?", "eqp_delete_yes": "Удалить", "eqp_deleted": "Профиль %s удалён"},
		"id": {"h_delete": "HAPUS", "eqp_delete": "Hapus profil", "eqp_delete_q": "Hapus profil %s?", "eqp_delete_yes": "Hapus", "eqp_deleted": "Profil %s dihapus"},
		"ja": {"h_delete": "削除", "eqp_delete": "プロファイルを削除", "eqp_delete_q": "プロファイル %s を削除しますか？", "eqp_delete_yes": "削除", "eqp_deleted": "プロファイル %s を削除しました"},
		"zh": {"h_delete": "删除", "eqp_delete": "删除配置", "eqp_delete_q": "删除配置 %s？", "eqp_delete_yes": "删除", "eqp_deleted": "已删除配置 %s"},
		"ko": {"h_delete": "삭제", "eqp_delete": "프로필 삭제", "eqp_delete_q": "%s 프로필을 삭제할까요?", "eqp_delete_yes": "삭제", "eqp_deleted": "%s 프로필 삭제됨"},
	} {
		for k, v := range m {
			strs[lang][k] = v
		}
	}
}
