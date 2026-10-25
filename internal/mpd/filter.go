package mpd

import (
	"errors"
	"regexp"
	"strings"
)

// Tags son las etiquetas de una canción, como las nombra MPD.
type Tags struct {
	Title, Artist, AlbumArtist, Album, Genre, Date, Track string
}

// tagNames son las etiquetas que se sirven, con su nombre en MPD.
var tagNames = []string{"Artist", "AlbumArtist", "Album", "Title", "Track", "Genre", "Date"}

// canonTag es el nombre de MPD de una etiqueta (sin distinguir mayúsculas),
// o "" si no se conoce.
func canonTag(name string) string {
	for _, t := range tagNames {
		if strings.EqualFold(t, name) {
			return t
		}
	}
	switch strings.ToLower(name) {
	case "file", "base", "any", "modified-since":
		return strings.ToLower(name)
	}
	return ""
}

// get es el valor de la etiqueta tag (con su nombre de MPD). Sin
// AlbumArtist vale el Artist, como en MPD.
func (t Tags) get(tag string) string {
	switch tag {
	case "Artist":
		return t.Artist
	case "AlbumArtist":
		if t.AlbumArtist == "" {
			return t.Artist
		}
		return t.AlbumArtist
	case "Album":
		return t.Album
	case "Title":
		return t.Title
	case "Track":
		return t.Track
	case "Genre":
		return t.Genre
	case "Date":
		return t.Date
	}
	return ""
}

// set pone la etiqueta tag (addtagid).
func (t *Tags) set(tag, v string) {
	switch tag {
	case "Artist":
		t.Artist = v
	case "AlbumArtist":
		t.AlbumArtist = v
	case "Album":
		t.Album = v
	case "Title":
		t.Title = v
	case "Track":
		t.Track = v
	case "Genre":
		t.Genre = v
	case "Date":
		t.Date = v
	}
}

// filter dice si una canción (su URI y etiquetas) cumple una condición.
type filter func(uri string, t Tags) bool

// matchAll no filtra nada.
func matchAll(string, Tags) bool { return true }

// valueOf es el valor de tag en una canción ("file": su URI).
func valueOf(tag, uri string, t Tags) []string {
	switch tag {
	case "file":
		return []string{uri}
	case "any":
		return []string{uri, t.Artist, t.AlbumArtist, t.Album, t.Title, t.Genre, t.Date}
	}
	return []string{t.get(tag)}
}

// compare arma la condición "tag op value"; fold es la de search (sin
// distinguir mayúsculas y "contiene" en vez de "igual").
func compare(tag, op, value string, fold bool) (filter, error) {
	if tag == "" {
		return nil, errors.New("unknown tag")
	}
	if tag == "base" {
		base := strings.TrimSuffix(value, "/")
		return func(uri string, _ Tags) bool {
			return base == "" || uri == base || strings.HasPrefix(uri, base+"/")
		}, nil
	}
	if tag == "modified-since" {
		return matchAll, nil
	}
	if fold {
		value = strings.ToLower(value)
	}
	var test func(string) bool
	switch op {
	case "==", "!=":
		test = func(v string) bool {
			if fold {
				return strings.Contains(strings.ToLower(v), value)
			}
			return v == value
		}
	case "contains":
		test = func(v string) bool {
			if fold {
				return strings.Contains(strings.ToLower(v), value)
			}
			return strings.Contains(v, value)
		}
	case "starts_with":
		test = func(v string) bool {
			if fold {
				return strings.HasPrefix(strings.ToLower(v), value)
			}
			return strings.HasPrefix(v, value)
		}
	case "=~", "!~":
		expr := value
		if fold {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, err
		}
		test = re.MatchString
	default:
		return nil, errors.New("unknown operator " + op)
	}
	neg := op == "!=" || op == "!~"
	return func(uri string, t Tags) bool {
		hit := false
		for _, v := range valueOf(tag, uri, t) {
			if test(v) {
				hit = true
				break
			}
		}
		return hit != neg
	}, nil
}

// parseFilter lee los filtros de find/search/list/count: una expresión
// "(Artist == 'x')" o pares viejos "artist x album y". Devuelve lo que
// sobra (sort, window, group...).
func parseFilter(args []string, fold bool) (filter, []string, error) {
	var parts []filter
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(strings.TrimSpace(a), "(") {
			f, err := parseExpr(a, fold)
			if err != nil {
				return nil, nil, err
			}
			parts = append(parts, f)
			i++
			continue
		}
		switch strings.ToLower(a) {
		case "sort", "window", "group", "position":
			return joinAnd(parts), args[i:], nil
		}
		if i+1 >= len(args) {
			return nil, nil, errors.New("missing filter value")
		}
		f, err := compare(canonTag(a), "==", args[i+1], fold)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, f)
		i += 2
	}
	return joinAnd(parts), nil, nil
}

// joinAnd es la condición de que se cumplan todas.
func joinAnd(parts []filter) filter {
	if len(parts) == 0 {
		return matchAll
	}
	return func(uri string, t Tags) bool {
		for _, f := range parts {
			if !f(uri, t) {
				return false
			}
		}
		return true
	}
}

// parseExpr lee una expresión de filtro de MPD 0.21+.
func parseExpr(s string, fold bool) (filter, error) {
	p := &exprParser{s: s, fold: fold}
	f, err := p.expr()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, errors.New("trailing text in filter")
	}
	return f, nil
}

// exprParser lee expresiones como "((Artist == 'a') AND (!(Album == 'b')))".
type exprParser struct {
	s    string
	i    int
	fold bool
}

func (p *exprParser) space() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

// eat se come lit si viene.
func (p *exprParser) eat(lit string) bool {
	p.space()
	if strings.HasPrefix(p.s[p.i:], lit) {
		p.i += len(lit)
		return true
	}
	return false
}

// word lee una palabra (una etiqueta o un operador).
func (p *exprParser) word() string {
	p.space()
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(" ()'\"", rune(p.s[p.i])) {
		p.i++
	}
	return p.s[start:p.i]
}

// quoted lee un valor entre comillas (simples o dobles, con \ de escape).
func (p *exprParser) quoted() (string, error) {
	p.space()
	if p.i >= len(p.s) || (p.s[p.i] != '"' && p.s[p.i] != '\'') {
		return "", errors.New("expected a quoted value")
	}
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == '\\' && p.i < len(p.s):
			b.WriteByte(p.s[p.i])
			p.i++
		case c == q:
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", errors.New("unterminated value")
}

// expr lee una expresión entre paréntesis.
func (p *exprParser) expr() (filter, error) {
	if !p.eat("(") {
		return nil, errors.New("expected (")
	}
	if p.eat("!") {
		inner, err := p.expr()
		if err != nil {
			return nil, err
		}
		if !p.eat(")") {
			return nil, errors.New("expected )")
		}
		return func(uri string, t Tags) bool { return !inner(uri, t) }, nil
	}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '(' {
		// Una conjunción: (expr AND expr ...).
		var parts []filter
		for {
			f, err := p.expr()
			if err != nil {
				return nil, err
			}
			parts = append(parts, f)
			if p.eat(")") {
				return joinAnd(parts), nil
			}
			if !p.eat("AND") {
				return nil, errors.New("expected AND")
			}
		}
	}
	tag := p.word()
	switch strings.ToLower(tag) {
	case "base", "modified-since":
		v, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if !p.eat(")") {
			return nil, errors.New("expected )")
		}
		return compare(strings.ToLower(tag), "", v, false)
	}
	op := p.word()
	v, err := p.quoted()
	if err != nil {
		return nil, err
	}
	if !p.eat(")") {
		return nil, errors.New("expected )")
	}
	// En search no se distinguen mayúsculas (las regex sí, como se escriben).
	return compare(canonTag(tag), op, v, p.fold && op != "=~" && op != "!~")
}
