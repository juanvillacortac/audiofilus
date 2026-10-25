package io.github.juanvillacortac.audiofilus

import android.content.Context
import android.net.wifi.WifiManager
import dev.audiofilus.mobile.Multicast

// MulticastHold toma el MulticastLock del Wi-Fi mientras audiofilus lo pide
// (DLNA o el servidor MPD encendidos): sin él, Android tira los paquetes
// multicast, y otros aparatos no encuentran el teléfono por SSDP ni mDNS.
// Gasta batería, así que solo mientras hace falta.
class MulticastHold(context: Context) : Multicast {
    private val lock = (context.getSystemService(Context.WIFI_SERVICE) as WifiManager)
        .createMulticastLock("audiofilus").apply { setReferenceCounted(false) }

    override fun hold(on: Boolean) {
        if (on) lock.acquire() else if (lock.isHeld) lock.release()
    }
}
