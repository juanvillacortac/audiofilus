package ui

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // las texturas de los modelos pueden venir en JPEG
	_ "image/png"
	"math"
)

// Un lector mínimo de glTF binario (.glb) para las maquetas de las consolas:
// solo lo que usan esos modelos (triángulos, posiciones, coordenadas de
// textura, colores por vértice, el color base del material y su textura,
// y las transformaciones de los nodos).

// glbTri es un triángulo leído: sus puntos, su intensidad (la luminancia del
// material y del color de sus vértices), ese color (sRGB, 0..1) y, si tiene
// textura, sus coordenadas en ella.
type glbTri struct {
	p    [3]v3
	tone float64
	col  [3]float64
	uv   [3][2]float32
	tex  int // índice en glbMesh.texs, o -1
}

// glbMesh es un modelo leído: sus triángulos y las texturas.
type glbMesh struct {
	tris []glbTri
	texs []*image.RGBA
}

type gltfDoc struct {
	Scene  int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Nodes []struct {
		Mesh        *int      `json:"mesh"`
		Children    []int     `json:"children"`
		Matrix      []float64 `json:"matrix"`
		Translation []float64 `json:"translation"`
		Rotation    []float64 `json:"rotation"`
		Scale       []float64 `json:"scale"`
	} `json:"nodes"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
			Material   *int           `json:"material"`
			Mode       *int           `json:"mode"`
		} `json:"primitives"`
	} `json:"meshes"`
	Materials []struct {
		PBR struct {
			BaseColorFactor  []float64 `json:"baseColorFactor"`
			BaseColorTexture *struct {
				Index    int `json:"index"`
				TexCoord int `json:"texCoord"`
			} `json:"baseColorTexture"`
		} `json:"pbrMetallicRoughness"`
		Emissive []float64 `json:"emissiveFactor"`
	} `json:"materials"`
	Textures []struct {
		Source *int `json:"source"`
	} `json:"textures"`
	Images []struct {
		BufferView *int `json:"bufferView"`
	} `json:"images"`
	Accessors []struct {
		BufferView    *int   `json:"bufferView"`
		ByteOffset    int    `json:"byteOffset"`
		ComponentType int    `json:"componentType"`
		Normalized    bool   `json:"normalized"`
		Count         int    `json:"count"`
		Type          string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
}

// mat4 es una matriz 4×4 por columnas, como en glTF.
type mat4 [16]float64

func identity4() mat4 { return mat4{0: 1, 5: 1, 10: 1, 15: 1} }

func (a mat4) mul(b mat4) mat4 {
	var r mat4
	for c := 0; c < 4; c++ {
		for row := 0; row < 4; row++ {
			var s float64
			for k := 0; k < 4; k++ {
				s += a[k*4+row] * b[c*4+k]
			}
			r[c*4+row] = s
		}
	}
	return r
}

func (a mat4) apply(p v3) v3 {
	return v3{
		a[0]*p.x + a[4]*p.y + a[8]*p.z + a[12],
		a[1]*p.x + a[5]*p.y + a[9]*p.z + a[13],
		a[2]*p.x + a[6]*p.y + a[10]*p.z + a[14],
	}
}

// trs arma la matriz de traslación, rotación (cuaternión x, y, z, w) y
// escala.
func trs(t, q, s []float64) mat4 {
	tx, ty, tz := 0.0, 0.0, 0.0
	if len(t) == 3 {
		tx, ty, tz = t[0], t[1], t[2]
	}
	x, y, z, w := 0.0, 0.0, 0.0, 1.0
	if len(q) == 4 {
		x, y, z, w = q[0], q[1], q[2], q[3]
	}
	sx, sy, sz := 1.0, 1.0, 1.0
	if len(s) == 3 {
		sx, sy, sz = s[0], s[1], s[2]
	}
	return mat4{
		(1 - 2*(y*y+z*z)) * sx, (2 * (x*y + z*w)) * sx, (2 * (x*z - y*w)) * sx, 0,
		(2 * (x*y - z*w)) * sy, (1 - 2*(x*x+z*z)) * sy, (2 * (y*z + x*w)) * sy, 0,
		(2 * (x*z + y*w)) * sz, (2 * (y*z - x*w)) * sz, (1 - 2*(x*x+y*y)) * sz, 0,
		tx, ty, tz, 1,
	}
}

// lumaLin es la luminancia de un color lineal (0..1).
func lumaLin(r, g, b float64) float64 { return 0.2126*r + 0.7152*g + 0.0722*b }

// parseGLB lee un .glb.
func parseGLB(b []byte) (*glbMesh, error) {
	if len(b) < 20 || string(b[:4]) != "glTF" {
		return nil, errors.New("not a glb file")
	}
	var doc gltfDoc
	var bin []byte
	for off := 12; off+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[off:]))
		kind := string(b[off+4 : off+8])
		if off+8+n > len(b) {
			return nil, errors.New("truncated glb chunk")
		}
		chunk := b[off+8 : off+8+n]
		switch kind {
		case "JSON":
			if err := json.Unmarshal(chunk, &doc); err != nil {
				return nil, err
			}
		case "BIN\x00":
			bin = chunk
		}
		off += 8 + n
	}
	view := func(i int) ([]byte, int, error) {
		if i < 0 || i >= len(doc.BufferViews) {
			return nil, 0, fmt.Errorf("bad buffer view %d", i)
		}
		v := doc.BufferViews[i]
		if v.ByteOffset+v.ByteLength > len(bin) {
			return nil, 0, errors.New("buffer view out of range")
		}
		return bin[v.ByteOffset : v.ByteOffset+v.ByteLength], v.ByteStride, nil
	}
	// floats lee un accessor como floats (comp por elemento).
	floats := func(ai, comp int) ([]float64, error) {
		if ai < 0 || ai >= len(doc.Accessors) {
			return nil, fmt.Errorf("bad accessor %d", ai)
		}
		a := doc.Accessors[ai]
		ncomp := map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4}[a.Type]
		if ncomp == 0 || a.BufferView == nil {
			return nil, fmt.Errorf("unsupported accessor %d", ai)
		}
		data, stride, err := view(*a.BufferView)
		if err != nil {
			return nil, err
		}
		size := map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}[a.ComponentType]
		if size == 0 {
			return nil, fmt.Errorf("unsupported component type %d", a.ComponentType)
		}
		if stride == 0 {
			stride = size * ncomp
		}
		out := make([]float64, 0, a.Count*comp)
		for i := 0; i < a.Count; i++ {
			base := a.ByteOffset + i*stride
			if base+size*ncomp > len(data) {
				return nil, errors.New("accessor out of range")
			}
			for c := 0; c < comp; c++ {
				if c >= ncomp {
					out = append(out, 1) // el alfa que falta
					continue
				}
				p := data[base+c*size:]
				var v float64
				switch a.ComponentType {
				case 5126:
					v = float64(math.Float32frombits(binary.LittleEndian.Uint32(p)))
				case 5125:
					v = float64(binary.LittleEndian.Uint32(p))
				case 5123:
					v = float64(binary.LittleEndian.Uint16(p))
					if a.Normalized {
						v /= 65535
					}
				case 5122:
					v = float64(int16(binary.LittleEndian.Uint16(p)))
					if a.Normalized {
						v = math.Max(v/32767, -1)
					}
				case 5121:
					v = float64(p[0])
					if a.Normalized {
						v /= 255
					}
				case 5120:
					v = float64(int8(p[0]))
					if a.Normalized {
						v = math.Max(v/127, -1)
					}
				}
				out = append(out, v)
			}
		}
		return out, nil
	}

	m := &glbMesh{}
	// Las texturas, en luminancia (una por imagen).
	texOf := map[int]int{} // índice de imagen → índice en m.texs
	texture := func(ti int) int {
		if ti < 0 || ti >= len(doc.Textures) || doc.Textures[ti].Source == nil {
			return -1
		}
		src := *doc.Textures[ti].Source
		if k, ok := texOf[src]; ok {
			return k
		}
		texOf[src] = -1
		if src < 0 || src >= len(doc.Images) || doc.Images[src].BufferView == nil {
			return -1
		}
		data, _, err := view(*doc.Images[src].BufferView)
		if err != nil {
			return -1
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return -1
		}
		g := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(g, g.Bounds(), img, img.Bounds().Min, draw.Src)
		texOf[src] = len(m.texs)
		m.texs = append(m.texs, g)
		return texOf[src]
	}

	var walk func(n int, parent mat4, depth int) error
	walk = func(n int, parent mat4, depth int) error {
		if n < 0 || n >= len(doc.Nodes) || depth > 32 {
			return fmt.Errorf("bad node %d", n)
		}
		node := doc.Nodes[n]
		local := trs(node.Translation, node.Rotation, node.Scale)
		if len(node.Matrix) == 16 {
			copy(local[:], node.Matrix)
		}
		world := parent.mul(local)
		if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(doc.Meshes) {
			for _, prim := range doc.Meshes[*node.Mesh].Primitives {
				if prim.Mode != nil && *prim.Mode != 4 {
					continue // solo triángulos
				}
				if err := m.addPrimitive(&doc, prim.Attributes, prim.Indices, prim.Material, world, floats, texture); err != nil {
					return err
				}
			}
		}
		for _, c := range node.Children {
			if err := walk(c, world, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	var roots []int
	if doc.Scene < len(doc.Scenes) {
		roots = doc.Scenes[doc.Scene].Nodes
	}
	if roots == nil {
		for i := range doc.Nodes {
			roots = append(roots, i)
		}
	}
	for _, r := range roots {
		if err := walk(r, identity4(), 0); err != nil {
			return nil, err
		}
	}
	if len(m.tris) == 0 {
		return nil, errors.New("glb without triangles")
	}
	return m, nil
}

// addPrimitive suma los triángulos de una primitiva.
func (m *glbMesh) addPrimitive(doc *gltfDoc, attrs map[string]int, indices, material *int, world mat4,
	floats func(ai, comp int) ([]float64, error), texture func(int) int) error {
	pi, ok := attrs["POSITION"]
	if !ok {
		return nil
	}
	pos, err := floats(pi, 3)
	if err != nil {
		return err
	}
	nv := len(pos) / 3
	var idx []int
	if indices != nil {
		f, err := floats(*indices, 1)
		if err != nil {
			return err
		}
		for _, v := range f {
			idx = append(idx, int(v))
		}
	} else {
		for i := 0; i < nv; i++ {
			idx = append(idx, i)
		}
	}
	base := [3]float64{1, 1, 1}
	tex := -1
	texCoord := 0
	if material != nil && *material >= 0 && *material < len(doc.Materials) {
		mt := doc.Materials[*material]
		if f := mt.PBR.BaseColorFactor; len(f) >= 3 {
			base = [3]float64{f[0], f[1], f[2]}
		}
		if t := mt.PBR.BaseColorTexture; t != nil {
			tex, texCoord = texture(t.Index), t.TexCoord
		}
	}
	var uv []float64
	if tex >= 0 {
		if ui, ok := attrs[fmt.Sprintf("TEXCOORD_%d", texCoord)]; ok {
			if uv, err = floats(ui, 2); err != nil {
				return err
			}
		}
		if len(uv) < nv*2 {
			tex, uv = -1, nil
		}
	}
	var col []float64
	if ci, ok := attrs["COLOR_0"]; ok {
		if col, err = floats(ci, 4); err != nil {
			return err
		}
		if len(col) < nv*4 {
			col = nil
		}
	}
	world3 := make([]v3, nv)
	for i := range world3 {
		world3[i] = world.apply(v3{pos[i*3], pos[i*3+1], pos[i*3+2]})
	}
	// Una matriz que da la vuelta (escala negativa) invierte el sentido.
	flip := world.det3() < 0
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := idx[i], idx[i+1], idx[i+2]
		if a >= nv || b >= nv || c >= nv {
			return errors.New("index out of range")
		}
		if flip {
			b, c = c, b
		}
		t := glbTri{p: [3]v3{world3[a], world3[b], world3[c]}, tex: tex}
		cr, cg, cb := base[0], base[1], base[2]
		if col != nil {
			var r, g, bl float64
			for _, k := range [3]int{a, b, c} {
				r += col[k*4] / 3
				g += col[k*4+1] / 3
				bl += col[k*4+2] / 3
			}
			cr, cg, cb = cr*r, cg*g, cb*bl
		}
		t.tone = linearToSrgb(lumaLin(cr, cg, cb))
		t.col = [3]float64{linearToSrgb(cr), linearToSrgb(cg), linearToSrgb(cb)}
		if tex >= 0 {
			for k, v := range [3]int{a, b, c} {
				t.uv[k] = [2]float32{float32(uv[v*2]), float32(uv[v*2+1])}
			}
		}
		m.tris = append(m.tris, t)
	}
	return nil
}

// det3 es el determinante de la parte 3×3 de la matriz.
func (a mat4) det3() float64 {
	return a[0]*(a[5]*a[10]-a[9]*a[6]) - a[4]*(a[1]*a[10]-a[9]*a[2]) + a[8]*(a[1]*a[6]-a[5]*a[2])
}

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSrgb(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}
