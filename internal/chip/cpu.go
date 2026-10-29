package chip

// cpu es un 6502 (el 6510 del C64) que cuenta ciclos, con los opcodes
// ilegales estables, que algunas rutinas de música usan (LAX, SAX, DCP,
// ISC, SLO, RLA, SRE, RRA, ANC, ALR, ARR, SBX y los NOP largos). Los JAM
// paran la CPU. Los ciclos son los de la tabla, más uno al cruzar de página
// al leer con índice y los de los saltos tomados; basta para la música.
type cpu struct {
	a, x, y, s, p byte
	pc            uint16
	jammed        bool
	mem           bus
}

// bus es la memoria que ve la CPU.
type bus interface {
	read(addr uint16) byte
	write(addr uint16, v byte)
}

// Las banderas del registro de estado.
const (
	flagC = 0x01
	flagZ = 0x02
	flagI = 0x04
	flagD = 0x08
	flagB = 0x10
	flagU = 0x20
	flagV = 0x40
	flagN = 0x80
)

// cpuCycles son los ciclos de cada opcode, sin los extra.
var cpuCycles = [256]byte{
	7, 6, 2, 8, 3, 3, 5, 5, 3, 2, 2, 2, 4, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
	6, 6, 2, 8, 3, 3, 5, 5, 4, 2, 2, 2, 4, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
	6, 6, 2, 8, 3, 3, 5, 5, 3, 2, 2, 2, 3, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
	6, 6, 2, 8, 3, 3, 5, 5, 4, 2, 2, 2, 5, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
	2, 6, 2, 6, 3, 3, 3, 3, 2, 2, 2, 2, 4, 4, 4, 4,
	2, 6, 2, 6, 4, 4, 4, 4, 2, 5, 2, 5, 5, 5, 5, 5,
	2, 6, 2, 6, 3, 3, 3, 3, 2, 2, 2, 2, 4, 4, 4, 4,
	2, 5, 2, 5, 4, 4, 4, 4, 2, 4, 2, 4, 4, 4, 4, 4,
	2, 6, 2, 8, 3, 3, 5, 5, 2, 2, 2, 2, 4, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
	2, 6, 2, 8, 3, 3, 5, 5, 2, 2, 2, 2, 4, 4, 6, 6,
	2, 5, 2, 8, 4, 4, 6, 6, 2, 4, 2, 7, 4, 4, 7, 7,
}

func (c *cpu) fetch() byte {
	v := c.mem.read(c.pc)
	c.pc++
	return v
}

func (c *cpu) fetch16() uint16 {
	lo := uint16(c.fetch())
	return lo | uint16(c.fetch())<<8
}

func (c *cpu) read16(a uint16) uint16 {
	return uint16(c.mem.read(a)) | uint16(c.mem.read(a+1))<<8
}

// read16zp lee un puntero de la página cero (que da la vuelta en ella).
func (c *cpu) read16zp(a byte) uint16 {
	return uint16(c.mem.read(uint16(a))) | uint16(c.mem.read(uint16(a+1)))<<8
}

func (c *cpu) push(v byte) {
	c.mem.write(0x100|uint16(c.s), v)
	c.s--
}

func (c *cpu) pull() byte {
	c.s++
	return c.mem.read(0x100 | uint16(c.s))
}

func (c *cpu) push16(v uint16) {
	c.push(byte(v >> 8))
	c.push(byte(v))
}

func (c *cpu) pull16() uint16 {
	lo := uint16(c.pull())
	return lo | uint16(c.pull())<<8
}

func (c *cpu) setZN(v byte) {
	c.p &^= flagZ | flagN
	if v == 0 {
		c.p |= flagZ
	}
	c.p |= v & flagN
}

func (c *cpu) flag(f byte, on bool) {
	if on {
		c.p |= f
	} else {
		c.p &^= f
	}
}

// interrupt salta al vector (IRQ, NMI) guardando el contador y el estado.
func (c *cpu) interrupt(vector uint16) {
	c.push16(c.pc)
	c.push(c.p&^flagB | flagU)
	c.p |= flagI
	c.pc = c.read16(vector)
}

// Los modos de direccionamiento: dan la dirección y si cruzó de página.
func (c *cpu) zp() uint16  { return uint16(c.fetch()) }
func (c *cpu) zpx() uint16 { return uint16(c.fetch() + c.x) }
func (c *cpu) zpy() uint16 { return uint16(c.fetch() + c.y) }
func (c *cpu) abs() uint16 { return c.fetch16() }
func (c *cpu) indx() uint16 {
	return c.read16zp(c.fetch() + c.x)
}

func (c *cpu) absIdx(i byte) (uint16, bool) {
	b := c.fetch16()
	a := b + uint16(i)
	return a, a&0xff00 != b&0xff00
}

func (c *cpu) indy() (uint16, bool) {
	b := c.read16zp(c.fetch())
	a := b + uint16(c.y)
	return a, a&0xff00 != b&0xff00
}

// addr01 es la dirección de los modos del grupo de ORA/LDA/STA (bbb) y, con
// yIdx, la de LAX/SAX (que indexan con Y en vez de X). extra suma un ciclo
// si cruzó de página y la instrucción solo lee.
func (c *cpu) addr01(bbb byte, yIdx bool, extra *int, reads bool) uint16 {
	var a uint16
	var cross bool
	switch bbb {
	case 0:
		a = c.indx()
	case 1:
		a = c.zp()
	case 2:
		a = c.pc
		c.pc++
	case 3:
		a = c.abs()
	case 4:
		a, cross = c.indy()
	case 5:
		if yIdx {
			a = c.zpy()
		} else {
			a = c.zpx()
		}
	case 6:
		a, cross = c.absIdx(c.y)
	case 7:
		if yIdx {
			a, cross = c.absIdx(c.y)
		} else {
			a, cross = c.absIdx(c.x)
		}
	}
	if cross && reads {
		*extra++
	}
	return a
}

func (c *cpu) adc(v byte) {
	if c.p&flagD != 0 {
		carry := int(c.p & flagC)
		lo := int(c.a&15) + int(v&15) + carry
		hi := int(c.a>>4) + int(v>>4)
		if lo > 9 {
			lo += 6
		}
		if lo > 15 {
			hi++
		}
		bin := int(c.a) + int(v) + carry
		c.flag(flagZ, byte(bin) == 0)
		c.flag(flagN, hi&8 != 0)
		c.flag(flagV, (^(c.a^v)&(c.a^byte(hi<<4)))&0x80 != 0)
		if hi > 9 {
			hi += 6
		}
		c.flag(flagC, hi > 15)
		c.a = byte(hi<<4 | lo&15)
		return
	}
	sum := int(c.a) + int(v) + int(c.p&flagC)
	r := byte(sum)
	c.flag(flagV, (^(c.a^v)&(c.a^r))&0x80 != 0)
	c.flag(flagC, sum > 0xff)
	c.a = r
	c.setZN(r)
}

func (c *cpu) sbc(v byte) {
	if c.p&flagD != 0 {
		borrow := 1 - int(c.p&flagC)
		bin := int(c.a) - int(v) - borrow
		lo := int(c.a&15) - int(v&15) - borrow
		hi := int(c.a>>4) - int(v>>4)
		if lo < 0 {
			lo -= 6
			hi--
		}
		if hi < 0 {
			hi -= 6
		}
		c.flag(flagV, ((c.a^v)&(c.a^byte(bin)))&0x80 != 0)
		c.flag(flagC, bin >= 0)
		c.setZN(byte(bin))
		c.a = byte(hi<<4 | lo&15)
		return
	}
	c.adc(^v)
}

func (c *cpu) cmp(r, v byte) {
	c.flag(flagC, r >= v)
	c.setZN(r - v)
}

func (c *cpu) asl(v byte) byte {
	c.flag(flagC, v&0x80 != 0)
	v <<= 1
	c.setZN(v)
	return v
}

func (c *cpu) lsr(v byte) byte {
	c.flag(flagC, v&1 != 0)
	v >>= 1
	c.setZN(v)
	return v
}

func (c *cpu) rol(v byte) byte {
	carry := c.p & flagC
	c.flag(flagC, v&0x80 != 0)
	v = v<<1 | carry
	c.setZN(v)
	return v
}

func (c *cpu) ror(v byte) byte {
	carry := c.p & flagC
	c.flag(flagC, v&1 != 0)
	v = v>>1 | carry<<7
	c.setZN(v)
	return v
}

// alu hace la operación aaa del grupo de ORA...SBC con v.
func (c *cpu) alu(aaa, v byte) {
	switch aaa {
	case 0:
		c.a |= v
		c.setZN(c.a)
	case 1:
		c.a &= v
		c.setZN(c.a)
	case 2:
		c.a ^= v
		c.setZN(c.a)
	case 3:
		c.adc(v)
	case 5:
		c.a = v
		c.setZN(v)
	case 6:
		c.cmp(c.a, v)
	case 7:
		c.sbc(v)
	}
}

// shift hace la operación aaa del grupo de ASL...INC (0-3 y 6-7) con v.
func (c *cpu) shift(aaa, v byte) byte {
	switch aaa {
	case 0:
		return c.asl(v)
	case 1:
		return c.rol(v)
	case 2:
		return c.lsr(v)
	case 3:
		return c.ror(v)
	case 6:
		v--
	case 7:
		v++
	}
	c.setZN(v)
	return v
}

func (c *cpu) branch(cond bool, extra *int) {
	off := int8(c.fetch())
	if !cond {
		return
	}
	*extra++
	to := uint16(int(c.pc) + int(off))
	if to&0xff00 != c.pc&0xff00 {
		*extra++
	}
	c.pc = to
}

// step ejecuta una instrucción y dice cuántos ciclos tardó.
func (c *cpu) step() int {
	if c.jammed {
		return 2
	}
	op := c.fetch()
	extra := 0
	aaa, bbb := op>>5, op>>2&7
	switch op & 3 {
	case 1: // ORA AND EOR ADC STA LDA CMP SBC
		if aaa == 4 {
			if bbb != 2 { // 0x89 es un NOP con operando
				c.mem.write(c.addr01(bbb, false, &extra, false), c.a)
			} else {
				c.pc++
			}
			break
		}
		c.alu(aaa, c.mem.read(c.addr01(bbb, false, &extra, true)))
	case 2:
		c.op2(op, aaa, bbb, &extra)
	case 0:
		c.op0(op, aaa, bbb, &extra)
	case 3:
		c.op3(op, aaa, bbb, &extra)
	}
	return int(cpuCycles[op]) + extra
}

// op2 son ASL ROL LSR ROR STX LDX DEC INC y las transferencias de X.
func (c *cpu) op2(op, aaa, bbb byte, extra *int) {
	switch bbb {
	case 0: // inmediato: LDX; los demás, NOP; 02, 22, 42, 62: JAM
		v := c.fetch()
		if aaa == 5 {
			c.x = v
			c.setZN(v)
		} else if aaa < 4 {
			c.jammed = true
		}
		return
	case 4: // x2 con bbb=4: JAM
		c.jammed = true
		return
	case 2: // acumulador y transferencias
		switch aaa {
		case 4:
			c.a = c.x
			c.setZN(c.a)
		case 5:
			c.x = c.a
			c.setZN(c.x)
		case 6:
			c.x--
			c.setZN(c.x)
		case 7: // NOP
		default:
			c.a = c.shift(aaa, c.a)
		}
		return
	case 6:
		switch aaa {
		case 4:
			c.s = c.x
		case 5:
			c.x = c.s
			c.setZN(c.x)
		}
		return // los demás, NOP
	}
	var a uint16
	cross := false
	switch bbb {
	case 1:
		a = c.zp()
	case 3:
		a = c.abs()
	case 5:
		if aaa == 4 || aaa == 5 {
			a = c.zpy()
		} else {
			a = c.zpx()
		}
	case 7:
		if aaa == 4 || aaa == 5 {
			a, cross = c.absIdx(c.y)
		} else {
			a, cross = c.absIdx(c.x)
		}
	}
	switch aaa {
	case 4:
		if bbb == 7 { // SHX
			c.mem.write(a, c.x&(byte(a>>8)+1))
			return
		}
		c.mem.write(a, c.x)
	case 5:
		if cross {
			*extra++
		}
		c.x = c.mem.read(a)
		c.setZN(c.x)
	default:
		c.mem.write(a, c.shift(aaa, c.mem.read(a)))
	}
}

// op0 son los saltos, las pilas, las banderas, BIT, LDY/STY/CPY/CPX y NOPs.
func (c *cpu) op0(op, aaa, bbb byte, extra *int) {
	switch op {
	case 0x00: // BRK
		c.pc++
		c.push16(c.pc)
		c.push(c.p | flagB | flagU)
		c.p |= flagI
		c.pc = c.read16(0xfffe)
		return
	case 0x20:
		a := c.fetch16()
		c.push16(c.pc - 1)
		c.pc = a
		return
	case 0x40:
		c.p = c.pull()&^flagB | flagU
		c.pc = c.pull16()
		return
	case 0x60:
		c.pc = c.pull16() + 1
		return
	case 0x4c:
		c.pc = c.fetch16()
		return
	case 0x6c: // con el error del 6502 al final de página
		p := c.fetch16()
		c.pc = uint16(c.mem.read(p)) | uint16(c.mem.read(p&0xff00|uint16(byte(p)+1)))<<8
		return
	}
	switch bbb {
	case 4: // saltos condicionales
		var cond bool
		switch aaa >> 1 {
		case 0:
			cond = c.p&flagN != 0
		case 1:
			cond = c.p&flagV != 0
		case 2:
			cond = c.p&flagC != 0
		case 3:
			cond = c.p&flagZ != 0
		}
		c.branch(cond == (aaa&1 != 0), extra)
		return
	case 2:
		switch aaa {
		case 0:
			c.push(c.p | flagB | flagU)
		case 1:
			c.p = c.pull()&^flagB | flagU
		case 2:
			c.push(c.a)
		case 3:
			c.a = c.pull()
			c.setZN(c.a)
		case 4:
			c.y--
			c.setZN(c.y)
		case 5:
			c.y = c.a
			c.setZN(c.y)
		case 6:
			c.y++
			c.setZN(c.y)
		case 7:
			c.x++
			c.setZN(c.x)
		}
		return
	case 6:
		switch aaa {
		case 0:
			c.p &^= flagC
		case 1:
			c.p |= flagC
		case 2:
			c.p &^= flagI
		case 3:
			c.p |= flagI
		case 4:
			c.a = c.y
			c.setZN(c.a)
		case 5:
			c.p &^= flagV
		case 6:
			c.p &^= flagD
		case 7:
			c.p |= flagD
		}
		return
	}
	var a uint16
	cross := false
	switch bbb {
	case 0:
		a = c.pc
		c.pc++
	case 1:
		a = c.zp()
	case 3:
		a = c.abs()
	case 5:
		a = c.zpx()
	case 7:
		a, cross = c.absIdx(c.x)
	}
	switch aaa {
	case 1:
		if bbb == 1 || bbb == 3 { // BIT
			v := c.mem.read(a)
			c.flag(flagZ, c.a&v == 0)
			c.p = c.p&^(flagN|flagV) | v&(flagN|flagV)
			return
		}
	case 4:
		if bbb == 7 { // SHY
			c.mem.write(a, c.y&(byte(a>>8)+1))
		} else if bbb != 0 {
			c.mem.write(a, c.y)
		}
		return
	case 5:
		if cross {
			*extra++
		}
		c.y = c.mem.read(a)
		c.setZN(c.y)
		return
	case 6, 7: // CPY y CPX (con zp,x y abs,x son NOP)
		if bbb == 5 || bbb == 7 {
			break
		}
		r := c.y
		if aaa == 7 {
			r = c.x
		}
		c.cmp(r, c.mem.read(a))
		return
	}
	// Los demás son NOP (que leen, y con abs,x cuentan el cruce).
	if cross {
		*extra++
	}
}

// op3 son los ilegales: la combinación de los grupos 1 y 2.
func (c *cpu) op3(op, aaa, bbb byte, extra *int) {
	if bbb == 2 { // inmediatos
		v := c.fetch()
		switch aaa {
		case 0, 1: // ANC
			c.a &= v
			c.setZN(c.a)
			c.flag(flagC, c.a&0x80 != 0)
		case 2: // ALR
			c.a = c.lsr(c.a & v)
		case 3: // ARR
			c.a = c.ror(c.a & v)
			c.flag(flagC, c.a&0x40 != 0)
			c.flag(flagV, (c.a>>6^c.a>>5)&1 != 0)
		case 4: // ANE (inestable: con la constante más común)
			c.a = (c.a | 0xee) & c.x & v
			c.setZN(c.a)
		case 5: // LXA
			c.a = (c.a | 0xee) & v
			c.x = c.a
			c.setZN(c.a)
		case 6: // SBX
			r := int(c.a&c.x) - int(v)
			c.flag(flagC, r >= 0)
			c.x = byte(r)
			c.setZN(c.x)
		case 7: // SBC
			c.sbc(v)
		}
		return
	}
	switch op {
	case 0x9b: // TAS
		a, _ := c.absIdx(c.y)
		c.s = c.a & c.x
		c.mem.write(a, c.s&(byte(a>>8)+1))
		return
	case 0xbb: // LAS
		a, cross := c.absIdx(c.y)
		if cross {
			*extra++
		}
		v := c.mem.read(a) & c.s
		c.a, c.x, c.s = v, v, v
		c.setZN(v)
		return
	case 0x93, 0x9f: // SHA
		var a uint16
		if op == 0x93 {
			a, _ = c.indy()
		} else {
			a, _ = c.absIdx(c.y)
		}
		c.mem.write(a, c.a&c.x&(byte(a>>8)+1))
		return
	}
	idxY := aaa == 4 || aaa == 5
	a := c.addr01(bbb, idxY, extra, aaa == 5)
	switch aaa {
	case 4: // SAX
		c.mem.write(a, c.a&c.x)
	case 5: // LAX
		v := c.mem.read(a)
		c.a, c.x = v, v
		c.setZN(v)
	case 6: // DCP
		v := c.mem.read(a) - 1
		c.mem.write(a, v)
		c.cmp(c.a, v)
	case 7: // ISC
		v := c.mem.read(a) + 1
		c.mem.write(a, v)
		c.sbc(v)
	default: // SLO RLA SRE RRA
		v := c.shift(aaa, c.mem.read(a))
		c.mem.write(a, v)
		c.alu([4]byte{0, 1, 2, 3}[aaa], v)
	}
}
