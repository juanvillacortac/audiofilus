package input

// bits convierte un mapa de bits tal como lo imprime el kernel (palabras
// hexadecimales de 64 bits, la más significativa primero) en la lista de
// códigos activos.
func bits(words []string) []uint16 {
	var out []uint16
	for i := range words {
		word := words[len(words)-1-i]
		var v uint64
		for _, c := range word {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= uint64(c - '0')
			case c >= 'a' && c <= 'f':
				v |= uint64(c-'a') + 10
			}
		}
		for b := 0; b < 64; b++ {
			if v&(1<<b) != 0 {
				out = append(out, uint16(i*64+b))
			}
		}
	}
	return out
}
