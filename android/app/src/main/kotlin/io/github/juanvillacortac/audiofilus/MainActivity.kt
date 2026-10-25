// La app de Android: la vista de Ebitengine con audiofilus (la biblioteca
// mobile.aar, hecha con ebitenmobile desde mobile/), a pantalla completa, y
// lo que audiofilus no puede hacer desde Go: los permisos para leer la
// música, el selector de carpetas, el teclado del sistema y, en Media y
// PlaybackService, la sesión de medios y la notificación que dejan sonar la
// música en segundo plano. Es lo mismo que AppDelegate.swift en iOS.
package io.github.juanvillacortac.audiofilus

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.DocumentsContract
import android.provider.Settings
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.util.Log
import android.view.InputDevice
import android.view.KeyEvent
import android.view.View
import android.view.WindowInsets
import android.view.WindowManager
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.Toast
import android.window.OnBackInvokedDispatcher
import dev.audiofilus.mobile.EbitenView
import dev.audiofilus.mobile.Host
import dev.audiofilus.mobile.Mobile
import java.io.File

class MainActivity : Activity(), Host {
    private lateinit var game: GameView
    private lateinit var keyboard: EditText
    // settingText: el texto lo pone audiofilus, no el usuario (no se le
    // devuelve). keyboardShown: el teclado está abierto para audiofilus.
    private var settingText = false
    private var keyboardShown = false
    private var imeVisible = false
    // home es la carpeta de audiofilus (homeDir).
    private var home = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        edgeToEdge()

        Media.init(applicationContext)
        Mobile.setHost(this)
        Mobile.setDeviceName(deviceName())
        Store.setup(this)
        home = homeDir().path
        Mobile.start(home, cacheDir.path, languages())

        val root = FrameLayout(this)
        game = GameView(this)
        root.addView(game, FrameLayout.LayoutParams(-1, -1))
        keyboard = hiddenKeyboard()
        root.addView(keyboard, FrameLayout.LayoutParams(1, 1))
        root.setOnApplyWindowInsetsListener { _, insets -> onInsets(insets); insets }
        setContentView(root)
        game.requestFocus()

        askPermissions()
        if (Build.VERSION.SDK_INT >= 33) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
    }

    // back es el gesto Atrás: vuelve atrás en audiofilus (como B) y, si ya
    // no hay adónde, la app pasa a segundo plano (la música sigue).
    private fun back() {
        if (!Mobile.back()) moveTaskToBack(true)
    }

    @Deprecated("Deprecated in Java")
    @Suppress("DEPRECATION")
    override fun onBackPressed() {
        back()
    }

    override fun onPause() {
        super.onPause()
        game.suspendGame()
    }

    override fun onResume() {
        super.onResume()
        game.resumeGame()
    }

    // homeDir es la carpeta de audiofilus (audiofilus.json, data/, el log):
    // la de la app en el almacenamiento compartido, que se ve desde el PC por
    // USB (Android/data/...); si no está o no se puede usar (archivos de una
    // instalación anterior con otro dueño), la interna.
    private fun homeDir(): File {
        val ext = getExternalFilesDir(null)
        if (ext != null && usable(ext)) return ext
        Log.w(TAG, "home: $ext not usable, using ${filesDir.path}")
        filesDir.mkdirs()
        return filesDir
    }

    // usable dice si audiofilus puede escribir en dir y leer lo suyo que ya
    // haya ahí.
    private fun usable(dir: File): Boolean = try {
        dir.mkdirs()
        File(dir, ".probe").apply { writeText("ok"); delete() }
        listOf("audiofilus.json", "data").map { File(dir, it) }.all { f ->
            !f.exists() || f.canRead() && (!f.isDirectory || f.canWrite())
        }
    } catch (_: Exception) {
        false
    }

    // languages son los idiomas del sistema, "es-VE,en-US".
    private fun languages(): String {
        val list = resources.configuration.locales
        return (0 until list.size()).joinToString(",") { list.get(it).toLanguageTag() }
    }

    // edgeToEdge dibuja por detrás de las barras del sistema y del recorte
    // de la cámara; audiofilus pone su contenido dentro de la zona segura
    // (onInsets).
    @Suppress("DEPRECATION")
    private fun edgeToEdge() {
        if (Build.VERSION.SDK_INT >= 30) {
            window.setDecorFitsSystemWindows(false)
        } else {
            window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_LAYOUT_STABLE or
                View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
        }
        if (Build.VERSION.SDK_INT >= 28) {
            window.attributes.layoutInDisplayCutoutMode =
                WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
        }
    }

    // onInsets le dice a audiofilus qué tapan las barras y el recorte (en dp,
    // como mide Ebitengine) y sigue al teclado: si el usuario lo baja, se
    // deja de escribir.
    @Suppress("DEPRECATION")
    private fun onInsets(insets: WindowInsets) {
        val d = resources.displayMetrics.density.toDouble()
        val visible: Boolean
        if (Build.VERSION.SDK_INT >= 30) {
            val i = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout())
            Mobile.setSafeArea(i.top / d, i.left / d, i.bottom / d, i.right / d)
            visible = insets.isVisible(WindowInsets.Type.ime())
        } else {
            var t = insets.systemWindowInsetTop
            var l = insets.systemWindowInsetLeft
            var r = insets.systemWindowInsetRight
            var b = insets.stableInsetBottom
            if (Build.VERSION.SDK_INT >= 28) {
                insets.displayCutout?.let {
                    t = maxOf(t, it.safeInsetTop); l = maxOf(l, it.safeInsetLeft); r = maxOf(r, it.safeInsetRight)
                }
            }
            Mobile.setSafeArea(t / d, l / d, b / d, r / d)
            visible = insets.systemWindowInsetBottom > resources.displayMetrics.heightPixels / 5
        }
        if (imeVisible && !visible && keyboardShown) {
            keyboardShown = false
            game.requestFocus()
            Mobile.keyboardClosed()
        }
        imeVisible = visible
    }

    // hiddenKeyboard es un campo invisible: enfocarlo abre el teclado del
    // sistema, y lo que se escribe va a audiofilus (showKeyboard).
    private fun hiddenKeyboard(): EditText {
        val e = EditText(this)
        e.alpha = 0f
        e.isFocusableInTouchMode = true
        e.imeOptions = EditorInfo.IME_ACTION_DONE or EditorInfo.IME_FLAG_NO_EXTRACT_UI
        e.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) {
                if (!settingText) Mobile.keyboardText(s?.toString() ?: "")
            }
        })
        e.setOnEditorActionListener { _, action, _ ->
            if (action == EditorInfo.IME_ACTION_DONE || action == EditorInfo.IME_NULL) {
                Mobile.keyboardDone()
                true
            } else {
                false
            }
        }
        return e
    }

    // showKeyboard lo llama audiofilus (desde su hilo) cuando empieza o deja
    // de escribirse algo: la búsqueda, un nombre, una dirección o una
    // contraseña (secret).
    override fun showKeyboard(show: Boolean, text: String?, secret: Boolean) {
        runOnUiThread {
            val imm = getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
            if (show) {
                settingText = true
                keyboard.inputType = if (secret) {
                    InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
                } else {
                    InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                }
                keyboard.setText(text ?: "")
                keyboard.setSelection(keyboard.text.length)
                settingText = false
                keyboardShown = true
                keyboard.requestFocus()
                imm.showSoftInput(keyboard, InputMethodManager.SHOW_IMPLICIT)
            } else if (keyboardShown) {
                keyboardShown = false
                imm.hideSoftInputFromWindow(keyboard.windowToken, 0)
                game.requestFocus()
            }
        }
    }

    // pickFolder lo llama audiofilus (desde su hilo): el selector de
    // carpetas del sistema. La carpeta elegida se lee por su ruta (con el
    // permiso de audio), así que se convierte en una.
    override fun pickFolder() {
        runOnUiThread {
            try {
                startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE), PICK_FOLDER)
            } catch (e: Exception) {
                Log.w(TAG, "folder picker: $e")
            }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        val uri = data?.data
        if (requestCode != PICK_FOLDER || resultCode != RESULT_OK || uri == null) return
        try {
            contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
        } catch (_: SecurityException) {
        }
        val id = DocumentsContract.getTreeDocumentId(uri)
        Log.i(TAG, "picked folder: ${uri.authority} $id")
        val path = treePath(uri.authority, id)
        if (path == null) {
            Toast.makeText(this, getString(R.string.folder_unreadable, "${uri.authority}: $id"), Toast.LENGTH_LONG).show()
            return
        }
        // audiofilus lee la música por su ruta: sin el permiso de audio (o si
        // el sistema no deja listar esa carpeta) no encontraría nada.
        if (!canReadAudio()) {
            pendingFolder = path
            askPermissions()
            return
        }
        useFolder(path)
    }

    // pendingFolder es la carpeta elegida que espera el permiso de audio.
    private var pendingFolder: String? = null

    // useFolder pone path como la carpeta de la música, o avisa si no se
    // puede leer.
    private fun useFolder(path: String) {
        val f = File(path)
        Log.i(TAG, "music folder $path: readable ${f.canRead()}, entries ${f.list()?.size}")
        if (f.list() == null) {
            Toast.makeText(this, getString(R.string.folder_unreadable, path), Toast.LENGTH_LONG).show()
            return
        }
        Mobile.setMusicFolder(path)
        getSharedPreferences("audiofilus", MODE_PRIVATE).edit().putString("folder_for", home).apply()
        // Qué carpeta quedó y cuánto deja ver Android de ella (si no aparece
        // la música, esto dice por qué).
        Toast.makeText(this, getString(R.string.folder_picked, path, f.list()?.size ?: 0), Toast.LENGTH_LONG).show()
    }

    private fun canReadAudio(): Boolean = checkSelfPermission(
        if (Build.VERSION.SDK_INT >= 33) Manifest.permission.READ_MEDIA_AUDIO else Manifest.permission.READ_EXTERNAL_STORAGE,
    ) == PackageManager.PERMISSION_GRANTED

    // nowPlaying lo llama audiofilus (desde su hilo) cada vez que cambia lo
    // que suena: lo que muestran la notificación y la pantalla bloqueada.
    override fun nowPlaying(
        title: String?, artist: String?, album: String?, duration: Double,
        position: Double, playing: Boolean, artwork: ByteArray?,
    ) {
        Media.update(applicationContext, title ?: "", artist ?: "", album ?: "", duration, position, playing, artwork)
    }

    // askPermissions pide leer la música y mostrar la notificación. La
    // primera vez que se concede, la carpeta de música es la Music del
    // teléfono.
    private fun askPermissions() {
        val want = if (Build.VERSION.SDK_INT >= 33) {
            arrayOf(
                Manifest.permission.READ_MEDIA_AUDIO,
                Manifest.permission.READ_MEDIA_IMAGES,
                Manifest.permission.POST_NOTIFICATIONS,
            )
        } else {
            arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE)
        }
        val missing = want.filter { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }
        if (missing.isEmpty()) {
            firstFolder()
        } else {
            requestPermissions(missing.toTypedArray(), PERMISSIONS)
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != PERMISSIONS) return
        val picked = pendingFolder
        pendingFolder = null
        when {
            picked == null -> firstFolder()
            canReadAudio() -> useFolder(picked)
            else -> Toast.makeText(this, R.string.need_audio_permission, Toast.LENGTH_LONG).show()
        }
    }

    // firstFolder pone la carpeta Music del teléfono como la de la música,
    // una sola vez por carpeta de audiofilus (después manda lo que elija el
    // usuario en Ajustes; si audiofilus cambió de carpeta, otra vez).
    private fun firstFolder() {
        if (!canReadAudio()) return
        val prefs = getSharedPreferences("audiofilus", MODE_PRIVATE)
        if (prefs.getString("folder_for", null) == home) return
        @Suppress("DEPRECATION")
        val music = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_MUSIC)
        if (music.isDirectory) {
            Mobile.setMusicFolder(music.path)
            prefs.edit().putString("folder_for", home).apply()
        }
    }

    companion object {
        private const val TAG = "audiofilus"
        private const val PICK_FOLDER = 1
        private const val PERMISSIONS = 2

        // treePath convierte el documento de una carpeta elegida en su ruta:
        // los del almacenamiento del sistema ("primary:Music/Rock",
        // "1A2B-3C4D:Música", "home:..." para Documentos) y los que ya la
        // llevan ("raw:/storage/...", de
        // algunos gestores de archivos). De otros proveedores, null.
        @Suppress("DEPRECATION")
        fun treePath(authority: String?, id: String): String? {
            if (id.startsWith("raw:/")) return id.removePrefix("raw:")
            if (authority != "com.android.externalstorage.documents") return null
            val (volume, rel) = id.split(":", limit = 2).let { it[0] to it.getOrElse(1) { "" } }
            val base = when (volume) {
                "primary" -> Environment.getExternalStorageDirectory().path
                // La raíz "Documentos" del proveedor del sistema.
                "home" -> Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOCUMENTS).path
                else -> "/storage/$volume"
            }
            return if (rel.isEmpty()) base else "$base/$rel"
        }
    }

    // deviceName es el nombre con el que el teléfono se ve en la red: el que
    // le dio el usuario (Ajustes → Acerca del teléfono) o, si no, el modelo.
    private fun deviceName(): String {
        val name = if (Build.VERSION.SDK_INT >= 25) {
            Settings.Global.getString(contentResolver, Settings.Global.DEVICE_NAME)
        } else null
        return name?.takeIf { it.isNotBlank() } ?: Build.MODEL
    }
}

// GameView es la vista de Ebitengine (la genera ebitenmobile). La tecla
// Atrás que no viene de un mando (la del sistema, en Android viejos) no se la
// queda: es el gesto Atrás de la actividad.
class GameView(context: Context) : EbitenView(context) {
    private fun systemBack(keyCode: Int, e: KeyEvent) =
        keyCode == KeyEvent.KEYCODE_BACK && (e.source and InputDevice.SOURCE_GAMEPAD) != InputDevice.SOURCE_GAMEPAD

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean =
        if (systemBack(keyCode, event)) false else super.onKeyDown(keyCode, event)

    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean =
        if (systemBack(keyCode, event)) false else super.onKeyUp(keyCode, event)

    override fun onErrorOnGameUpdate(e: Exception) {
        Log.e("audiofilus", "game: $e")
    }
}
