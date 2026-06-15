import AppKit
import WebKit

class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    var window: NSWindow!
    var webView: WKWebView!
    var serverPort: Int = 4567
    var embeddedServer: Process?

    func applicationDidFinishLaunching(_ notification: Notification) {
        parseArgs()
        launchEmbeddedServer()

        let config = WKWebViewConfiguration()
        let userContent = config.userContentController
        userContent.add(self, name: "nativeOpenFolder")
        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1200, height: 800),
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered,
            defer: false
        )
        window.title = "4x Live"
        window.contentView = webView
        window.center()
        window.setFrameAutosaveName("4xLiveWindow")
        window.makeKeyAndOrderFront(nil)

        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)

        pollServerAndLoad()
    }

    // launchEmbeddedServer 啟動與 Swift 執行檔同層的 bundled `4x` binary（Contents/MacOS/4x），
    // 以 `live --port=<serverPort>` 提供 dashboard server。
    // 若 bundle 內找不到 `4x`，則不啟動子程序，交由 pollServerAndLoad 連既有外部 server（向後相容）。
    func launchEmbeddedServer() {
        let execDir = URL(fileURLWithPath: CommandLine.arguments[0])
            .deletingLastPathComponent()
        let binary = execDir.appendingPathComponent("4x")
        guard FileManager.default.isExecutableFile(atPath: binary.path) else {
            NSLog("4x binary not found at \(binary.path); falling back to external server")
            return
        }

        let proc = Process()
        proc.executableURL = binary
        proc.arguments = ["live", "--port=\(serverPort)"]
        do {
            try proc.run()
            embeddedServer = proc
        } catch {
            NSLog("failed to launch embedded 4x server: \(error)")
        }
    }

    func parseArgs() {
        let args = CommandLine.arguments
        for (i, arg) in args.enumerated() {
            if arg.starts(with: "--port="), let p = Int(arg.replacingOccurrences(of: "--port=", with: "")) {
                serverPort = p
            } else if arg == "--port", i + 1 < args.count, let p = Int(args[i + 1]) {
                serverPort = p
            }
        }
    }

    func pollServerAndLoad() {
        let url = URL(string: "http://localhost:\(serverPort)/api/projects")!
        let task = URLSession.shared.dataTask(with: url) { [weak self] data, response, error in
            guard let self = self else { return }
            if let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 {
                DispatchQueue.main.async {
                    let pageURL = URL(string: "http://localhost:\(self.serverPort)")!
                    self.webView.load(URLRequest(url: pageURL))
                }
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    self.pollServerAndLoad()
                }
            }
        }
        task.resume()
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.name == "nativeOpenFolder" {
            let panel = NSOpenPanel()
            panel.canChooseDirectories = true
            panel.canChooseFiles = false
            panel.allowsMultipleSelection = false
            panel.message = "Select a 4x project folder"

            if panel.runModal() == .OK, let url = panel.url {
                let path = url.path
                let js = "addProjectFromNative('\(path.replacingOccurrences(of: "'", with: "\\'"))')"
                webView.evaluateJavaScript(js, completionHandler: nil)
            }
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        injectNativeBridge()
        startTitleSync()
    }

    func injectNativeBridge() {
        let js = "window._isNativeApp = true;"
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func startTitleSync() {
        Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            self?.webView.evaluateJavaScript("activeProjectId ? (openTabs.find(t=>t.id===activeProjectId)||{}).name || '4x Live' : '4x Live'") { result, _ in
                if let name = result as? String {
                    DispatchQueue.main.async {
                        self?.window.title = name == "4x Live" ? name : "\(name) — 4x Live"
                    }
                }
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    // applicationWillTerminate 在 app 結束前 terminate 內嵌的 4x server 子程序，避免殘留孤兒程序。
    func applicationWillTerminate(_ notification: Notification) {
        if let proc = embeddedServer, proc.isRunning {
            proc.terminate()
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
