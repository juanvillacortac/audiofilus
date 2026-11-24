// SPDX-License-Identifier: GPL-2.0-or-later
//
// Este archivo se enlaza con libvgm, que en conjunto es GPL-2.0 (algunos de
// sus núcleos, como ym2151.c, lo son): por eso la librería que sale de aquí
// (libvgmshim) se distribuye bajo la GPL-2.0, aparte de audiofilus (MIT), que
// solo la carga en tiempo de ejecución. El texto de la licencia va en
// tools/libvgm/GPL-2.0.txt y en licenses/ de cada paquete.
//
// Una interfaz en C mínima sobre libvgm (ValleyBell), para cargarla con
// purego desde Go (internal/decode/vgm.go): abrir un VGM/VGZ (también S98,
// DRO y GYM), sacar estéreo de 16 bits, saber cuánto dura (con sus vueltas y
// el fundido), saltar y leer sus etiquetas GD3. libvgm es C++: esta capa la
// envuelve en funciones extern "C" y se compila, con libvgm y zlib dentro,
// como una sola biblioteca (libvgmshim.so) sin dependencias más que la libc.
#include <stdlib.h>
#include <string.h>
#include <string>

#include "player/playerbase.hpp"
#include "player/vgmplayer.hpp"
#include "player/s98player.hpp"
#include "player/droplayer.hpp"
#include "player/gymplayer.hpp"
#include "player/playera.hpp"
#include "utils/DataLoader.h"
#include "utils/FileLoader.h"
#include "emu/SoundDevs.h"
#include "emu/EmuCores.h"

// VGM_ACCURATE es la bandera de vgm_open_ex que pide los núcleos más fieles
// (Nuked) en vez de los de siempre, más rápidos.
#define VGM_ACCURATE 0x01

// accurateCore es un chip con un núcleo más fiel que el que libvgm usa por
// defecto: core va al chip, linked a su chip enlazado (0 = el de siempre).
struct accurateCore {
	DEV_ID dev;
	UINT32 core;
	UINT32 linked;
};

// accurateCores son los chips con un núcleo de Nuked en esta versión de
// libvgm: el YM2612/YM3438 (Nuked OPN2, en vez de Genesis Plus GX), el YM2151
// (Nuked OPM, en vez de MAME), el YM2413 (Nuked OPLL, en vez de EMU2413), el
// YMF262 (Nuked OPL3, en vez de AdLibEmu), el YM3812 (también con Nuked OPL3,
// que lo emula en su modo OPL2) y la parte FM del YMF278B (OPL4), que es un
// YMF262 enlazado. Los demás chips o solo tienen un núcleo, o el de por
// defecto ya es el más fiel (SN76496 de MAME, Game Boy de SameBoy, NES de
// NSFPlay, QSound de superctr): se quedan como están. Pedir un núcleo que un
// chip no tiene haría que no arrancase, por eso la lista es cerrada.
static const accurateCore accurateCores[] = {
	{DEVID_YM2612, FCC_NUKE, 0},
	{DEVID_YM2151, FCC_NUKE, 0},
	{DEVID_YM2413, FCC_NUKE, 0},
	{DEVID_YMF262, FCC_NUKE, 0},
	{DEVID_YM3812, FCC_NUKE, 0},
	{DEVID_YMF278B, 0, FCC_NUKE},
};

// useAccurateCores pide a eng (ya con el archivo cargado y antes de Start,
// que es cuando crea los chips) los núcleos de accurateCores, para las dos
// instancias de cada chip. Los reproductores que no tienen un chip (el de GYM
// solo tiene YM2612 y SN76496, el de DRO solo OPL2 y OPL3) dicen que no y se
// sigue.
static void useAccurateCores(PlayerBase* eng) {
	for (size_t i = 0; i < sizeof(accurateCores) / sizeof(accurateCores[0]); i++) {
		const accurateCore& c = accurateCores[i];
		for (UINT32 inst = 0; inst < 2; inst++) {
			UINT32 id = PLR_DEV_ID(c.dev, inst);
			PLR_DEV_OPTS opts;
			if (eng->GetDeviceOptions(id, opts))
				continue;
			if (c.core)
				opts.emuCore[0] = c.core;
			if (c.linked)
				opts.emuCore[1] = c.linked;
			eng->SetDeviceOptions(id, opts);
		}
	}
}

// vgmHandle es una canción abierta.
struct vgmHandle {
	PlayerA player;
	DATA_LOADER* loader;
	UINT32 rate;
	UINT32 total;  // cuadros que suena en total (con las vueltas y el fundido)
	UINT32 pos;    // cuadros ya dados
	int loops;     // tiene un tramo que da la vuelta
	std::string tag;
};

extern "C" {

// vgm_open_ex abre path a rate cuadros por segundo; loops son las vueltas
// antes del fundido de fadeMs. flags: VGM_ACCURATE pide los núcleos más
// fieles (ver accurateCores). NULL si no se puede.
void* vgm_open_ex(const char* path, int rate, int loops, int fadeMs, int flags) {
	vgmHandle* h = new vgmHandle();
	h->rate = (UINT32)rate;
	h->pos = 0;
	h->player.RegisterPlayerEngine(new VGMPlayer);
	h->player.RegisterPlayerEngine(new S98Player);
	h->player.RegisterPlayerEngine(new DROPlayer);
	h->player.RegisterPlayerEngine(new GYMPlayer);
	if (h->player.SetOutputSettings(h->rate, 2, 16, 2048)) {
		delete h;
		return NULL;
	}
	PlayerA::Config cfg = h->player.GetConfiguration();
	cfg.masterVol = 0x10000;
	cfg.loopCount = (UINT32)loops;
	cfg.fadeSmpls = (UINT32)((UINT64)rate * (UINT32)fadeMs / 1000);
	cfg.endSilenceSmpls = 0;
	cfg.pbSpeed = 1.0;
	h->player.SetConfiguration(cfg);
	h->loader = FileLoader_Init(path);
	if (h->loader == NULL) {
		delete h;
		return NULL;
	}
	DataLoader_SetPreloadBytes(h->loader, 0x100);
	if (DataLoader_Load(h->loader) || h->player.LoadFile(h->loader)) {
		DataLoader_Deinit(h->loader);
		delete h;
		return NULL;
	}
	PlayerBase* eng = h->player.GetPlayer();
	if (flags & VGM_ACCURATE)
		useAccurateCores(eng);
	if (eng->GetPlayerType() == FCC_VGM) {
		VGMPlayer* v = dynamic_cast<VGMPlayer*>(eng);
		h->player.SetLoopCount(v->GetModifiedLoopCount((UINT32)loops));
	}
	h->player.Start();
	h->loops = eng->GetLoopTicks() > 0;
	h->total = eng->Tick2Sample(eng->GetTotalPlayTicks(h->player.GetLoopCount()));
	if (h->loops)
		h->total += h->player.GetFadeSamples();
	return h;
}

// vgm_open es vgm_open_ex con los núcleos de siempre (lo que exportaban las
// versiones anteriores de esta capa).
void* vgm_open(const char* path, int rate, int loops, int fadeMs) {
	return vgm_open_ex(path, rate, loops, fadeMs, 0);
}

// vgm_render llena buf con frames cuadros estéreo de 16 bits; devuelve los
// que dio (0 al final).
int vgm_render(void* p, short* buf, int frames) {
	vgmHandle* h = (vgmHandle*)p;
	if (h->pos >= h->total)
		return 0;
	UINT32 n = (UINT32)frames;
	if (n > h->total - h->pos)
		n = h->total - h->pos;
	memset(buf, 0, n * 4);
	UINT32 got = h->player.Render(n * 4, buf) / 4;
	h->pos += got;
	return (int)got;
}

// vgm_length_ms es lo que dura, en ms.
int vgm_length_ms(void* p) {
	vgmHandle* h = (vgmHandle*)p;
	return (int)((UINT64)h->total * 1000 / h->rate);
}

// vgm_loops dice si tiene un tramo que da la vuelta (y se apaga al final).
int vgm_loops(void* p) { return ((vgmHandle*)p)->loops; }

// vgm_seek salta a ms.
int vgm_seek(void* p, int ms) {
	vgmHandle* h = (vgmHandle*)p;
	UINT32 smpl = (UINT32)((UINT64)ms * h->rate / 1000);
	if (h->player.Seek(PLAYPOS_SAMPLE, smpl))
		return 1;
	h->pos = smpl;
	return 0;
}

// vgm_tag es la etiqueta key ("TITLE", "GAME", "SYSTEM", "ARTIST", con
// "-JPN" las japonesas), en UTF-8; "" si no está. Vale hasta la siguiente.
const char* vgm_tag(void* p, const char* key) {
	vgmHandle* h = (vgmHandle*)p;
	const char* const* tags = h->player.GetPlayer()->GetTags();
	h->tag.clear();
	for (; tags != NULL && tags[0] != NULL; tags += 2) {
		if (strcmp(tags[0], key) == 0 && tags[1] != NULL) {
			h->tag = tags[1];
			break;
		}
	}
	return h->tag.c_str();
}

// vgm_close cierra la canción.
void vgm_close(void* p) {
	vgmHandle* h = (vgmHandle*)p;
	if (h == NULL)
		return;
	h->player.Stop();
	h->player.UnloadFile();
	h->player.UnregisterAllPlayers();
	DataLoader_Deinit(h->loader);
	delete h;
}

}
