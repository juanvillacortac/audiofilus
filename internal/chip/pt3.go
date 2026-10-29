package chip

import (
	"encoding/binary"
	"strings"
	"time"
)

// Los módulos de Pro Tracker 3 (y de Vortex Tracker II, que guarda el mismo
// formato) del ZX Spectrum. No son un volcado: traen los patrones, las
// muestras (de volumen y tono, no de audio) y los ornamentos, y una rutina
// los convierte en los registros del AY 50 veces por segundo. Esta es la de
// Sergey Bulba (Ay_Emul), pasada a C por Volutar (pt3player, MIT, ver
// LICENSE.pt3player) y de ahí a Go; donde la de C se aparta de la de Pascal
// (el deslizamiento de la envolvente, que es con signo), se sigue la de
// Pascal.
//
// Los TurboSound (dos AY: dos módulos seguidos con "02TS" al final, o uno
// que se toca dos veces con los patrones al revés) suenan con dos chips.

// pt3Vol son las tablas de volumen (el del canal por el de la muestra): la
// de las versiones 3.3 y 3.4, y la de la 3.5 en adelante.
var pt3Vol = [2][16][16]byte{{
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3},
	{0, 0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4},
	{0, 0, 0, 1, 1, 1, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5},
	{0, 0, 0, 1, 1, 2, 2, 3, 3, 3, 4, 4, 5, 5, 6, 6},
	{0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7},
	{0, 0, 1, 1, 2, 2, 3, 3, 4, 5, 5, 6, 6, 7, 7, 8},
	{0, 0, 1, 1, 2, 3, 3, 4, 5, 5, 6, 6, 7, 8, 8, 9},
	{0, 0, 1, 2, 2, 3, 4, 4, 5, 6, 6, 7, 8, 8, 9, 10},
	{0, 0, 1, 2, 3, 3, 4, 5, 6, 6, 7, 8, 9, 9, 10, 11},
	{0, 0, 1, 2, 3, 4, 4, 5, 6, 7, 8, 8, 9, 11, 11, 12},
	{0, 0, 1, 2, 3, 4, 5, 6, 7, 7, 8, 9, 10, 11, 12, 13},
	{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
}, {
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 3, 3, 3},
	{0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4},
	{0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5},
	{0, 0, 1, 1, 2, 2, 2, 3, 3, 4, 4, 4, 5, 5, 6, 6},
	{0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7},
	{0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8},
	{0, 1, 1, 2, 2, 3, 4, 4, 5, 5, 6, 7, 7, 8, 8, 9},
	{0, 1, 1, 2, 3, 3, 4, 5, 5, 6, 7, 7, 8, 9, 9, 10},
	{0, 1, 1, 2, 3, 4, 4, 5, 6, 7, 7, 8, 9, 10, 10, 11},
	{0, 1, 2, 2, 3, 4, 5, 6, 6, 7, 8, 9, 10, 10, 11, 12},
	{0, 1, 2, 3, 3, 4, 5, 6, 7, 8, 9, 10, 10, 11, 12, 13},
	{0, 1, 2, 3, 4, 5, 6, 7, 7, 8, 9, 10, 11, 12, 13, 14},
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
}}

// Las tablas de notas (el periodo del tono de cada una de las 96): cada
// módulo elige una (0 Pro Tracker, 1 Sound Tracker, 2 ASM, 3 REAL), y las
// versiones 3.3 y 3.4r usan otras un poco distintas.
var (
	pt3NotesPT3334r = [96]uint16{
		0xC21, 0xB73, 0xACE, 0xA33, 0x9A0, 0x916, 0x893, 0x818, 0x7A4, 0x736, 0x6CE, 0x66D,
		0x610, 0x5B9, 0x567, 0x519, 0x4D0, 0x48B, 0x449, 0x40C, 0x3D2, 0x39B, 0x367, 0x336,
		0x308, 0x2DC, 0x2B3, 0x28C, 0x268, 0x245, 0x224, 0x206, 0x1E9, 0x1CD, 0x1B3, 0x19B,
		0x184, 0x16E, 0x159, 0x146, 0x134, 0x122, 0x112, 0x103, 0x0F4, 0x0E6, 0x0D9, 0x0CD,
		0x0C2, 0x0B7, 0x0AC, 0x0A3, 0x09A, 0x091, 0x089, 0x081, 0x07A, 0x073, 0x06C, 0x066,
		0x061, 0x05B, 0x056, 0x051, 0x04D, 0x048, 0x044, 0x040, 0x03D, 0x039, 0x036, 0x033,
		0x030, 0x02D, 0x02B, 0x028, 0x026, 0x024, 0x022, 0x020, 0x01E, 0x01C, 0x01B, 0x019,
		0x018, 0x016, 0x015, 0x014, 0x013, 0x012, 0x011, 0x010, 0x00F, 0x00E, 0x00D, 0x00C}
	pt3NotesPT3435 = [96]uint16{
		0xC22, 0xB73, 0xACF, 0xA33, 0x9A1, 0x917, 0x894, 0x819, 0x7A4, 0x737, 0x6CF, 0x66D,
		0x611, 0x5BA, 0x567, 0x51A, 0x4D0, 0x48B, 0x44A, 0x40C, 0x3D2, 0x39B, 0x367, 0x337,
		0x308, 0x2DD, 0x2B4, 0x28D, 0x268, 0x246, 0x225, 0x206, 0x1E9, 0x1CE, 0x1B4, 0x19B,
		0x184, 0x16E, 0x15A, 0x146, 0x134, 0x123, 0x112, 0x103, 0x0F5, 0x0E7, 0x0DA, 0x0CE,
		0x0C2, 0x0B7, 0x0AD, 0x0A3, 0x09A, 0x091, 0x089, 0x082, 0x07A, 0x073, 0x06D, 0x067,
		0x061, 0x05C, 0x056, 0x052, 0x04D, 0x049, 0x045, 0x041, 0x03D, 0x03A, 0x036, 0x033,
		0x031, 0x02E, 0x02B, 0x029, 0x027, 0x024, 0x022, 0x020, 0x01F, 0x01D, 0x01B, 0x01A,
		0x018, 0x017, 0x016, 0x014, 0x013, 0x012, 0x011, 0x010, 0x00F, 0x00E, 0x00D, 0x00C}
	pt3NotesST = [96]uint16{
		0xEF8, 0xE10, 0xD60, 0xC80, 0xBD8, 0xB28, 0xA88, 0x9F0, 0x960, 0x8E0, 0x858, 0x7E0,
		0x77C, 0x708, 0x6B0, 0x640, 0x5EC, 0x594, 0x544, 0x4F8, 0x4B0, 0x470, 0x42C, 0x3FD,
		0x3BE, 0x384, 0x358, 0x320, 0x2F6, 0x2CA, 0x2A2, 0x27C, 0x258, 0x238, 0x216, 0x1F8,
		0x1DF, 0x1C2, 0x1AC, 0x190, 0x17B, 0x165, 0x151, 0x13E, 0x12C, 0x11C, 0x10A, 0x0FC,
		0x0EF, 0x0E1, 0x0D6, 0x0C8, 0x0BD, 0x0B2, 0x0A8, 0x09F, 0x096, 0x08E, 0x085, 0x07E,
		0x077, 0x070, 0x06B, 0x064, 0x05E, 0x059, 0x054, 0x04F, 0x04B, 0x047, 0x042, 0x03F,
		0x03B, 0x038, 0x035, 0x032, 0x02F, 0x02C, 0x02A, 0x027, 0x025, 0x023, 0x021, 0x01F,
		0x01D, 0x01C, 0x01A, 0x019, 0x017, 0x016, 0x015, 0x013, 0x012, 0x011, 0x010, 0x00F}
	pt3NotesASM34r = [96]uint16{
		0xD3E, 0xC80, 0xBCC, 0xB22, 0xA82, 0x9EC, 0x95C, 0x8D6, 0x858, 0x7E0, 0x76E, 0x704,
		0x69F, 0x640, 0x5E6, 0x591, 0x541, 0x4F6, 0x4AE, 0x46B, 0x42C, 0x3F0, 0x3B7, 0x382,
		0x34F, 0x320, 0x2F3, 0x2C8, 0x2A1, 0x27B, 0x257, 0x236, 0x216, 0x1F8, 0x1DC, 0x1C1,
		0x1A8, 0x190, 0x179, 0x164, 0x150, 0x13D, 0x12C, 0x11B, 0x10B, 0x0FC, 0x0EE, 0x0E0,
		0x0D4, 0x0C8, 0x0BD, 0x0B2, 0x0A8, 0x09F, 0x096, 0x08D, 0x085, 0x07E, 0x077, 0x070,
		0x06A, 0x064, 0x05E, 0x059, 0x054, 0x050, 0x04B, 0x047, 0x043, 0x03F, 0x03C, 0x038,
		0x035, 0x032, 0x02F, 0x02D, 0x02A, 0x028, 0x026, 0x024, 0x022, 0x020, 0x01E, 0x01D,
		0x01B, 0x01A, 0x019, 0x018, 0x015, 0x014, 0x013, 0x012, 0x011, 0x010, 0x00F, 0x00E}
	pt3NotesASM3435 = [96]uint16{
		0xD10, 0xC55, 0xBA4, 0xAFC, 0xA5F, 0x9CA, 0x93D, 0x8B8, 0x83B, 0x7C5, 0x755, 0x6EC,
		0x688, 0x62A, 0x5D2, 0x57E, 0x52F, 0x4E5, 0x49E, 0x45C, 0x41D, 0x3E2, 0x3AB, 0x376,
		0x344, 0x315, 0x2E9, 0x2BF, 0x298, 0x272, 0x24F, 0x22E, 0x20F, 0x1F1, 0x1D5, 0x1BB,
		0x1A2, 0x18B, 0x174, 0x160, 0x14C, 0x139, 0x128, 0x117, 0x107, 0x0F9, 0x0EB, 0x0DD,
		0x0D1, 0x0C5, 0x0BA, 0x0B0, 0x0A6, 0x09D, 0x094, 0x08C, 0x084, 0x07C, 0x075, 0x06F,
		0x069, 0x063, 0x05D, 0x058, 0x053, 0x04E, 0x04A, 0x046, 0x042, 0x03E, 0x03B, 0x037,
		0x034, 0x031, 0x02F, 0x02C, 0x029, 0x027, 0x025, 0x023, 0x021, 0x01F, 0x01D, 0x01C,
		0x01A, 0x019, 0x017, 0x016, 0x015, 0x014, 0x012, 0x011, 0x010, 0x00F, 0x00E, 0x00D}
	pt3NotesREAL34r = [96]uint16{
		0xCDA, 0xC22, 0xB73, 0xACF, 0xA33, 0x9A1, 0x917, 0x894, 0x819, 0x7A4, 0x737, 0x6CF,
		0x66D, 0x611, 0x5BA, 0x567, 0x51A, 0x4D0, 0x48B, 0x44A, 0x40C, 0x3D2, 0x39B, 0x367,
		0x337, 0x308, 0x2DD, 0x2B4, 0x28D, 0x268, 0x246, 0x225, 0x206, 0x1E9, 0x1CE, 0x1B4,
		0x19B, 0x184, 0x16E, 0x15A, 0x146, 0x134, 0x123, 0x113, 0x103, 0x0F5, 0x0E7, 0x0DA,
		0x0CE, 0x0C2, 0x0B7, 0x0AD, 0x0A3, 0x09A, 0x091, 0x089, 0x082, 0x07A, 0x073, 0x06D,
		0x067, 0x061, 0x05C, 0x056, 0x052, 0x04D, 0x049, 0x045, 0x041, 0x03D, 0x03A, 0x036,
		0x033, 0x031, 0x02E, 0x02B, 0x029, 0x027, 0x024, 0x022, 0x020, 0x01F, 0x01D, 0x01B,
		0x01A, 0x018, 0x017, 0x016, 0x014, 0x013, 0x012, 0x011, 0x010, 0x00F, 0x00E, 0x00D}
	pt3NotesREAL3435 = [96]uint16{
		0xCDA, 0xC22, 0xB73, 0xACF, 0xA33, 0x9A1, 0x917, 0x894, 0x819, 0x7A4, 0x737, 0x6CF,
		0x66D, 0x611, 0x5BA, 0x567, 0x51A, 0x4D0, 0x48B, 0x44A, 0x40C, 0x3D2, 0x39B, 0x367,
		0x337, 0x308, 0x2DD, 0x2B4, 0x28D, 0x268, 0x246, 0x225, 0x206, 0x1E9, 0x1CE, 0x1B4,
		0x19B, 0x184, 0x16E, 0x15A, 0x146, 0x134, 0x123, 0x112, 0x103, 0x0F5, 0x0E7, 0x0DA,
		0x0CE, 0x0C2, 0x0B7, 0x0AD, 0x0A3, 0x09A, 0x091, 0x089, 0x082, 0x07A, 0x073, 0x06D,
		0x067, 0x061, 0x05C, 0x056, 0x052, 0x04D, 0x049, 0x045, 0x041, 0x03D, 0x03A, 0x036,
		0x033, 0x031, 0x02E, 0x02B, 0x029, 0x027, 0x024, 0x022, 0x020, 0x01F, 0x01D, 0x01B,
		0x01A, 0x018, 0x017, 0x016, 0x014, 0x013, 0x012, 0x011, 0x010, 0x00F, 0x00E, 0x00D}
)

// La cabecera del módulo.
const (
	pt3Table     = 0x63 // la tabla de notas
	pt3Delay     = 0x64 // la velocidad (ticks por fila)
	pt3Loop      = 0x66 // la posición a la que vuelve
	pt3Patterns  = 0x67 // dónde están los patrones
	pt3Samples   = 0x69 // 32 punteros a muestras
	pt3Ornaments = 0xA9 // 16 punteros a ornamentos
	pt3Positions = 0xC9 // la lista de posiciones (patrón*3), con 0xff al final
)

// pt3Clock y pt3Hz son el reloj del AY del Spectrum 128 y su interrupción.
const (
	pt3Clock = 1773400
	pt3Hz    = 50
)

// pt3Chan es el estado de un canal del módulo.
type pt3Chan struct {
	addr                                    uint16 // dónde va en el patrón
	ornPtr, smpPtr                          uint16
	ton                                     uint16
	ornLoop, ornLen, ornPos                 byte
	smpLoop, smpLen, smpPos                 byte
	volume, skip, note, slideTo, amplitude  byte
	envOn, enabled, simpleGliss             bool
	ampSliding, noiseSliding, envSliding    int16
	slideCount, onOff, onOffDelay, offOnDel int16
	slideDelay, tonSliding, tonAcc          int16
	slideStep, tonDelta                     int16
	skipCount                               byte
}

// pt3Mod es un módulo en marcha: sus datos (en 64 KB, como en el Spectrum)
// y el estado de la rutina.
type pt3Mod struct {
	m       [65536]byte
	version int
	ts      byte // 0x20, o la mitad de un TurboSound de un solo módulo
	nPos    byte

	envBase                  uint16
	curEnvSlide, envSlideAdd int16
	curEnvDelay, envDelay    int8
	noiseBase, delay         byte
	addToNoise, delayCount   byte
	pos                      byte
	ch                       [3]pt3Chan
	regs                     [14]byte
	mixer                    byte
	addToEnv                 int16
	looped                   bool // ya dio la vuelta
}

func (p *pt3Mod) u16(a int) uint16 {
	return uint16(p.m[a&0xffff]) | uint16(p.m[(a+1)&0xffff])<<8
}

// newPT3 prepara un módulo; ts es 0x20, o el valor que lo hace la segunda
// mitad de un TurboSound de un solo módulo.
func newPT3(data []byte, ts byte) (*pt3Mod, bool) {
	if len(data) < pt3Positions+1 {
		return nil, false
	}
	p := &pt3Mod{version: 6, ts: ts}
	copy(p.m[:], data)
	if c := p.m[13]; c >= '0' && c <= '9' {
		p.version = int(c - '0')
	}
	n := 0
	for pt3Positions+n < len(data) && p.m[pt3Positions+n] != 0xff {
		n++
	}
	if n == 0 {
		return nil, false
	}
	p.nPos = byte(min(n, 255))
	p.restart()
	return p, true
}

// restart vuelve al principio.
func (p *pt3Mod) restart() {
	p.delayCount, p.delay = 1, p.m[pt3Delay]
	p.noiseBase, p.addToNoise = 0, 0
	p.curEnvSlide, p.curEnvDelay, p.envBase = 0, 0, 0
	p.pos, p.looped = 0, false
	p.setPattern()
	for i := range p.ch {
		c := &p.ch[i]
		addr := c.addr
		*c = pt3Chan{addr: addr, volume: 15, skipCount: 1}
		p.setOrnament(c, 0)
		p.setSample(c, 1)
	}
}

// setPattern apunta los tres canales al patrón de la posición actual.
func (p *pt3Mod) setPattern() {
	i := int(p.m[pt3Positions+int(p.pos)])
	if p.ts != 0x20 {
		i = int(p.ts)*3 - 3 - i
	}
	for abc := range p.ch {
		p.ch[abc].addr = p.u16(int(p.u16(pt3Patterns)) + (i+abc)*2)
	}
}

func (p *pt3Mod) setOrnament(c *pt3Chan, n int) {
	c.ornPtr = p.u16(pt3Ornaments + n*2)
	c.ornLoop = p.m[c.ornPtr]
	c.ornLen = p.m[c.ornPtr+1]
	c.ornPtr += 2
	c.ornPos = 0
}

func (p *pt3Mod) setSample(c *pt3Chan, n int) {
	c.smpPtr = p.u16(pt3Samples + (n&31)*2)
	c.smpLoop = p.m[c.smpPtr]
	c.smpLen = p.m[c.smpPtr+1]
	c.smpPtr += 2
}

// noteFreq es el periodo de la nota j según la tabla y la versión.
func (p *pt3Mod) noteFreq(j byte) uint16 {
	j = min(j, 95)
	old := p.version <= 3
	switch p.m[pt3Table] {
	case 0:
		if old {
			return pt3NotesPT3334r[j]
		}
		return pt3NotesPT3435[j]
	case 1:
		return pt3NotesST[j]
	case 2:
		if old {
			return pt3NotesASM34r[j]
		}
		return pt3NotesASM3435[j]
	}
	if old {
		return pt3NotesREAL34r[j]
	}
	return pt3NotesREAL3435[j]
}

// next lee el siguiente byte del patrón del canal.
func (p *pt3Mod) next(c *pt3Chan) byte {
	c.addr++
	return p.m[c.addr]
}

// resetNote borra los deslizamientos al empezar (o apagar) una nota.
func resetNote(c *pt3Chan) {
	c.smpPos, c.ampSliding, c.noiseSliding, c.envSliding = 0, 0, 0, 0
	c.ornPos, c.slideCount, c.tonSliding, c.tonAcc, c.onOff = 0, 0, 0, 0, 0
}

// interpret lee una fila del patrón de un canal.
func (p *pt3Mod) interpret(c *pt3Chan) {
	var f1, f2, f3, f4, f5, f8, f9, counter byte
	prNote, prSliding := c.note, c.tonSliding
	for quit := false; !quit; c.addr++ {
		op := p.m[c.addr]
		switch {
		case op >= 0xf0: // ornamento y muestra
			p.setOrnament(c, int(op-0xf0))
			p.setSample(c, int(p.next(c)/2))
			c.envOn = false
		case op >= 0xd1: // muestra
			p.setSample(c, int(op-0xd0))
		case op == 0xd0: // fila vacía
			quit = true
		case op >= 0xc1: // volumen
			c.volume = op - 0xc0
		case op == 0xc0: // silencio
			resetNote(c)
			c.enabled = false
			quit = true
		case op >= 0xb2: // envolvente
			c.envOn = true
			p.regs[13] = op - 0xb1
			p.envBase = uint16(p.next(c))<<8 | uint16(p.next(c))
			c.ornPos = 0
			p.curEnvSlide, p.curEnvDelay = 0, 0
		case op == 0xb1: // filas que se salta
			c.skip = p.next(c)
		case op == 0xb0: // sin envolvente
			c.envOn = false
			c.ornPos = 0
		case op >= 0x50: // nota
			c.note = op - 0x50
			resetNote(c)
			c.enabled = true
			quit = true
		case op >= 0x40: // ornamento
			p.setOrnament(c, int(op-0x40))
		case op >= 0x20: // ruido
			p.noiseBase = op - 0x20
		case op >= 0x11: // envolvente y muestra
			p.regs[13] = op - 0x10
			p.envBase = uint16(p.next(c))<<8 | uint16(p.next(c))
			p.curEnvSlide, p.curEnvDelay = 0, 0
			c.envOn = true
			p.setSample(c, int(p.next(c)/2))
			c.ornPos = 0
		case op == 0x10: // sin envolvente, y muestra
			c.envOn = false
			p.setSample(c, int(p.next(c)/2))
			c.ornPos = 0
		case op == 9:
			counter++
			f9 = counter
		case op == 8:
			counter++
			f8 = counter
		case op == 5:
			counter++
			f5 = counter
		case op == 4:
			counter++
			f4 = counter
		case op == 3:
			counter++
			f3 = counter
		case op == 2:
			counter++
			f2 = counter
		case op == 1:
			counter++
			f1 = counter
		}
	}
	// Los parámetros de las órdenes vienen detrás de la nota, en orden
	// inverso al de las órdenes.
	for ; counter > 0; counter-- {
		switch counter {
		case f1: // glissando: retardo, paso
			c.slideDelay = int16(p.m[c.addr])
			c.addr++
			c.slideCount = c.slideDelay
			c.slideStep = int16(p.u16(int(c.addr)))
			c.simpleGliss = true
			c.onOff = 0
			if c.slideCount == 0 && p.version >= 7 {
				c.slideCount++
			}
			c.addr += 2
		case f2: // portamento: retardo, (máximo), paso
			c.simpleGliss = false
			c.onOff = 0
			c.slideDelay = int16(p.m[c.addr])
			c.addr++
			c.slideCount = c.slideDelay
			step := int16(p.u16(int(c.addr) + 2))
			if step < 0 {
				step = -step
			}
			c.slideStep = step
			c.tonDelta = int16(p.noteFreq(c.note)) - int16(p.noteFreq(prNote))
			c.slideTo = c.note
			c.note = prNote
			if p.version >= 6 {
				c.tonSliding = prSliding
			}
			if c.tonDelta-c.tonSliding < 0 {
				c.slideStep = -c.slideStep
			}
			c.addr += 4
		case f3: // posición en la muestra
			c.smpPos = p.m[c.addr]
			c.addr++
		case f4: // posición en el ornamento
			c.ornPos = p.m[c.addr]
			c.addr++
		case f5: // trémolo: encendido, apagado
			c.onOffDelay = int16(p.m[c.addr])
			c.offOnDel = int16(p.m[c.addr+1])
			c.addr += 2
			c.onOff = c.onOffDelay
			c.slideCount, c.tonSliding = 0, 0
		case f8: // deslizamiento de la envolvente: retardo, paso
			p.envDelay = int8(p.m[c.addr])
			c.addr++
			p.curEnvDelay = p.envDelay
			p.envSlideAdd = int16(p.u16(int(c.addr)))
			c.addr += 2
		case f9: // velocidad
			p.delay = p.m[c.addr]
			c.addr++
		}
	}
	c.skipCount = c.skip
}

// changeRegs calcula el tono, el volumen y el mezclador de un canal.
func (p *pt3Mod) changeRegs(c *pt3Chan) {
	if c.enabled {
		s := int(c.smpPtr) + int(c.smpPos)*4
		c.ton = p.u16(s+2) + uint16(c.tonAcc)
		b0, b1 := p.m[s&0xffff], p.m[(s+1)&0xffff]
		if b1&0x40 != 0 {
			c.tonAcc = int16(c.ton)
		}
		j := c.note + p.m[(int(c.ornPtr)+int(c.ornPos))&0xffff]
		if j >= 128 {
			j = 0
		}
		c.ton = (c.ton + uint16(c.tonSliding) + p.noteFreq(j)) & 0xfff
		if c.slideCount > 0 {
			c.slideCount--
			if c.slideCount == 0 {
				c.tonSliding += c.slideStep
				c.slideCount = c.slideDelay
				if !c.simpleGliss && (c.slideStep < 0 && c.tonSliding <= c.tonDelta ||
					c.slideStep >= 0 && c.tonSliding >= c.tonDelta) {
					c.note = c.slideTo
					c.slideCount, c.tonSliding = 0, 0
				}
			}
		}
		amp := int(b1 & 15)
		if b0&0x80 != 0 {
			if b0&0x40 != 0 {
				if c.ampSliding < 15 {
					c.ampSliding++
				}
			} else if c.ampSliding > -15 {
				c.ampSliding--
			}
		}
		amp = min(max(amp+int(c.ampSliding), 0), 15)
		vt := 1
		if p.version <= 4 {
			vt = 0
		}
		c.amplitude = pt3Vol[vt][c.volume&15][amp]
		if b0&1 == 0 && c.envOn {
			c.amplitude |= 0x10
		}
		if b1&0x80 != 0 {
			var j int8
			if b0&0x20 != 0 {
				j = int8(b0>>1|0xf0) + int8(c.envSliding)
			} else {
				j = int8(b0>>1&0x0f) + int8(c.envSliding)
			}
			if b1&0x20 != 0 {
				c.envSliding = int16(j)
			}
			p.addToEnv += int16(j)
		} else {
			p.addToNoise = b0>>1 + byte(c.noiseSliding)
			if b1&0x20 != 0 {
				c.noiseSliding = int16(p.addToNoise)
			}
		}
		p.mixer |= b1 >> 1 & 0x48
		c.smpPos++
		if c.smpPos >= c.smpLen {
			c.smpPos = c.smpLoop
		}
		c.ornPos++
		if c.ornPos >= c.ornLen {
			c.ornPos = c.ornLoop
		}
	} else {
		c.amplitude = 0
	}
	p.mixer >>= 1
	if c.onOff > 0 {
		c.onOff--
		if c.onOff == 0 {
			c.enabled = !c.enabled
			if c.enabled {
				c.onOff = c.onOffDelay
			} else {
				c.onOff = c.offOnDel
			}
		}
	}
}

// tick avanza una interrupción (1/50 s) y deja los registros en regs (el 13
// es 0xff si no hay que tocar la envolvente).
func (p *pt3Mod) tick() {
	p.regs[13] = 0xff
	p.delayCount--
	if p.delayCount == 0 {
		a := &p.ch[0]
		a.skipCount--
		if a.skipCount == 0 {
			if p.m[a.addr] == 0 {
				p.pos++
				if p.pos == p.nPos {
					p.pos = p.m[pt3Loop]
					if p.pos >= p.nPos {
						p.pos = 0
					}
					p.looped = true
				}
				p.setPattern()
				p.noiseBase = 0
			}
			p.interpret(a)
		}
		for i := 1; i < 3; i++ {
			c := &p.ch[i]
			c.skipCount--
			if c.skipCount == 0 {
				p.interpret(c)
			}
		}
		p.delayCount = p.delay
	}
	p.addToEnv, p.mixer = 0, 0
	for i := range p.ch {
		p.changeRegs(&p.ch[i])
	}
	for i, c := range p.ch {
		p.regs[i*2] = byte(c.ton)
		p.regs[i*2+1] = byte(c.ton >> 8)
		p.regs[8+i] = c.amplitude
	}
	p.regs[6] = (p.noiseBase + p.addToNoise) & 0x1f
	p.regs[7] = p.mixer
	env := p.envBase + uint16(p.addToEnv) + uint16(p.curEnvSlide)
	p.regs[11], p.regs[12] = byte(env), byte(env>>8)
	if p.curEnvDelay > 0 {
		p.curEnvDelay--
		if p.curEnvDelay == 0 {
			p.curEnvDelay = p.envDelay
			p.curEnvSlide += p.envSlideAdd
		}
	}
}

// pt3Song es un archivo PT3: uno o dos módulos (TurboSound).
type pt3Song struct {
	data  [][]byte // los módulos
	ts    byte     // el TurboSound de un solo módulo (0x20 si no lo es)
	track Track
}

// pt3MaxTicks es lo más que se mira para saber cuánto dura (media hora).
const pt3MaxTicks = 30 * 60 * pt3Hz

// loadPT3 lee un PT3.
func loadPT3(b []byte, base string) (song, error) {
	if !strings.HasPrefix(string(b), "ProTracker 3") && !strings.HasPrefix(string(b), "Vortex Tracker II") {
		return nil, ErrFormat
	}
	s := &pt3Song{data: [][]byte{b}, ts: 0x20}
	if n := len(b); n > 16 && string(b[n-4:]) == "02TS" {
		s1 := int(binary.LittleEndian.Uint16(b[n-12:]))
		s2 := int(binary.LittleEndian.Uint16(b[n-6:]))
		if s1+s2 <= n-16 {
			s.data = [][]byte{b[:s1], b[s1 : s1+s2]}
		}
	} else if len(b) > 98 && b[98] != 0x20 {
		s.ts = b[98]
	}
	p, ok := newPT3(s.data[0], 0x20)
	if !ok {
		return nil, ErrFormat
	}
	if len(s.data) == 2 {
		if _, ok := newPT3(s.data[1], 0x20); !ok {
			return nil, ErrFormat
		}
	}
	ticks := 0
	for !p.looped && ticks < pt3MaxTicks {
		p.tick()
		ticks++
	}
	title := fixed(b[0x1e:0x3e])
	author := ""
	if strings.HasPrefix(string(b[0x3e:0x42]), " by ") {
		author = fixed(b[0x42:0x62])
	}
	s.track = Track{Song: 1, Title: orStr(title, base), Author: author, System: "ZX Spectrum",
		Length: time.Duration(ticks) * time.Second / pt3Hz}
	return s, nil
}

func (s *pt3Song) tracks() []Track { return []Track{s.track} }

func (s *pt3Song) play(_ int, rate int) (Tune, error) {
	t := &pt3Tune{per: float64(rate) / pt3Hz, ticks: int(s.track.Length * pt3Hz / time.Second)}
	add := func(data []byte, ts byte) {
		m, _ := newPT3(data, ts)
		t.mods = append(t.mods, m)
		t.chips = append(t.chips, newAY(pt3Clock, rate, false, panABC))
	}
	add(s.data[0], 0x20)
	switch {
	case len(s.data) == 2:
		add(s.data[1], 0x20)
	case s.ts != 0x20:
		add(s.data[0], s.ts)
	}
	return t, nil
}

// pt3Tune toca un PT3: cada 1/50 s la rutina da los registros de cada chip.
type pt3Tune struct {
	mods  []*pt3Mod
	chips []*ay
	per   float64 // muestras por tick
	left  float64
	ticks int // los que dura (una vuelta)
	dc    [2]dcBlock
}

// next avanza un tick de cada módulo y escribe sus registros; false si ya
// se acabó.
func (t *pt3Tune) next() bool {
	if t.ticks <= 0 {
		return false
	}
	t.ticks--
	for i, m := range t.mods {
		m.tick()
		for r := 0; r < 13; r++ {
			t.chips[i].write(r, m.regs[r])
		}
		if m.regs[13] != 0xff {
			t.chips[i].write(13, m.regs[13])
		}
	}
	t.left += t.per
	return true
}

// Skip avanza frames cuadros sin sonar: solo corre la rutina.
func (t *pt3Tune) Skip(frames int) {
	for f := float64(frames); f > 0; {
		if t.left <= 0 && !t.next() {
			return
		}
		k := min(t.left, f)
		t.left -= k
		f -= k
	}
}

func (t *pt3Tune) Render(dst []float32) int {
	n := 0
	gain := 1.2 / float64(len(t.chips))
	for ; n+1 < len(dst); n += 2 {
		if t.left <= 0 && !t.next() {
			return n
		}
		t.left--
		var l, r float64
		for _, c := range t.chips {
			cl, cr := c.sample()
			l += cl
			r += cr
		}
		dst[n] = clamp1(t.dc[0].next(l) * gain)
		dst[n+1] = clamp1(t.dc[1].next(r) * gain)
	}
	return n
}
