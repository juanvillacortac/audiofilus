// PlaybackService mantiene viva la app mientras suena la música (un
// servicio en primer plano de tipo mediaPlayback, con la notificación de
// Media): el servicio de audio de audiofilus corre en una goroutine de este
// proceso, y sin esto Android lo cerraría al salir de la app.
package io.github.juanvillacortac.audiofilus

import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import dev.audiofilus.mobile.Mobile

class PlaybackService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        current = this
        foreground()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_PREV -> Mobile.previous()
            ACTION_TOGGLE -> Mobile.toggle()
            ACTION_NEXT -> Mobile.next()
            ACTION_DISMISS -> if (!Media.playing) stopSelf()
        }
        return START_NOT_STICKY
    }

    // foreground pone (o quita) el servicio en primer plano según suene o
    // no; la notificación queda en los dos casos.
    private fun foreground() {
        val n = Media.notification(this)
        if (Media.playing) {
            if (Build.VERSION.SDK_INT >= 29) {
                startForeground(Media.NOTIFICATION, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
            } else {
                startForeground(Media.NOTIFICATION, n)
            }
        } else {
            stopForeground(STOP_FOREGROUND_DETACH)
            getSystemService(NotificationManager::class.java).notify(Media.NOTIFICATION, n)
        }
    }

    // Si se cierra la app desde Recientes con la música en pausa, el
    // servicio se va con ella; sonando, sigue.
    override fun onTaskRemoved(rootIntent: Intent?) {
        if (!Media.playing) stopSelf()
    }

    override fun onDestroy() {
        current = null
        Media.stopped()
        getSystemService(NotificationManager::class.java).cancel(Media.NOTIFICATION)
        super.onDestroy()
    }

    companion object {
        const val ACTION_PREV = "prev"
        const val ACTION_TOGGLE = "toggle"
        const val ACTION_NEXT = "next"
        const val ACTION_DISMISS = "dismiss"

        private var current: PlaybackService? = null

        // refresh actualiza la notificación (y el primer plano) con lo que
        // suena ahora.
        fun refresh(ctx: Context) {
            val s = current
            if (s != null) {
                s.foreground()
            } else if (Media.title.isNotEmpty()) {
                ctx.getSystemService(NotificationManager::class.java).notify(Media.NOTIFICATION, Media.notification(ctx))
            }
        }
    }
}
