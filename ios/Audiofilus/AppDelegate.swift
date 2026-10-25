// La app de iOS: la vista de Ebitengine con audiofilus (el framework Mobile,
// hecho con ebitenmobile desde mobile/), dentro de la zona segura, y lo que
// audiofilus no puede hacer desde Go: la sesión de audio para que la música
// siga en segundo plano, el selector de carpetas del sistema y los controles
// de la pantalla bloqueada y el Centro de control.
import AVFoundation
import MediaPlayer
import Mobile
import UIKit
import UniformTypeIdentifiers

@main
class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?
    private let root = RootViewController()
    private let bonjour = Bonjour()

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        // Reproducción: suena con el interruptor de silencio puesto, con la
        // pantalla bloqueada y con la app en segundo plano.
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .default)
        try? session.setActive(true)

        MobileSetHost(root)
        // El nombre en la red: el del aparato (desde iOS 16, sin un permiso
        // especial, solo "iPhone" o "iPad").
        MobileSetDeviceName(UIDevice.current.name)
        // Buscar y anunciar en la red con el Bonjour del sistema (Bonjour.swift).
        MobileSetBonjour(bonjour)
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        MobileStart(docs.path, NSTemporaryDirectory(), Locale.preferredLanguages.joined(separator: ","))
        root.restoreFolder()
        RemoteControls.install()

        let window = UIWindow(frame: UIScreen.main.bounds)
        window.rootViewController = root
        window.makeKeyAndVisible()
        self.window = window
        return true
    }

    // Archivos abiertos con audiofilus desde Archivos u otra app ("Abrir en",
    // "Compartir"): suenan ya. Si se abren en su sitio (no copiados a la
    // app), se pide permiso de lectura y se mantiene mientras la app corre.
    func application(_ app: UIApplication, open url: URL,
                     options: [UIApplication.OpenURLOptionsKey: Any] = [:]) -> Bool {
        guard url.isFileURL else { return false }
        if url.startAccessingSecurityScopedResource() {
            openedURLs.append(url)
        }
        MobileOpenFiles(url.path)
        return true
    }

    // openedURLs son los archivos abiertos en su sitio, con su permiso.
    private var openedURLs: [URL] = []

    // La interfaz se para mientras la app no está delante (la música no:
    // el servicio sigue en su goroutine).
    func applicationWillResignActive(_ application: UIApplication) {
        root.game.suspendGame()
    }

    func applicationDidBecomeActive(_ application: UIApplication) {
        root.game.resumeGame()
    }
}

// RootViewController pone la vista de audiofilus a pantalla completa, con la
// barra de estado del sistema encima: el fondo llega a los bordes y sube por
// detrás de la hora y la isla, y audiofilus pone su contenido dentro de la
// zona segura (se la dice sendSafeArea). Abre
// también el selector de carpetas cuando audiofilus lo pide (Ajustes →
// Carpeta).
class RootViewController: UIViewController, MobileHostProtocol, UIDocumentPickerDelegate, UITextFieldDelegate {
    let game = GameViewController()
    private static let bookmarkKey = "musicFolderBookmark"
    // keyboardField es invisible: enfocarlo abre el teclado del sistema, y lo
    // que se escribe va a audiofilus (showKeyboard).
    private let keyboardField = UITextField(frame: CGRect(x: 0, y: -100, width: 10, height: 10))
    private var closingKeyboard = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        addChild(game)
        view.addSubview(game.view)
        game.view.frame = view.bounds
        game.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        game.didMove(toParent: self)

        keyboardField.autocorrectionType = .no
        keyboardField.autocapitalizationType = .none
        keyboardField.spellCheckingType = .no
        keyboardField.returnKeyType = .done
        keyboardField.keyboardAppearance = .dark
        keyboardField.delegate = self
        keyboardField.addTarget(self, action: #selector(keyboardChanged), for: .editingChanged)
        view.addSubview(keyboardField)
    }

    // showKeyboard lo llama audiofilus (desde su hilo) cuando empieza o deja
    // de escribirse algo: la búsqueda, un nombre, una dirección o una
    // contraseña (secret).
    func showKeyboard(_ show: Bool, text: String?, secret: Bool) {
        DispatchQueue.main.async {
            if show {
                self.keyboardField.isSecureTextEntry = secret
                self.keyboardField.text = text ?? ""
                self.keyboardField.becomeFirstResponder()
            } else if self.keyboardField.isFirstResponder {
                self.closingKeyboard = true
                self.keyboardField.resignFirstResponder()
                self.closingKeyboard = false
            }
        }
    }

    @objc private func keyboardChanged() {
        MobileKeyboardText(keyboardField.text ?? "")
    }

    func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        MobileKeyboardDone()
        return false
    }

    // Si el usuario baja el teclado (no audiofilus), se deja de escribir.
    func textFieldDidEndEditing(_ textField: UITextField) {
        if !closingKeyboard {
            MobileKeyboardClosed()
        }
    }

    // La barra de estado del sistema (hora, batería), en claro sobre el
    // fondo de audiofilus, que sube hasta arriba por detrás.
    override var prefersStatusBarHidden: Bool { false }
    override var preferredStatusBarStyle: UIStatusBarStyle { .lightContent }
    override var prefersHomeIndicatorAutoHidden: Bool { true }
    override var childForStatusBarHidden: UIViewController? { nil }
    override var childForStatusBarStyle: UIViewController? { nil }

    override func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        sendSafeArea()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        sendSafeArea()
    }

    // sendSafeArea le dice a audiofilus qué tapan la isla, las esquinas y la
    // raya de inicio, para dejar ahí solo el fondo.
    private func sendSafeArea() {
        let i = view.safeAreaInsets
        MobileSetSafeArea(Double(i.top), Double(i.left), Double(i.bottom), Double(i.right))
    }

    // pickFolder lo llama audiofilus (desde su hilo): el selector de
    // carpetas de Archivos (iCloud Drive, En mi iPhone, otras apps).
    func pickFolder() {
        DispatchQueue.main.async {
            let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.folder])
            picker.delegate = self
            self.present(picker, animated: true)
        }
    }

    func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        guard let url = urls.first, url.startAccessingSecurityScopedResource() else { return }
        // El permiso para esa carpeta se guarda (un marcador): la próxima vez
        // que se abra la app sigue pudiendo leerla.
        if let bookmark = try? url.bookmarkData() {
            UserDefaults.standard.set(bookmark, forKey: Self.bookmarkKey)
        }
        MobileSetMusicFolder(url.path)
    }

    // restoreFolder vuelve a abrir la carpeta elegida otra vez (su marcador).
    func restoreFolder() {
        guard let data = UserDefaults.standard.data(forKey: Self.bookmarkKey) else { return }
        var stale = false
        guard let url = try? URL(resolvingBookmarkData: data, bookmarkDataIsStale: &stale),
              url.startAccessingSecurityScopedResource() else { return }
        if stale, let fresh = try? url.bookmarkData() {
            UserDefaults.standard.set(fresh, forKey: Self.bookmarkKey)
        }
        MobileSetMusicFolder(url.path)
    }
}

extension RootViewController {
    // nowPlaying lo llama audiofilus (desde su hilo) cada vez que cambia lo
    // que suena: lo que muestran la pantalla bloqueada y el Centro de
    // control.
    func nowPlaying(_ title: String?, artist: String?, album: String?, duration: Double,
                    position: Double, playing: Bool, artwork: Data?) {
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: title ?? "",
            MPMediaItemPropertyArtist: artist ?? "",
            MPMediaItemPropertyAlbumTitle: album ?? "",
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: playing ? 1.0 : 0.0,
        ]
        if let data = artwork, let image = UIImage(data: data) {
            info[MPMediaItemPropertyArtwork] = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
        }
        DispatchQueue.main.async {
            MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            MPNowPlayingInfoCenter.default().playbackState = playing ? .playing : .paused
        }
    }
}

// RemoteControls son los botones del sistema (pantalla bloqueada, Centro de
// control, auriculares, CarPlay): cada uno manda su orden a audiofilus.
enum RemoteControls {
    static func install() {
        UIApplication.shared.beginReceivingRemoteControlEvents()
        let c = MPRemoteCommandCenter.shared()
        c.playCommand.addTarget { _ in MobilePlay(); return .success }
        c.pauseCommand.addTarget { _ in MobilePause(); return .success }
        c.togglePlayPauseCommand.addTarget { _ in MobileToggle(); return .success }
        c.nextTrackCommand.addTarget { _ in MobileNext(); return .success }
        c.previousTrackCommand.addTarget { _ in MobilePrevious(); return .success }
        c.changePlaybackPositionCommand.addTarget { event in
            guard let e = event as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            MobileSeekTo(e.positionTime)
            return .success
        }
    }
}

// GameViewController es la vista de Ebitengine (la genera ebitenmobile).
class GameViewController: MobileEbitenViewController {
    override var prefersHomeIndicatorAutoHidden: Bool { true }
}
