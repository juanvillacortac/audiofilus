// Store es lo que cambia entre la versión de GitHub y la de Google Play. En
// la de GitHub, audiofilus se actualiza solo: Ajustes → Actualizar baja el
// APK de la última release, lo comprueba con su SHA256SUMS (en Go,
// internal/update) y se lo pasa a installPackage, que lo instala con el
// PackageInstaller del sistema (el usuario confirma).
package io.github.juanvillacortac.audiofilus

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build
import android.util.Log
import dev.audiofilus.mobile.Installer
import dev.audiofilus.mobile.Mobile
import java.io.File

object Store {
    fun setup(ctx: Context) {
        val app = ctx.applicationContext
        Mobile.setInstaller(object : Installer {
            override fun installPackage(path: String) {
                Thread { install(app, path) }.start()
            }
        })
    }

    // install escribe el APK en una sesión del PackageInstaller y la cierra:
    // el sistema le pide confirmar al usuario (InstallReceiver) y, si nunca
    // dejó a audiofilus instalar apps, primero ese permiso.
    private fun install(ctx: Context, path: String) {
        try {
            val pi = ctx.packageManager.packageInstaller
            val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
            if (Build.VERSION.SDK_INT >= 31) {
                params.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
            }
            val id = pi.createSession(params)
            pi.openSession(id).use { s ->
                val f = File(path)
                f.inputStream().use { input ->
                    s.openWrite("audiofilus.apk", 0, f.length()).use { out ->
                        input.copyTo(out)
                        s.fsync(out)
                    }
                }
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                    (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
                val done = PendingIntent.getBroadcast(ctx, id, Intent(ctx, InstallReceiver::class.java), flags)
                s.commit(done.intentSender)
            }
        } catch (e: Exception) {
            Log.w("audiofilus", "install update: $e")
        }
    }
}

// InstallReceiver recibe cómo va la instalación: si hace falta que el usuario
// la confirme, abre la pantalla del sistema para eso.
class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            @Suppress("DEPRECATION")
            val confirm = if (Build.VERSION.SDK_INT >= 33) {
                intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
            } else {
                intent.getParcelableExtra(Intent.EXTRA_INTENT)
            }
            confirm?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)?.let { ctx.startActivity(it) }
        } else if (status != PackageInstaller.STATUS_SUCCESS) {
            Log.w("audiofilus", "install update: status $status ${intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)}")
        }
    }
}
