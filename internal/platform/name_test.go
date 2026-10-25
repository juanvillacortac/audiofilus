package platform

import "testing"

// TestCleanModel comprueba que los modelos del chip se descartan y los
// códigos de placa salen en mayúsculas.
func TestCleanModel(t *testing.T) {
	for in, want := range map[string]string{
		"rg34xx-sp":           "RG34XX-SP",
		"Anbernic RG34XX SP":  "Anbernic RG34XX SP",
		"sun50iw9":            "",
		"Rockchip RK3326 EVB": "",
		"":                    "",
	} {
		if got := cleanModel(in); got != want {
			t.Errorf("cleanModel(%q) = %q, want %q", in, got, want)
		}
	}
}
