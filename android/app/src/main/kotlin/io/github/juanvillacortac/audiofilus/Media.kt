// Media es lo que Android sabe de lo que suena: la sesión de medios (la
// notificación, la pantalla bloqueada, los botones de los audífonos y del
// Bluetooth, Android Auto) y la notificación de PlaybackService. Cada botón
// del sistema manda su orden a audiofilus; audiofilus le cuenta qué suena con
// update (por su NowPlayingHook).
package io.github.juanvillacortac.audiofilus

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import dev.audiofilus.mobile.Mobile

object Media {
    const val CHANNEL = "playback"
    const val NOTIFICATION = 1

    private val main = Handler(Looper.getMainLooper())
    private var session: MediaSession? = null

    // Lo último que contó audiofilus.
    var title = ""
        private set
    var artist = ""
        private set
    var playing = false
        private set
    private var art: Bitmap? = null
    private var artBytes: ByteArray? = null
    private var started = false

    // init crea la sesión (una por proceso).
    fun init(ctx: Context) {
        if (session != null) return
        val s = MediaSession(ctx, "audiofilus")
        s.setCallback(object : MediaSession.Callback() {
            override fun onPlay() = Mobile.play()
            override fun onPause() = Mobile.pause()
            override fun onStop() = Mobile.pause()
            override fun onSkipToNext() = Mobile.next()
            override fun onSkipToPrevious() = Mobile.previous()
            override fun onSeekTo(pos: Long) = Mobile.seekTo(pos / 1000.0)
        })
        s.setSessionActivity(openApp(ctx))
        s.isActive = true
        session = s
        val nm = ctx.getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26) {
            val ch = NotificationChannel(CHANNEL, ctx.getString(R.string.channel_playback), NotificationManager.IMPORTANCE_LOW)
            ch.setShowBadge(false)
            nm.createNotificationChannel(ch)
        }
    }

    // update lo llama audiofilus (desde su hilo) cuando cambia lo que suena.
    fun update(
        ctx: Context, title: String, artist: String, album: String,
        duration: Double, position: Double, playing: Boolean, artwork: ByteArray?,
    ) {
        main.post {
            val s = session ?: return@post
            this.title = title
            this.artist = artist
            this.playing = playing
            if (artwork == null) {
                art = null
                artBytes = null
            } else if (!artwork.contentEquals(artBytes)) {
                art = BitmapFactory.decodeByteArray(artwork, 0, artwork.size)
                artBytes = artwork
            }
            val meta = MediaMetadata.Builder()
                .putString(MediaMetadata.METADATA_KEY_TITLE, title)
                .putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
                .putString(MediaMetadata.METADATA_KEY_ALBUM, album)
                .putLong(MediaMetadata.METADATA_KEY_DURATION, if (duration > 0) (duration * 1000).toLong() else -1L)
            art?.let { meta.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, it) }
            s.setMetadata(meta.build())
            var actions = PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PAUSE or
                PlaybackState.ACTION_PLAY_PAUSE or PlaybackState.ACTION_SKIP_TO_NEXT or
                PlaybackState.ACTION_SKIP_TO_PREVIOUS or PlaybackState.ACTION_STOP
            if (duration > 0) actions = actions or PlaybackState.ACTION_SEEK_TO
            s.setPlaybackState(
                PlaybackState.Builder()
                    .setActions(actions)
                    .setState(
                        if (playing) PlaybackState.STATE_PLAYING else PlaybackState.STATE_PAUSED,
                        (position * 1000).toLong(), if (playing) 1f else 0f,
                    )
                    .build(),
            )
            // Sonando, el servicio en primer plano mantiene viva la app; en
            // pausa queda solo la notificación (que se puede quitar).
            if (playing && !started) {
                try {
                    ctx.startForegroundService(Intent(ctx, PlaybackService::class.java))
                    started = true
                } catch (e: Exception) {
                    Log.w("audiofilus", "playback service: $e")
                }
            } else if (started || title.isNotEmpty()) {
                PlaybackService.refresh(ctx)
            }
        }
    }

    // stopped: el servicio terminó (se puede volver a empezar).
    fun stopped() {
        started = false
    }

    // notification es la de lo que suena, con sus botones.
    fun notification(ctx: Context): Notification {
        val b = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(ctx, CHANNEL) else @Suppress("DEPRECATION") Notification.Builder(ctx)
        b.setSmallIcon(R.drawable.ic_stat_audiofilus)
            .setContentTitle(title.ifEmpty { "audiofilus" })
            .setContentText(artist)
            .setLargeIcon(art)
            .setContentIntent(openApp(ctx))
            .setDeleteIntent(action(ctx, PlaybackService.ACTION_DISMISS))
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setOngoing(playing)
            .setShowWhen(false)
            .addAction(Notification.Action.Builder(null, "Previous", action(ctx, PlaybackService.ACTION_PREV)).build())
            .addAction(
                Notification.Action.Builder(
                    null, if (playing) "Pause" else "Play", action(ctx, PlaybackService.ACTION_TOGGLE),
                ).build(),
            )
            .addAction(Notification.Action.Builder(null, "Next", action(ctx, PlaybackService.ACTION_NEXT)).build())
            .setStyle(
                Notification.MediaStyle()
                    .setMediaSession(session?.sessionToken)
                    .setShowActionsInCompactView(0, 1, 2),
            )
        return b.build()
    }

    private fun openApp(ctx: Context): PendingIntent = PendingIntent.getActivity(
        ctx, 0, Intent(ctx, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun action(ctx: Context, what: String): PendingIntent = PendingIntent.getService(
        ctx, what.hashCode(), Intent(ctx, PlaybackService::class.java).setAction(what),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
}
