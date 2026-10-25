// Bonjour es el Bonjour del sistema para audiofilus (mobile.SetBonjour): iOS
// no deja a la app mandar ni recibir multicast por su cuenta (pediría el
// permiso com.apple.developer.networking.multicast de Apple), así que el
// mDNS de Go no oye nada. Con Bonjour, "Enviar a" encuentra Chromecasts y
// servidores MPD, y otros aparatos encuentran el servidor MPD del teléfono.
// NetService va en el run loop del hilo principal.
import Foundation
import Mobile

final class Bonjour: NSObject, MobileBonjourProtocol {
    // Los anuncios en marcha, por su id.
    private var published: [Int: NetService] = [:]

    // browse lo llama audiofilus desde una goroutine: busca durante ms
    // milisegundos y devuelve una instancia por línea (nombre, IPv4,
    // puerto y el TXT clave=valor, separados por tabuladores).
    func browse(_ service: String?, ms: Int) -> String {
        guard let service = service, !Thread.isMainThread else { return "" }
        let search = BonjourSearch(type: service + ".")
        DispatchQueue.main.async { search.start() }
        Thread.sleep(forTimeInterval: Double(ms) / 1000)
        var out = ""
        DispatchQueue.main.sync { out = search.finish() }
        return out
    }

    func publish(_ id_: Int, name: String?, service: String?, port: Int, txt: String?) {
        var record: [String: Data] = [:]
        for kv in (txt ?? "").split(separator: "\t") {
            let parts = kv.split(separator: "=", maxSplits: 1)
            record[String(parts[0])] = Data((parts.count > 1 ? String(parts[1]) : "").utf8)
        }
        DispatchQueue.main.async {
            let s = NetService(domain: "local.", type: (service ?? "") + ".", name: name ?? "", port: Int32(port))
            s.setTXTRecord(NetService.data(fromTXTRecord: record))
            s.publish()
            self.published[id_] = s
        }
    }

    func unpublish(_ id_: Int) {
        DispatchQueue.main.async {
            self.published.removeValue(forKey: id_)?.stop()
        }
    }
}

// BonjourSearch es una búsqueda: encuentra las instancias de un tipo y
// resuelve su dirección, su puerto y su TXT.
private final class BonjourSearch: NSObject, NetServiceBrowserDelegate, NetServiceDelegate {
    private let type: String
    private let browser = NetServiceBrowser()
    private var services: [NetService] = []
    private var found: [String: String] = [:] // por nombre

    init(type: String) {
        self.type = type
    }

    func start() {
        browser.delegate = self
        browser.searchForServices(ofType: type, inDomain: "local.")
    }

    // finish para la búsqueda y devuelve lo encontrado.
    func finish() -> String {
        browser.stop()
        services.forEach { $0.stop() }
        return found.values.joined(separator: "\n")
    }

    func netServiceBrowser(_ browser: NetServiceBrowser, didFind service: NetService, moreComing: Bool) {
        services.append(service)
        service.delegate = self
        service.resolve(withTimeout: 3)
    }

    func netServiceDidResolveAddress(_ sender: NetService) {
        guard let ip = sender.addresses?.compactMap(ipv4).first else { return }
        var fields = [sender.name, ip, String(sender.port)]
        if let data = sender.txtRecordData() {
            for (k, v) in NetService.dictionary(fromTXTRecord: data) {
                fields.append(k + "=" + (String(data: v, encoding: .utf8) ?? ""))
            }
        }
        found[sender.name] = fields.map { field in
            String(field.map { $0 == "\t" || $0 == "\n" ? " " : $0 })
        }.joined(separator: "\t")
    }
}

// ipv4 es la dirección IPv4 de un sockaddr (nil si es otra cosa).
private func ipv4(_ data: Data) -> String? {
    guard data.count >= MemoryLayout<sockaddr_in>.size else { return nil }
    var sa = sockaddr_in()
    _ = withUnsafeMutableBytes(of: &sa) { data.copyBytes(to: $0) }
    guard sa.sin_family == sa_family_t(AF_INET) else { return nil }
    var buf = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
    guard inet_ntop(AF_INET, &sa.sin_addr, &buf, socklen_t(INET_ADDRSTRLEN)) != nil else { return nil }
    return String(cString: buf)
}
