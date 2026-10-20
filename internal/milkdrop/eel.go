package milkdrop

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// El lenguaje de las ecuaciones de MilkDrop es un subconjunto de EEL (el de
// ns-eel de Nullsoft): números de coma flotante, variables sin declarar (que
// empiezan en 0 y no distinguen mayúsculas), los operadores de C, asignación
// y secuencias con ';', y unas cuantas funciones. Aquí se compila a cierres
// (una función de Go por nodo) que leen y escriben punteros a las variables:
// evaluar no crea memoria, solo compilar.
//
// Lo que se sale de lo de siempre en EEL y se respeta aquí:
//
//   - Dividir por cero da 0, y el resto (%) es entero y también da 0.
//   - Los booleanos son 0 y 1; cualquier valor con |x| > 0,00001 es verdad,
//     y == compara con ese margen.
//   - sqrt y log toman el valor absoluto (sqrt(-4) = 2), como ns-eel.
//   - rand(n) es un entero al azar de 0 a n-1 (con n al menos 1).
//   - megabuf(i) y gmegabuf(i) son memorias de un millón de casillas (la del
//     contexto y la global), que se crean por bloques al escribir en ellas.

// epsilon es el margen de los booleanos y de ==.
const epsilon = 0.00001

// megaSize es el tamaño de megabuf y gmegabuf, y megaBlock lo que se crea de
// una vez al escribir en una casilla nueva.
const (
	megaSize  = 1 << 20
	megaBlock = 1 << 16
)

// loopMax es el tope de vueltas de loop() y while(): una ecuación mal hecha
// no debe colgar la interfaz.
const loopMax = 1 << 16

// megabuf es una memoria de megaSize casillas, por bloques.
type megabuf struct {
	blocks [megaSize / megaBlock][]float64
}

// index pasa un valor de EEL a una casilla (-1 si se sale).
func megaIndex(v float64) int {
	i := int(v + epsilon)
	if v < 0 || i >= megaSize {
		return -1
	}
	return i
}

// get lee la casilla i (0 si no existe).
func (m *megabuf) get(v float64) float64 {
	i := megaIndex(v)
	if i < 0 {
		return 0
	}
	b := m.blocks[i/megaBlock]
	if b == nil {
		return 0
	}
	return b[i%megaBlock]
}

// at es la casilla i para escribir (crea su bloque la primera vez); fuera de
// rango, una casilla de descarte.
func (m *megabuf) at(v float64, junk *float64) *float64 {
	i := megaIndex(v)
	if i < 0 {
		return junk
	}
	b := &m.blocks[i/megaBlock]
	if *b == nil {
		*b = make([]float64, megaBlock)
	}
	return &(*b)[i%megaBlock]
}

// Globals es lo que comparten todos los contextos (y todos los presets):
// reg00..reg99 y gmegabuf.
type Globals struct {
	Reg   [100]float64
	gmega megabuf
	rng   uint64
}

// NewGlobals crea las variables globales, con la semilla del azar.
func NewGlobals(seed uint64) *Globals {
	return &Globals{rng: seed | 1}
}

// rand es un número al azar de 0 a 1 (xorshift: no crea memoria).
func (g *Globals) rand() float64 {
	g.rng ^= g.rng << 13
	g.rng ^= g.rng >> 7
	g.rng ^= g.rng << 17
	return float64(g.rng>>11) / (1 << 53)
}

// Scope son las variables de un contexto (las ecuaciones por fotograma de un
// preset, las de cada vértice, las de una onda...): cada nombre, en minúsculas,
// apunta a su valor, que vive mientras viva el contexto.
type Scope struct {
	vars map[string]*float64
	g    *Globals
	mega megabuf
	junk float64
}

// NewScope crea un contexto vacío que comparte g.
func NewScope(g *Globals) *Scope {
	return &Scope{vars: map[string]*float64{}, g: g}
}

// Var es el puntero a la variable name (la crea a 0 si no existía). Las regNN
// son las globales.
func (s *Scope) Var(name string) *float64 {
	name = strings.ToLower(name)
	if len(name) == 5 && strings.HasPrefix(name, "reg") && name[3] >= '0' && name[3] <= '9' && name[4] >= '0' && name[4] <= '9' {
		return &s.g.Reg[int(name[3]-'0')*10+int(name[4]-'0')]
	}
	if p, ok := s.vars[name]; ok {
		return p
	}
	p := new(float64)
	s.vars[name] = p
	return p
}

// Get es el valor de una variable (0 si no existe), para las pruebas.
func (s *Scope) Get(name string) float64 {
	return *s.Var(name)
}

// Set pone el valor de una variable.
func (s *Scope) Set(name string, v float64) {
	*s.Var(name) = v
}

// Code es una ecuación compilada.
type Code struct {
	fn func() float64
}

// Run evalúa la ecuación (nada si está vacía) y devuelve su último valor.
func (c *Code) Run() float64 {
	if c == nil || c.fn == nil {
		return 0
	}
	return c.fn()
}

// Empty dice si no hay nada que evaluar.
func (c *Code) Empty() bool { return c == nil || c.fn == nil }

// Compile compila src en el contexto s.
func Compile(src string, s *Scope) (*Code, error) {
	p := &parser{lx: lexer{src: src}}
	p.next()
	n, err := p.program()
	if err != nil {
		return nil, err
	}
	if n == nil {
		return &Code{}, nil
	}
	fn, err := s.compile(n)
	if err != nil {
		return nil, err
	}
	return &Code{fn: fn}, nil
}

// El analizador léxico.

// tokKind es la clase de una pieza.
type tokKind int

const (
	tokEOF tokKind = iota
	tokNum
	tokIdent
	tokOp
)

// token es una pieza del texto.
type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
}

// lexer parte el texto en piezas.
type lexer struct {
	src string
	pos int
}

// ops son los operadores, los largos primero.
var ops = []string{"&&", "||", "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=", "%=", "^=", "|=", "&=",
	"+", "-", "*", "/", "%", "^", "|", "&", "!", "<", ">", "=", "(", ")", ",", ";", "?", ":"}

// next es la pieza siguiente.
func (l *lexer) next() (token, error) {
	// Los espacios y los comentarios (// hasta el final de línea, /* */).
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case strings.HasPrefix(l.src[l.pos:], "//"):
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case strings.HasPrefix(l.src[l.pos:], "/*"):
			end := strings.Index(l.src[l.pos+2:], "*/")
			if end < 0 {
				l.pos = len(l.src)
			} else {
				l.pos += end + 4
			}
		default:
			goto tok
		}
	}
	return token{kind: tokEOF, pos: l.pos}, nil
tok:
	start := l.pos
	c := l.src[l.pos]
	switch {
	case isDigit(c) || (c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
		if strings.HasPrefix(l.src[l.pos:], "0x") || strings.HasPrefix(l.src[l.pos:], "0X") {
			l.pos += 2
			for l.pos < len(l.src) && isHex(l.src[l.pos]) {
				l.pos++
			}
			v, err := strconv.ParseUint(l.src[start+2:l.pos], 16, 64)
			if err != nil {
				return token{}, fmt.Errorf("bad number %q at %d", l.src[start:l.pos], start)
			}
			return token{kind: tokNum, num: float64(v), pos: start}, nil
		}
		for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
			l.pos++
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
			p := l.pos + 1
			if p < len(l.src) && (l.src[p] == '+' || l.src[p] == '-') {
				p++
			}
			if p < len(l.src) && isDigit(l.src[p]) {
				l.pos = p
				for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
					l.pos++
				}
			}
		}
		text := l.src[start:l.pos]
		// "1." y "1.5.2" (esto último lo acepta ns-eel hasta el segundo punto).
		if i := strings.Index(text, "."); i >= 0 {
			if j := strings.Index(text[i+1:], "."); j >= 0 {
				text = text[:i+1+j]
			}
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(text, "."), 64)
		if err != nil {
			return token{}, fmt.Errorf("bad number %q at %d", text, start)
		}
		return token{kind: tokNum, num: v, pos: start}, nil
	case isIdentStart(c):
		for l.pos < len(l.src) && (isIdentStart(l.src[l.pos]) || isDigit(l.src[l.pos])) {
			l.pos++
		}
		return token{kind: tokIdent, text: strings.ToLower(l.src[start:l.pos]), pos: start}, nil
	}
	for _, op := range ops {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.pos += len(op)
			return token{kind: tokOp, text: op, pos: start}, nil
		}
	}
	return token{}, fmt.Errorf("unexpected %q at %d", c, start)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool   { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }
func isIdentStart(c byte) bool {
	return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') || c == '$' || c == '#'
}

// El árbol.

// nodeKind es la clase de un nodo.
type nodeKind int

const (
	nNum nodeKind = iota
	nVar
	nUnary
	nBinary
	nAssign
	nCall
	nSeq
	nCond
)

// node es un nodo del árbol de una ecuación.
type node struct {
	kind nodeKind
	op   string
	num  float64
	name string
	kids []*node
}

// parser es un analizador de precedencia.
type parser struct {
	lx  lexer
	tok token
	err error
}

// next avanza una pieza.
func (p *parser) next() {
	if p.err != nil {
		return
	}
	p.tok, p.err = p.lx.next()
}

// is dice si la pieza actual es el operador op.
func (p *parser) is(op string) bool {
	return p.tok.kind == tokOp && p.tok.text == op
}

// expect consume el operador op o falla.
func (p *parser) expect(op string) error {
	if !p.is(op) {
		return p.fail("expected %q", op)
	}
	p.next()
	return p.err
}

// fail es un error en la pieza actual.
func (p *parser) fail(f string, args ...any) error {
	if p.err != nil {
		return p.err
	}
	what := p.tok.text
	if p.tok.kind == tokEOF {
		what = "end"
	} else if p.tok.kind == tokNum {
		what = strconv.FormatFloat(p.tok.num, 'g', -1, 64)
	}
	return fmt.Errorf("%s at %d (near %q)", fmt.Sprintf(f, args...), p.tok.pos, what)
}

// program es una secuencia hasta el final del texto.
func (p *parser) program() (*node, error) {
	n, err := p.seq()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokEOF {
		return nil, p.fail("unexpected")
	}
	return n, nil
}

// startsExpr dice si la pieza actual puede empezar una expresión (para las
// líneas que olvidan el ';' final: se toman como otra sentencia).
func (p *parser) startsExpr() bool {
	switch p.tok.kind {
	case tokNum, tokIdent:
		return true
	case tokOp:
		return p.tok.text == "(" || p.tok.text == "!" || p.tok.text == "-" || p.tok.text == "+"
	}
	return false
}

// seq son expresiones separadas por ';' (vacías incluidas); vale la última.
func (p *parser) seq() (*node, error) {
	var kids []*node
	for {
		for p.is(";") {
			p.next()
		}
		if p.err != nil {
			return nil, p.err
		}
		if !p.startsExpr() {
			break
		}
		e, err := p.assign()
		if err != nil {
			return nil, err
		}
		kids = append(kids, e)
		if !p.is(";") && !p.startsExpr() {
			break
		}
	}
	switch len(kids) {
	case 0:
		return nil, p.err
	case 1:
		return kids[0], p.err
	}
	return &node{kind: nSeq, kids: kids}, p.err
}

// assign es la asignación (=, +=...), que va de derecha a izquierda.
func (p *parser) assign() (*node, error) {
	l, err := p.cond()
	if err != nil {
		return nil, err
	}
	if p.tok.kind == tokOp {
		switch p.tok.text {
		case "=", "+=", "-=", "*=", "/=", "%=", "^=", "|=", "&=":
			op := p.tok.text
			if !(l.kind == nVar || (l.kind == nCall && (l.name == "megabuf" || l.name == "gmegabuf") && len(l.kids) == 1)) {
				return nil, p.fail("cannot assign")
			}
			p.next()
			r, err := p.assign()
			if err != nil {
				return nil, err
			}
			return &node{kind: nAssign, op: op, kids: []*node{l, r}}, nil
		}
	}
	return l, nil
}

// cond es c ? a : b.
func (p *parser) cond() (*node, error) {
	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.is("?") {
		return c, nil
	}
	p.next()
	a, err := p.assign()
	if err != nil {
		return nil, err
	}
	var b *node
	if p.is(":") {
		p.next()
		if b, err = p.assign(); err != nil {
			return nil, err
		}
	} else {
		b = &node{kind: nNum}
	}
	return &node{kind: nCond, kids: []*node{c, a, b}}, nil
}

// levels son los operadores binarios, de menos a más precedencia.
var levels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", ">", "<=", ">="},
	{"|", "&"},
	{"+", "-"},
	{"*", "/", "%"},
}

// binary es un nivel de operadores binarios (de izquierda a derecha).
func (p *parser) binary(level int) (*node, error) {
	if level == len(levels) {
		return p.unary()
	}
	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp && contains(levels[level], p.tok.text) {
		op := p.tok.text
		p.next()
		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		l = &node{kind: nBinary, op: op, kids: []*node{l, r}}
	}
	return l, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// unary es -x, +x y !x.
func (p *parser) unary() (*node, error) {
	if p.is("-") || p.is("+") || p.is("!") {
		op := p.tok.text
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			return x, nil
		}
		return &node{kind: nUnary, op: op, kids: []*node{x}}, nil
	}
	return p.power()
}

// power es x^y, que va de derecha a izquierda.
func (p *parser) power() (*node, error) {
	b, err := p.primary()
	if err != nil {
		return nil, err
	}
	if p.is("^") {
		p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &node{kind: nBinary, op: "^", kids: []*node{b, e}}, nil
	}
	return b, nil
}

// primary es un número, una variable, una llamada o un paréntesis.
func (p *parser) primary() (*node, error) {
	t := p.tok
	switch t.kind {
	case tokNum:
		p.next()
		return &node{kind: nNum, num: t.num}, p.err
	case tokIdent:
		p.next()
		if !p.is("(") {
			return &node{kind: nVar, name: t.text}, p.err
		}
		p.next()
		n := &node{kind: nCall, name: t.text}
		if p.is(")") {
			p.next()
			return n, p.err
		}
		for {
			a, err := p.seq()
			if err != nil {
				return nil, err
			}
			if a == nil {
				return nil, p.fail("expected argument")
			}
			n.kids = append(n.kids, a)
			if p.is(",") {
				p.next()
				continue
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return n, nil
		}
	case tokOp:
		if t.text == "(" {
			p.next()
			n, err := p.seq()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			if n == nil {
				n = &node{kind: nNum}
			}
			return n, nil
		}
	}
	return nil, p.fail("unexpected")
}

// La compilación a cierres.

// fn es una expresión compilada.
type fn = func() float64

// truth pasa un valor a booleano.
func truth(v float64) bool { return math.Abs(v) > epsilon }

// b2f pasa un booleano a 0 o 1.
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// div es la división de EEL: por cero, 0.
func div(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// toInt pasa a entero sin desbordar.
func toInt(v float64) int64 {
	if v != v {
		return 0
	}
	return int64(math.Max(-1e18, math.Min(1e18, v)))
}

// mod es el resto entero de EEL: por cero, 0.
func mod(a, b float64) float64 {
	ib := toInt(b)
	if ib == 0 {
		return 0
	}
	return float64(toInt(a) % ib)
}

// pow es la potencia, sin NaN (una base negativa con exponente no entero da 0).
func pow(a, b float64) float64 {
	v := math.Pow(a, b)
	if v != v {
		return 0
	}
	return v
}

// constant es una expresión que siempre vale v.
func constant(v float64) fn { return func() float64 { return v } }

// isConst dice si n es un número.
func isConst(n *node) bool { return n.kind == nNum }

// binop es la operación de un operador binario (sin los perezosos).
func binop(op string) func(a, b float64) float64 {
	switch op {
	case "+":
		return func(a, b float64) float64 { return a + b }
	case "-":
		return func(a, b float64) float64 { return a - b }
	case "*":
		return func(a, b float64) float64 { return a * b }
	case "/":
		return div
	case "%":
		return mod
	case "^":
		return pow
	case "|":
		return func(a, b float64) float64 { return float64(toInt(a) | toInt(b)) }
	case "&":
		return func(a, b float64) float64 { return float64(toInt(a) & toInt(b)) }
	case "==":
		return func(a, b float64) float64 { return b2f(math.Abs(a-b) < epsilon) }
	case "!=":
		return func(a, b float64) float64 { return b2f(math.Abs(a-b) >= epsilon) }
	case "<":
		return func(a, b float64) float64 { return b2f(a < b) }
	case ">":
		return func(a, b float64) float64 { return b2f(a > b) }
	case "<=":
		return func(a, b float64) float64 { return b2f(a <= b) }
	case ">=":
		return func(a, b float64) float64 { return b2f(a >= b) }
	}
	return nil
}

// compile compila un nodo.
func (s *Scope) compile(n *node) (fn, error) {
	switch n.kind {
	case nNum:
		return constant(n.num), nil
	case nVar:
		switch n.name {
		case "$pi":
			return constant(math.Pi), nil
		case "$e":
			return constant(math.E), nil
		case "$phi":
			return constant(1.618033988749894), nil
		}
		p := s.Var(n.name)
		return func() float64 { return *p }, nil
	case nUnary:
		x, err := s.compile(n.kids[0])
		if err != nil {
			return nil, err
		}
		if n.op == "-" {
			if isConst(n.kids[0]) {
				return constant(-n.kids[0].num), nil
			}
			return func() float64 { return -x() }, nil
		}
		return func() float64 { return b2f(!truth(x())) }, nil
	case nSeq:
		fs := make([]fn, len(n.kids))
		for i, k := range n.kids {
			f, err := s.compile(k)
			if err != nil {
				return nil, err
			}
			fs[i] = f
		}
		head, last := fs[:len(fs)-1], fs[len(fs)-1]
		return func() float64 {
			for _, f := range head {
				f()
			}
			return last()
		}, nil
	case nCond:
		c, err := s.compile(n.kids[0])
		if err != nil {
			return nil, err
		}
		a, err := s.compile(n.kids[1])
		if err != nil {
			return nil, err
		}
		b, err := s.compile(n.kids[2])
		if err != nil {
			return nil, err
		}
		return func() float64 {
			if truth(c()) {
				return a()
			}
			return b()
		}, nil
	case nBinary:
		return s.compileBinary(n)
	case nAssign:
		return s.compileAssign(n)
	case nCall:
		return s.compileCall(n)
	}
	return nil, fmt.Errorf("bad node")
}

// compileBinary compila un operador binario.
func (s *Scope) compileBinary(n *node) (fn, error) {
	l, err := s.compile(n.kids[0])
	if err != nil {
		return nil, err
	}
	r, err := s.compile(n.kids[1])
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "&&":
		return func() float64 { return b2f(truth(l()) && truth(r())) }, nil
	case "||":
		return func() float64 { return b2f(truth(l()) || truth(r())) }, nil
	}
	op := binop(n.op)
	if isConst(n.kids[0]) && isConst(n.kids[1]) {
		return constant(op(n.kids[0].num, n.kids[1].num)), nil
	}
	// Los más comunes, sin la llamada a op (son los que más se evalúan).
	switch n.op {
	case "+":
		return func() float64 { return l() + r() }, nil
	case "-":
		return func() float64 { return l() - r() }, nil
	case "*":
		return func() float64 { return l() * r() }, nil
	case "/":
		return func() float64 { return div(l(), r()) }, nil
	}
	return func() float64 { return op(l(), r()) }, nil
}

// compileAssign compila una asignación, a una variable o a megabuf(i).
func (s *Scope) compileAssign(n *node) (fn, error) {
	r, err := s.compile(n.kids[1])
	if err != nil {
		return nil, err
	}
	var op func(a, b float64) float64
	if n.op != "=" {
		op = binop(n.op[:1])
	}
	lhs := n.kids[0]
	if lhs.kind == nVar {
		p := s.Var(lhs.name)
		if op == nil {
			return func() float64 { v := r(); *p = v; return v }, nil
		}
		return func() float64 { v := op(*p, r()); *p = v; return v }, nil
	}
	idx, err := s.compile(lhs.kids[0])
	if err != nil {
		return nil, err
	}
	m := &s.mega
	if lhs.name == "gmegabuf" {
		m = &s.g.gmega
	}
	junk := &s.junk
	return func() float64 {
		p := m.at(idx(), junk)
		v := r()
		if op != nil {
			v = op(*p, v)
		}
		*p = v
		return v
	}, nil
}

// funcs1 son las funciones de un argumento.
var funcs1 = map[string]func(float64) float64{
	"sin":  math.Sin,
	"cos":  math.Cos,
	"tan":  math.Tan,
	"asin": func(x float64) float64 { return math.Asin(math.Max(-1, math.Min(1, x))) },
	"acos": func(x float64) float64 { return math.Acos(math.Max(-1, math.Min(1, x))) },
	"atan": math.Atan,
	"sqrt": func(x float64) float64 { return math.Sqrt(math.Abs(x)) },
	"exp":  math.Exp,
	"log": func(x float64) float64 {
		if x == 0 {
			return 0
		}
		return math.Log(math.Abs(x))
	},
	"log10": func(x float64) float64 {
		if x == 0 {
			return 0
		}
		return math.Log10(math.Abs(x))
	},
	"abs": math.Abs,
	"sign": func(x float64) float64 {
		switch {
		case x > 0:
			return 1
		case x < 0:
			return -1
		}
		return 0
	},
	"int":   func(x float64) float64 { return float64(toInt(x)) },
	"floor": math.Floor,
	"ceil":  math.Ceil,
	"sqr":   func(x float64) float64 { return x * x },
	"invsqrt": func(x float64) float64 {
		if x == 0 {
			return 0
		}
		return 1 / math.Sqrt(math.Abs(x))
	},
	"bnot":     func(x float64) float64 { return b2f(!truth(x)) },
	"freembuf": func(x float64) float64 { return x },
}

// funcs2 son las de dos argumentos (que se evalúan los dos).
var funcs2 = map[string]func(a, b float64) float64{
	"atan2": math.Atan2,
	"pow":   pow,
	"min":   math.Min,
	"max":   math.Max,
	"sigmoid": func(x, k float64) float64 {
		t := 1 + math.Exp(-x*k)
		if math.Abs(t) > 0.00001 {
			return 1 / t
		}
		return 0
	},
	"above": func(a, b float64) float64 { return b2f(a > b) },
	"below": func(a, b float64) float64 { return b2f(a < b) },
	"equal": func(a, b float64) float64 { return b2f(math.Abs(a-b) < epsilon) },
}

// compileCall compila una llamada a una función.
func (s *Scope) compileCall(n *node) (fn, error) {
	args := make([]fn, len(n.kids))
	allConst := true
	for i, k := range n.kids {
		f, err := s.compile(k)
		if err != nil {
			return nil, err
		}
		args[i] = f
		allConst = allConst && isConst(k)
	}
	want := func(k int) error {
		if len(args) != k {
			return fmt.Errorf("%s() takes %d arguments, got %d", n.name, k, len(args))
		}
		return nil
	}
	if f, ok := funcs1[n.name]; ok {
		if err := want(1); err != nil {
			return nil, err
		}
		if allConst {
			return constant(f(n.kids[0].num)), nil
		}
		a := args[0]
		return func() float64 { return f(a()) }, nil
	}
	if f, ok := funcs2[n.name]; ok {
		if err := want(2); err != nil {
			return nil, err
		}
		if allConst {
			return constant(f(n.kids[0].num, n.kids[1].num)), nil
		}
		a, b := args[0], args[1]
		return func() float64 { return f(a(), b()) }, nil
	}
	switch n.name {
	case "if":
		if err := want(3); err != nil {
			return nil, err
		}
		c, a, b := args[0], args[1], args[2]
		return func() float64 {
			if truth(c()) {
				return a()
			}
			return b()
		}, nil
	case "band":
		if err := want(2); err != nil {
			return nil, err
		}
		a, b := args[0], args[1]
		return func() float64 { return b2f(truth(a()) && truth(b())) }, nil
	case "bor":
		if err := want(2); err != nil {
			return nil, err
		}
		a, b := args[0], args[1]
		return func() float64 { return b2f(truth(a()) || truth(b())) }, nil
	case "rand":
		if err := want(1); err != nil {
			return nil, err
		}
		a, g := args[0], s.g
		return func() float64 {
			n := math.Max(1, math.Floor(a()))
			return math.Floor(g.rand() * n)
		}, nil
	case "exec2", "exec3":
		if len(args) < 1 {
			return nil, fmt.Errorf("%s() needs arguments", n.name)
		}
		head, last := args[:len(args)-1], args[len(args)-1]
		return func() float64 {
			for _, f := range head {
				f()
			}
			return last()
		}, nil
	case "loop":
		if err := want(2); err != nil {
			return nil, err
		}
		cnt, body := args[0], args[1]
		return func() float64 {
			k := min(toInt(cnt()), loopMax)
			v := 0.0
			for i := int64(0); i < k; i++ {
				v = body()
			}
			return v
		}, nil
	case "while":
		if err := want(1); err != nil {
			return nil, err
		}
		body := args[0]
		return func() float64 {
			for i := 0; i < loopMax && truth(body()); i++ {
			}
			return 0
		}, nil
	case "megabuf", "gmegabuf":
		if err := want(1); err != nil {
			return nil, err
		}
		m := &s.mega
		if n.name == "gmegabuf" {
			m = &s.g.gmega
		}
		idx := args[0]
		return func() float64 { return m.get(idx()) }, nil
	}
	return nil, fmt.Errorf("unknown function %s()", n.name)
}
