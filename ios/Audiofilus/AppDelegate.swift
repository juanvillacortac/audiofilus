// La app de iOS: la vista de Ebitengine con audiofilus (el framework Mobile,
// hecho con ebitenmobile desde mobile/), dentro de la zona segura, y lo que
// audiofilus no puede hacer desde Go: la sesión de audio para que la música
// siga en segundo plano y el selector de carpetas del sistema.
import AVFoundation
import Mobile
import UIKit
import UniformTypeIdentifiers

@main
class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?
    private let root = RootViewController()

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        // Reproducción: suena con el interruptor de silencio puesto, con la
        // pantalla bloqueada y con la app en segundo plano.
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .default)
        try? session.setActive(true)

        MobileSetHost(root)
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        MobileStart(docs.path, NSTemporaryDirectory(), Locale.preferredLanguages.joined(separator: ","))
        root.restoreFolder()

        let window = UIWindow(frame: UIScreen.main.bounds)
        window.rootViewController = root
        window.makeKeyAndVisible()
        self.window = window
        return true
    }

    // La interfaz se para mientras la app no está delante (la música no:
    // el servicio sigue en su goroutine).
    func applicationWillResignActive(_ application: UIApplication) {
        root.game.suspendGame()
    }

    func applicationDidBecomeActive(_ application: UIApplication) {
        root.game.resumeGame()
    }
}

// RootViewController pone la vista de audiofilus dentro de la zona segura
// (sin la muesca ni la raya de inicio encima) y abre el selector de carpetas
// cuando audiofilus lo pide (Ajustes → Carpeta).
class RootViewController: UIViewController, MobileHostProtocol, UIDocumentPickerDelegate {
    let game = GameViewController()
    private static let bookmarkKey = "musicFolderBookmark"

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        addChild(game)
        view.addSubview(game.view)
        game.view.translatesAutoresizingMaskIntoConstraints = false
        let safe = view.safeAreaLayoutGuide
        NSLayoutConstraint.activate([
            game.view.topAnchor.constraint(equalTo: safe.topAnchor),
            game.view.bottomAnchor.constraint(equalTo: safe.bottomAnchor),
            game.view.leadingAnchor.constraint(equalTo: safe.leadingAnchor),
            game.view.trailingAnchor.constraint(equalTo: safe.trailingAnchor),
        ])
        game.didMove(toParent: self)
    }

    override var preferredStatusBarStyle: UIStatusBarStyle { .lightContent }

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

// GameViewController es la vista de Ebitengine (la genera ebitenmobile).
class GameViewController: MobileEbitenViewController {
    override var prefersHomeIndicatorAutoHidden: Bool { true }
}
