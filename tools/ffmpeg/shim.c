// SPDX-License-Identifier: LGPL-2.1-or-later
//
// Este archivo se enlaza con FFmpeg (LGPL-2.1 o posterior, compilado sin las
// partes GPL): la librería que sale de aquí (libavshim) se distribuye bajo la
// LGPL-2.1, aparte de audiofilus (MIT), que solo la carga en tiempo de
// ejecución. tools/ffmpeg/build.sh dice de qué versión y con qué opciones.
//
// Una interfaz en C mínima sobre libavformat y libavcodec, para cargarla con
// purego desde Go (internal/decode/av.go): abrir un archivo, sacar estéreo
// float32 intercalado a su frecuencia, saber cuánto dura y saltar. Así Go no
// lee ninguna estructura de FFmpeg (cambian entre versiones y entre 32 y 64
// bits): todo eso queda aquí, compilado contra la versión que se lleva.
#include <stdlib.h>
#include <string.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/samplefmt.h>

// avHandle es una canción abierta.
typedef struct {
	AVFormatContext* fmt;
	AVCodecContext* dec;
	AVPacket* pkt;
	AVFrame* frame;
	int stream;
	int rate;
	int eof;          // el demuxer ya no da más paquetes
	float* pend;      // estéreo ya decodificado que no cupo en la última lectura
	int pendLen;      // cuadros en pend
	int pendOff;      // primer cuadro de pend sin entregar
	int pendCap;      // cuadros que caben en pend
	long long skipTo; // tras un salto: cuadro desde el que se entrega (-1: nada que descartar)
	// base es la marca de tiempo del primer bloque (el cuadro 0): en WMA los
	// bloques llevan la del paquete, que va unos bloques por delante del
	// audio que sale de verdad.
	long long base;
	int haveBase;
	int late;       // tras un salto, el primer bloque empezaba después del cuadro pedido
	long long next; // cuadro en que empieza el siguiente bloque (-1: no se sabe)
} avHandle;

// frameAt es el cuadro en que empieza un bloque de n cuadros con la marca
// ts. ASF guarda milisegundos: se redondea al múltiplo de n más cercano (los
// bloques de WMA son todos del mismo tamaño), si cae cerca.
static long long frameAt(avHandle* h, long long ts, int n) {
	AVStream* st = h->fmt->streams[h->stream];
	long long at = av_rescale_q(ts - h->base, st->time_base, (AVRational){1, h->rate});
	if (n > 0) {
		long long k = (at + n / 2) / n;
		long long d = at - k * n;
		if (d < 0) d = -d;
		if (d <= n / 4) at = k * n;
	}
	return at;
}

// sample lee la muestra i (de las intercaladas, o del plano de un canal) en
// el formato fmt, como float.
static float sample(const uint8_t* p, enum AVSampleFormat fmt, int i) {
	switch (fmt) {
	case AV_SAMPLE_FMT_U8:
		return ((float)p[i] - 128.0f) / 128.0f;
	case AV_SAMPLE_FMT_S16:
		return (float)((const int16_t*)p)[i] / 32768.0f;
	case AV_SAMPLE_FMT_S32:
		return (float)((double)((const int32_t*)p)[i] / 2147483648.0);
	case AV_SAMPLE_FMT_FLT:
		return ((const float*)p)[i];
	case AV_SAMPLE_FMT_DBL:
		return (float)((const double*)p)[i];
	default:
		return 0;
	}
}

// toStereo pasa el cuadro decodificado a estéreo float intercalado en pend.
// Mono va a los dos lados; con más de dos canales se toman los dos primeros
// (izquierdo y derecho delanteros en todos los diseños de FFmpeg).
static int toStereo(avHandle* h) {
	AVFrame* f = h->frame;
	int n = f->nb_samples, ch = f->ch_layout.nb_channels;
	if (n <= 0 || ch <= 0) {
		h->pendLen = h->pendOff = 0;
		return 0;
	}
	if (n > h->pendCap) {
		float* p = realloc(h->pend, (size_t)n * 2 * sizeof(float));
		if (!p) return -1;
		h->pend = p;
		h->pendCap = n;
	}
	enum AVSampleFormat fmt = (enum AVSampleFormat)f->format;
	enum AVSampleFormat packed = av_get_packed_sample_fmt(fmt);
	int planar = av_sample_fmt_is_planar(fmt);
	int r = ch > 1 ? 1 : 0;
	for (int i = 0; i < n; i++) {
		float L, R;
		if (planar) {
			L = sample(f->extended_data[0], packed, i);
			R = sample(f->extended_data[r], packed, i);
		} else {
			L = sample(f->extended_data[0], packed, i * ch);
			R = sample(f->extended_data[0], packed, i * ch + r);
		}
		h->pend[i * 2] = L;
		h->pend[i * 2 + 1] = R;
	}
	h->pendLen = n;
	h->pendOff = 0;
	long long ts = f->best_effort_timestamp;
	if (!h->haveBase && ts != AV_NOPTS_VALUE) {
		h->base = ts;
		h->haveBase = 1;
	}
	// Dónde empieza: por su marca o, sin ella (el último bloque, que sale
	// al vaciar el decodificador), justo tras el anterior.
	long long at = ts != AV_NOPTS_VALUE ? frameAt(h, ts, n) : h->next;
	h->next = at >= 0 ? at + n : -1;
	// Tras un salto: lo que quede antes del cuadro pedido se descarta.
	if (h->skipTo >= 0 && at >= 0) {
		long long skip = h->skipTo - at;
		if (skip >= n) {
			h->pendLen = 0; // todo antes: el siguiente bloque
			return 0;
		}
		if (skip > 0) h->pendOff = (int)skip;
		if (skip < 0) h->late = 1;
		h->skipTo = -1;
	}
	return 0;
}

// decodeMore decodifica hasta tener algo en pend. 0 si hay algo, 1 al
// terminar la canción, -1 si falla.
static int decodeMore(avHandle* h) {
	for (;;) {
		int err = avcodec_receive_frame(h->dec, h->frame);
		if (err == 0) {
			int e = toStereo(h);
			av_frame_unref(h->frame);
			if (e < 0) return -1;
			if (h->pendLen > h->pendOff) return 0;
			continue;
		}
		if (err == AVERROR_EOF) return 1;
		if (err != AVERROR(EAGAIN)) return -1;
		if (h->eof) return 1;
		// Hace falta otro paquete.
		err = av_read_frame(h->fmt, h->pkt);
		if (err < 0) {
			h->eof = 1;
			avcodec_send_packet(h->dec, NULL); // vaciar lo que quede dentro
			continue;
		}
		if (h->pkt->stream_index == h->stream) {
			// Un paquete dañado no corta la canción: se salta.
			avcodec_send_packet(h->dec, h->pkt);
		}
		av_packet_unref(h->pkt);
	}
}

void afav_close(void* p);

// afav_open abre path. Devuelve su frecuencia en rate y su duración en
// cuadros en frames (-1 si no se sabe). NULL si no se puede abrir o no tiene
// audio que esta compilación sepa decodificar.
void* afav_open(const char* path, int* rate, long long* frames) {
	av_log_set_level(AV_LOG_QUIET);
	avHandle* h = calloc(1, sizeof(avHandle));
	if (!h) return NULL;
	h->skipTo = h->next = -1;
	if (avformat_open_input(&h->fmt, path, NULL, NULL) < 0) goto fail;
	if (avformat_find_stream_info(h->fmt, NULL) < 0) goto fail;
	const AVCodec* codec = NULL;
	h->stream = av_find_best_stream(h->fmt, AVMEDIA_TYPE_AUDIO, -1, -1, &codec, 0);
	if (h->stream < 0 || !codec) goto fail;
	AVStream* st = h->fmt->streams[h->stream];
	h->dec = avcodec_alloc_context3(codec);
	if (!h->dec) goto fail;
	if (avcodec_parameters_to_context(h->dec, st->codecpar) < 0) goto fail;
	if (avcodec_open2(h->dec, codec, NULL) < 0) goto fail;
	h->rate = h->dec->sample_rate;
	if (h->rate <= 0) goto fail;
	h->pkt = av_packet_alloc();
	h->frame = av_frame_alloc();
	if (!h->pkt || !h->frame) goto fail;
	// Solo se lee el flujo elegido.
	for (unsigned i = 0; i < h->fmt->nb_streams; i++)
		if ((int)i != h->stream) h->fmt->streams[i]->discard = AVDISCARD_ALL;

	long long n = -1;
	if (st->duration != AV_NOPTS_VALUE && st->duration > 0)
		n = av_rescale_q(st->duration, st->time_base, (AVRational){1, h->rate});
	else if (h->fmt->duration != AV_NOPTS_VALUE && h->fmt->duration > 0)
		n = av_rescale(h->fmt->duration, h->rate, AV_TIME_BASE);
	// El primer bloque, para saber la marca del cuadro 0 (base); se queda
	// en pend para la primera lectura.
	if (decodeMore(h) < 0) goto fail;
	*rate = h->rate;
	*frames = n;
	return h;
fail:
	afav_close(h);
	return NULL;
}

// afav_read deja hasta n cuadros estéreo float intercalados en out. Devuelve
// cuántos (0 al terminar la canción, -1 si falla).
int afav_read(void* p, float* out, int n) {
	avHandle* h = p;
	int got = 0;
	while (got < n) {
		if (h->pendLen <= h->pendOff) {
			int e = decodeMore(h);
			if (e < 0) return got > 0 ? got : -1;
			if (e > 0) break;
		}
		int take = h->pendLen - h->pendOff;
		if (take > n - got) take = n - got;
		memcpy(out + got * 2, h->pend + h->pendOff * 2, (size_t)take * 2 * sizeof(float));
		h->pendOff += take;
		got += take;
	}
	return got;
}

// afav_seek salta al cuadro frame: a un paquete anterior y, al decodificar,
// se descarta lo que quede antes. El salto de ASF es aproximado y a veces
// cae después: entonces se vuelve a saltar, cada vez más atrás (y al final
// desde el principio). 0 si pudo.
int afav_seek(void* p, long long frame) {
	avHandle* h = p;
	AVStream* st = h->fmt->streams[h->stream];
	long long ts = av_rescale_q(frame, (AVRational){1, h->rate}, st->time_base) + h->base;
	long long start = st->start_time != AV_NOPTS_VALUE ? st->start_time : 0;
	static const int backMs[] = {0, 500, 2000, 10000, -1}; // -1: desde el principio
	for (int i = 0; i < 5; i++) {
		long long at = start;
		if (backMs[i] >= 0) {
			at = ts - av_rescale_q(backMs[i], (AVRational){1, 1000}, st->time_base);
			if (at < start) at = start;
		}
		if (av_seek_frame(h->fmt, h->stream, at, AVSEEK_FLAG_BACKWARD) < 0 &&
		    av_seek_frame(h->fmt, h->stream, start, AVSEEK_FLAG_BACKWARD | AVSEEK_FLAG_ANY) < 0)
			return -1;
		avcodec_flush_buffers(h->dec);
		h->eof = 0;
		h->pendLen = h->pendOff = 0;
		h->skipTo = frame;
		h->late = 0;
		h->next = -1;
		// El primer bloque que sirve: si empieza después de frame, se cayó
		// tarde (salvo desde el principio, donde no hay nada antes).
		int e = decodeMore(h);
		if (e < 0) return -1;
		if (e > 0 || !h->late || at <= start) break;
	}
	return 0;
}

// afav_close cierra la canción.
void afav_close(void* p) {
	avHandle* h = p;
	if (!h) return;
	av_frame_free(&h->frame);
	av_packet_free(&h->pkt);
	avcodec_free_context(&h->dec);
	avformat_close_input(&h->fmt);
	free(h->pend);
	free(h);
}
