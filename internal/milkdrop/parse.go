package milkdrop

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Un preset de MilkDrop (.milk) es un INI: una sección [preset00] con líneas
// clave=valor. Hay valores iniciales (zoom=1.01, fDecay=0.98, nWaveMode=2...)
// y código partido en líneas numeradas: per_frame_init_N (una vez al cargar),
// per_frame_N (cada fotograma), per_pixel_N (cada vértice de la malla), y
// para las cuatro ondas y las cuatro formas, wavecode_N_clave / shapecode_N_clave
// (sus valores) y wave_N_init/per_frame/per_point M, shape_N_init/per_frame M
// (su código). Los shaders HLSL de MilkDrop 2 (warp_N, comp_N) no se usan:
// Parse solo apunta que los había (Preset.HasShaders).

// MaxWaves y MaxShapes son las ondas y formas propias de un preset.
const (
	MaxWaves  = 4
	MaxShapes = 4
)

// Preset es un preset ya leído (sin compilar).
type Preset struct {
	Name string
	// Vals son los valores iniciales, con la clave en minúsculas tal cual
	// está en el archivo (fdecay, zoom, nwavemode...).
	Vals map[string]float64
	// El código de cada parte, con las líneas unidas en orden.
	Init, PerFrame, PerPixel string
	Waves                    [MaxWaves]WaveDef
	Shapes                   [MaxShapes]ShapeDef
	// HasShaders dice si el preset traía shaders de MilkDrop 2 (se ignoran).
	HasShaders bool
}

// WaveDef es una onda propia: sus valores (enabled, samples, r...) y su código.
type WaveDef struct {
	Vals                     map[string]float64
	Init, PerFrame, PerPoint string
}

// ShapeDef es una forma propia.
type ShapeDef struct {
	Vals           map[string]float64
	Init, PerFrame string
}

// codeLines junta las líneas numeradas de un trozo de código.
type codeLines map[int]string

// join las une en orden.
func (c codeLines) join() string {
	keys := make([]int, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(c[k])
		b.WriteByte('\n')
	}
	return b.String()
}

// add pone la línea n, si el sufijo es un número.
func (c codeLines) add(suffix, val string) bool {
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return false
	}
	c[n] = val
	return true
}

// Parse lee un preset. name es el que se le da (el nombre del archivo, sin
// .milk). Solo falla si no hay nada que se parezca a un preset.
func Parse(name string, data []byte) (*Preset, error) {
	p := &Preset{Name: name, Vals: map[string]float64{}}
	init, frame, pixel := codeLines{}, codeLines{}, codeLines{}
	var wInit, wFrame, wPoint, sInit, sFrame [4]codeLines
	for i := 0; i < 4; i++ {
		wInit[i], wFrame[i], wPoint[i], sInit[i], sFrame[i] = codeLines{}, codeLines{}, codeLines{}, codeLines{}, codeLines{}
		p.Waves[i].Vals = map[string]float64{}
		p.Shapes[i].Vals = map[string]float64{}
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	section, lines := "", 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if strings.HasPrefix(section, "preset") && strings.HasPrefix(strings.ToLower(line[1:]), "preset") {
				break // la segunda: aquí acaba el primero
			}
			section = strings.ToLower(line[1 : len(line)-1])
			continue
		}
		// Un archivo con varias secciones: solo cuenta la primera [presetNN]
		// (o lo que haya antes de cualquier sección).
		if section != "" && !strings.HasPrefix(section, "preset") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(line[:eq])), strings.TrimSpace(line[eq+1:])
		lines++
		switch {
		case strings.HasPrefix(key, "per_frame_init_"):
			init.add(key[len("per_frame_init_"):], val)
		case strings.HasPrefix(key, "per_frame_"):
			frame.add(key[len("per_frame_"):], val)
		case strings.HasPrefix(key, "per_pixel_"):
			pixel.add(key[len("per_pixel_"):], val)
		case strings.HasPrefix(key, "per_vertex_"):
			pixel.add(key[len("per_vertex_"):], val)
		case strings.HasPrefix(key, "warp_") || strings.HasPrefix(key, "comp_"):
			// Los shaders de MilkDrop 2 (HLSL), que aquí no se pueden usar.
			p.HasShaders = true
		case strings.HasPrefix(key, "wavecode_"):
			if i, k, ok := indexed(key[len("wavecode_"):]); ok && i < MaxWaves {
				if v, ok := number(val); ok {
					p.Waves[i].Vals[k] = v
				}
			}
		case strings.HasPrefix(key, "shapecode_"):
			if i, k, ok := indexed(key[len("shapecode_"):]); ok && i < MaxShapes {
				if v, ok := number(val); ok {
					p.Shapes[i].Vals[k] = v
				}
			}
		case digitAfter(key, "wave_"):
			if i, k, ok := indexed(key[len("wave_"):]); ok && i < MaxWaves {
				codeKey(k, val, map[string]codeLines{"init": wInit[i], "per_frame": wFrame[i], "per_point": wPoint[i]})
			}
		case digitAfter(key, "shape_"):
			if i, k, ok := indexed(key[len("shape_"):]); ok && i < MaxShapes {
				codeKey(k, val, map[string]codeLines{"init": sInit[i], "per_frame": sFrame[i]})
			}
		default:
			if v, ok := number(val); ok {
				p.Vals[key] = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read preset %s: %w", name, err)
	}
	if lines == 0 {
		return nil, fmt.Errorf("preset %s: no key=value lines", name)
	}
	p.Init, p.PerFrame, p.PerPixel = init.join(), frame.join(), pixel.join()
	for i := 0; i < 4; i++ {
		p.Waves[i].Init, p.Waves[i].PerFrame, p.Waves[i].PerPoint = wInit[i].join(), wFrame[i].join(), wPoint[i].join()
		p.Shapes[i].Init, p.Shapes[i].PerFrame = sInit[i].join(), sFrame[i].join()
	}
	return p, nil
}

// indexed parte "3_per_frame12" en 3 y "per_frame12".
func indexed(s string) (int, string, bool) {
	us := strings.IndexByte(s, '_')
	if us <= 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(s[:us])
	if err != nil || n < 0 {
		return 0, "", false
	}
	return n, s[us+1:], true
}

// codeKey pone la línea de "per_frame12" (o "per_point3", "init1") en su
// trozo de código.
func codeKey(k, val string, parts map[string]codeLines) {
	// Del más largo al más corto: "per_frame" no debe tomar "per_frame_init".
	for _, name := range []string{"per_frame", "per_point", "init"} {
		c, ok := parts[name]
		if !ok || !strings.HasPrefix(k, name) {
			continue
		}
		if c.add(strings.TrimPrefix(k[len(name):], "_"), val) {
			return
		}
	}
}

// number lee un número de un valor (los de MilkDrop son siempre números).
func number(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil
}

// digitAfter dice si key empieza por prefix seguido de una cifra (wave_0_...).
func digitAfter(key, prefix string) bool {
	return len(key) > len(prefix) && strings.HasPrefix(key, prefix) && isDigit(key[len(prefix)])
}
