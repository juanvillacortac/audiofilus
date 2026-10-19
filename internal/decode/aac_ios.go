//go:build ios

package decode

// AAC con el decodificador de Apple: un AudioConverter de AudioToolbox, de
// AAC (los bloques sueltos de un MP4) a float32 intercalado. En iOS no hay
// libfdk-aac; Apple trae el suyo, y es el que usa el sistema para lo
// comprado en iTunes.
//
// El AudioConverter recibe la configuración de la pista como "magic
// cookie": la AudioSpecificConfig envuelta en un descriptor ES de MP4 (como
// la caja esds), igual que hace FFmpeg (audiotoolboxdec.c).

/*
#cgo LDFLAGS: -framework AudioToolbox
#include <stdlib.h>
#include <AudioToolbox/AudioToolbox.h>

// atInput es el bloque que se le da al convertidor, una vez.
typedef struct {
	const void *data;
	UInt32 size;
	int used;
	AudioStreamPacketDescription desc;
} atInput;

// atFeed le da el bloque al convertidor; la segunda vez, "no hay más" (1).
static OSStatus atFeed(AudioConverterRef c, UInt32 *n, AudioBufferList *io,
                       AudioStreamPacketDescription **desc, void *user) {
	atInput *in = (atInput *)user;
	if (in->used) {
		*n = 0;
		return 1;
	}
	in->used = 1;
	io->mNumberBuffers = 1;
	io->mBuffers[0].mData = (void *)in->data;
	io->mBuffers[0].mDataByteSize = in->size;
	io->mBuffers[0].mNumberChannels = 0;
	*n = 1;
	if (desc) {
		in->desc.mStartOffset = 0;
		in->desc.mVariableFramesInPacket = 0;
		in->desc.mDataByteSize = in->size;
		*desc = &in->desc;
	}
	return noErr;
}

// atNew crea el convertidor para la cookie; dice la frecuencia y los canales
// de salida.
static OSStatus atNew(const void *cookie, UInt32 cookieSize, AudioConverterRef *conv,
                      Float64 *rate, UInt32 *channels) {
	AudioStreamBasicDescription in = {0};
	in.mFormatID = kAudioFormatMPEG4AAC;
	UInt32 sz = sizeof(in);
	OSStatus st = AudioFormatGetProperty(kAudioFormatProperty_FormatInfo, cookieSize, cookie, &sz, &in);
	if (st != noErr) {
		return st;
	}
	AudioStreamBasicDescription out = {0};
	out.mSampleRate = in.mSampleRate;
	out.mFormatID = kAudioFormatLinearPCM;
	out.mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked;
	out.mChannelsPerFrame = in.mChannelsPerFrame;
	out.mBitsPerChannel = 32;
	out.mBytesPerFrame = 4 * in.mChannelsPerFrame;
	out.mFramesPerPacket = 1;
	out.mBytesPerPacket = out.mBytesPerFrame;
	st = AudioConverterNew(&in, &out, conv);
	if (st != noErr) {
		return st;
	}
	AudioConverterSetProperty(*conv, kAudioConverterDecompressionMagicCookie, cookieSize, cookie);
	*rate = out.mSampleRate;
	*channels = out.mChannelsPerFrame;
	return noErr;
}

// atDecode decodifica un bloque en out (outFrames como mucho); got es
// cuántos salieron.
static OSStatus atDecode(AudioConverterRef c, const void *pkt, UInt32 size,
                         float *out, UInt32 outFrames, UInt32 channels, UInt32 *got) {
	atInput in = {pkt, size, 0};
	AudioBufferList bl;
	bl.mNumberBuffers = 1;
	bl.mBuffers[0].mNumberChannels = channels;
	bl.mBuffers[0].mDataByteSize = outFrames * channels * 4;
	bl.mBuffers[0].mData = out;
	UInt32 n = outFrames;
	OSStatus st = AudioConverterFillComplexBuffer(c, atFeed, &in, &n, &bl, NULL);
	*got = n;
	return st == 1 ? noErr : st;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// atCodec es aacCodec con AudioToolbox.
type atCodec struct {
	conv     C.AudioConverterRef
	rate     int
	channels int
	out      []float32
	cookie   []byte
}

func newPlatformAAC(config []byte) (aacCodec, error) {
	if len(config) == 0 {
		return nil, fmt.Errorf("AAC without a configuration")
	}
	c := &atCodec{cookie: esdsCookie(config)}
	var rate C.Float64
	var ch C.UInt32
	ck := C.CBytes(c.cookie)
	defer C.free(ck)
	if st := C.atNew(ck, C.UInt32(len(c.cookie)), &c.conv, &rate, &ch); st != 0 {
		return nil, fmt.Errorf("AudioConverterNew: OSStatus %d", int32(st))
	}
	c.rate, c.channels = int(rate), int(ch)
	c.out = make([]float32, aacMaxFrame*max(1, c.channels))
	return c, nil
}

func (c *atCodec) decode(in []byte, out []float32) ([]float32, int, error) {
	pkt := C.CBytes(in)
	defer C.free(pkt)
	frames := len(c.out) / max(1, c.channels)
	var got C.UInt32
	if st := C.atDecode(c.conv, pkt, C.UInt32(len(in)), (*C.float)(unsafe.Pointer(&c.out[0])),
		C.UInt32(frames), C.UInt32(c.channels), &got); st != 0 {
		return out, c.rate, nil // un bloque dañado: se salta
	}
	ch := c.channels
	for k := 0; k < int(got); k++ {
		l := c.out[k*ch]
		r := l
		if ch > 1 {
			r = c.out[k*ch+1]
		}
		out = append(out, l, r)
	}
	return out, c.rate, nil
}

func (c *atCodec) reset() { C.AudioConverterReset(c.conv) }

func (c *atCodec) close() {
	if c.conv != nil {
		C.AudioConverterDispose(c.conv)
		c.conv = nil
	}
}
