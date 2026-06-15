import AppKit
import WebKit

class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    var window: NSWindow!
    var webView: WKWebView!
    var statusItem: NSStatusItem!
    var popover: NSPopover!
    var popoverWebView: WKWebView!
    var serverPort: Int = 4567
    var embeddedServer: Process?
    var statusTimer: Timer?
    var titleTimer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        parseArgs()
        launchEmbeddedServer()

        NSApp.setActivationPolicy(.regular)
        NSApp.appearance = NSAppearance(named: .darkAqua)

        setupAppIcon()
        setupMenu()
        createWindow()
        setupStatusItem()
        setupPopover()
        startStatusTimer()

        pollServerAndLoad()
    }

    // MARK: - App Icon

    func setupAppIcon() {
        let dir = execDir()
        let candidates = [
            Bundle.main.url(forResource: "AppIcon", withExtension: "icns")?.path,
            "\(dir)/../Resources/AppIcon.icns",
            "\(dir)/AppIcon.icns",
        ].compactMap { $0 }
        for path in candidates {
            if let icon = NSImage(contentsOfFile: path) {
                NSApp.applicationIconImage = icon
                break
            }
        }
    }

    // MARK: - Menu

    func setupMenu() {
        let mainMenu = NSMenu()

        // App menu
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About 4x Live", action: nil, keyEquivalent: "")
        appMenu.addItem(.separator())
        let settingsItem = NSMenuItem(title: "Settings\u{2026}", action: #selector(openSettings), keyEquivalent: ",")
        settingsItem.image = NSImage(systemSymbolName: "gearshape", accessibilityDescription: nil)
        appMenu.addItem(settingsItem)
        let globalSettingsItem = NSMenuItem(title: "Global Settings\u{2026}", action: #selector(openGlobalSettingsCmd), keyEquivalent: ",")
        globalSettingsItem.keyEquivalentModifierMask = [.command, .shift]
        globalSettingsItem.image = NSImage(systemSymbolName: "gearshape.2", accessibilityDescription: nil)
        appMenu.addItem(globalSettingsItem)
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit 4x Live", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        let appMenuItem = NSMenuItem()
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        // View menu
        let viewMenu = NSMenu(title: "View")
        let reloadItem = NSMenuItem(title: "Reload", action: #selector(reloadPage), keyEquivalent: "r")
        reloadItem.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: nil)
        viewMenu.addItem(reloadItem)
        viewMenu.addItem(.separator())
        let searchItem = NSMenuItem(title: "Search", action: #selector(openSearchCmd), keyEquivalent: "k")
        searchItem.image = NSImage(systemSymbolName: "magnifyingglass", accessibilityDescription: nil)
        viewMenu.addItem(searchItem)
        let shortcutsItem = NSMenuItem(title: "Keyboard Shortcuts", action: #selector(showShortcuts), keyEquivalent: "/")
        shortcutsItem.image = NSImage(systemSymbolName: "keyboard", accessibilityDescription: nil)
        viewMenu.addItem(shortcutsItem)
        viewMenu.addItem(.separator())
        let fullScreen = NSMenuItem(title: "Enter Full Screen", action: #selector(NSWindow.toggleFullScreen(_:)), keyEquivalent: "f")
        fullScreen.keyEquivalentModifierMask = [.command, .control]
        fullScreen.image = NSImage(systemSymbolName: "arrow.up.left.and.arrow.down.right", accessibilityDescription: nil)
        viewMenu.addItem(fullScreen)
        let viewMenuItem = NSMenuItem()
        viewMenuItem.submenu = viewMenu
        mainMenu.addItem(viewMenuItem)

        // Window menu
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.miniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.zoom(_:)), keyEquivalent: "")
        windowMenu.addItem(.separator())
        windowMenu.addItem(withTitle: "Close Window", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        let windowMenuItem = NSMenuItem()
        windowMenuItem.submenu = windowMenu
        mainMenu.addItem(windowMenuItem)
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = mainMenu
    }

    @objc func reloadPage() {
        webView.reload()
    }

    @objc func openSettings() {
        webView.evaluateJavaScript("activeProjectId?openProjectSettings():openGlobalSettings()", completionHandler: nil)
    }

    @objc func openGlobalSettingsCmd() {
        webView.evaluateJavaScript("openGlobalSettings()", completionHandler: nil)
    }

    @objc func openSearchCmd() {
        webView.evaluateJavaScript("openSearch()", completionHandler: nil)
    }

    @objc func showShortcuts() {
        webView.evaluateJavaScript("showShortcutsHelp('shortcuts')", completionHandler: nil)
    }

    // MARK: - Window (Frost style)

    func createWindow() {
        let screenFrame = NSScreen.main?.frame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let w: CGFloat = min(1400, screenFrame.width * 0.85)
        let h: CGFloat = min(920, screenFrame.height * 0.85)

        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "nativeOpenFolder")
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")

        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self
        webView.setValue(false, forKey: "drawsBackground")
        webView.translatesAutoresizingMaskIntoConstraints = false

        let vibrancy = NSVisualEffectView()
        vibrancy.material = .hudWindow
        vibrancy.blendingMode = .behindWindow
        vibrancy.state = .active
        vibrancy.translatesAutoresizingMaskIntoConstraints = false

        let container = NSView()
        container.addSubview(vibrancy)
        container.addSubview(webView)

        window = NSWindow(
            contentRect: NSRect(
                x: (screenFrame.width - w) / 2,
                y: (screenFrame.height - h) / 2,
                width: w, height: h
            ),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false
        )
        window.title = "4x Live"
        window.titlebarAppearsTransparent = true
        window.backgroundColor = NSColor(white: 0.08, alpha: 1.0)
        window.delegate = self
        window.minSize = NSSize(width: 900, height: 600)
        window.contentView = container
        window.setFrameAutosaveName("4xLiveWindow")
        window.miniwindowImage = NSApp.applicationIconImage
        window.makeKeyAndOrderFront(nil)

        NSLayoutConstraint.activate([
            vibrancy.topAnchor.constraint(equalTo: container.topAnchor),
            vibrancy.bottomAnchor.constraint(equalTo: container.bottomAnchor),
            vibrancy.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            vibrancy.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            webView.topAnchor.constraint(equalTo: container.topAnchor),
            webView.bottomAnchor.constraint(equalTo: container.bottomAnchor),
            webView.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: container.trailingAnchor),
        ])

        NSApp.activate(ignoringOtherApps: true)
    }

    // MARK: - Menu Bar Status Item

    func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        guard let button = statusItem.button else { return }
        button.image = loadMenuBarIcon(state: "idle")
        button.toolTip = "4x Live"
        button.action = #selector(statusItemClicked)
        button.target = self
    }

    func loadMenuBarIcon(state: String) -> NSImage? {
        let dir = execDir()
        let resDir = "\(dir)/../Resources"
        let baseName: String
        switch state {
        case "running": baseName = "MenuBarIconRunningTemplate"
        case "stopped": baseName = "MenuBarIconStoppedTemplate"
        default:        baseName = "MenuBarIconTemplate"
        }

        let img: NSImage?
        if let hi = NSImage(contentsOfFile: "\(resDir)/\(baseName)@2x.png") {
            hi.size = NSSize(width: 18, height: 18)
            img = hi
        } else {
            img = NSImage(contentsOfFile: "\(resDir)/\(baseName).png")
                ?? NSImage(contentsOfFile: "\(dir)/\(baseName).png")
        }
        img?.isTemplate = true
        return img
    }

    @objc func statusItemClicked() {
        if popover.isShown {
            popover.close()
            return
        }
        fetchSummaryAndShowPopover()
    }

    func fetchSummaryAndShowPopover() {
        let url = URL(string: "http://localhost:\(serverPort)/api/projects")!
        URLSession.shared.dataTask(with: URLRequest(url: url, timeoutInterval: 3)) { [weak self] data, response, error in
            guard let self = self else { return }
            var jsonStr = "null"
            if let data = data, let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 {
                jsonStr = String(data: data, encoding: .utf8) ?? "null"
            }
            DispatchQueue.main.async {
                self.popoverWebView.loadHTMLString(self.popoverHTML(jsonStr), baseURL: nil)
                guard let button = self.statusItem.button else { return }
                self.popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            }
        }.resume()
    }

    func setupPopover() {
        let config = WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        let handler = PopoverMessageHandler(delegate: self)
        config.userContentController.add(handler, name: "fourx")
        popoverWebView = WKWebView(frame: NSRect(x: 0, y: 0, width: 320, height: 440), configuration: config)
        popoverWebView.setValue(false, forKey: "drawsBackground")

        let vc = NSViewController()
        vc.view = popoverWebView

        popover = NSPopover()
        popover.contentSize = NSSize(width: 320, height: 440)
        popover.behavior = .transient
        popover.animates = true
        popover.contentViewController = vc
        popover.appearance = NSAppearance(named: .darkAqua)
    }

    func popoverHTML(_ json: String) -> String {
        return """
        <!DOCTYPE html>
        <html><head><meta charset="utf-8">
        <style>
        * { margin:0; padding:0; box-sizing:border-box; }
        body { font-family:-apple-system,BlinkMacSystemFont,'SF Pro Text',sans-serif;
               background:transparent; color:#f5f5f7; padding:16px; -webkit-font-smoothing:antialiased; }
        .header { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px; }
        .title { font-size:15px; font-weight:700; }
        .open-btn { background:rgba(255,255,255,0.08); border:1px solid rgba(255,255,255,0.12); color:#c0c0c5; cursor:pointer; font-size:11px; font-weight:600; padding:4px 10px; border-radius:6px; font-family:inherit; }
        .open-btn:hover { background:rgba(10,132,255,0.2); color:#0a84ff; border-color:rgba(10,132,255,0.3); }
        .stats { display:grid; grid-template-columns:1fr 1fr; gap:8px; margin-bottom:16px; }
        .stat { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.1); border-radius:10px; padding:10px 12px; }
        .stat-label { font-size:10px; color:#8e8e93; margin-bottom:2px; }
        .stat-value { font-size:22px; font-weight:700; }
        .stat-value.running { color:#30d158; }
        .stat-value.pending { color:#ff9f0a; }
        .stat-value.done { color:#0a84ff; }
        .stat-value.total { color:#f5f5f7; }
        .section { font-size:11px; color:#8e8e93; margin:12px 0 6px; font-weight:600; text-transform:uppercase; letter-spacing:0.5px; }
        .feature { display:flex; align-items:center; gap:8px; padding:6px 0; border-bottom:1px solid rgba(255,255,255,0.06); font-size:12px; }
        .feature:last-child { border-bottom:none; }
        .dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
        .dot.running { background:#30d158; }
        .dot.pending { background:#ff9f0a; }
        .dot.todo { background:#8e8e93; }
        .fname { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:#c0c0c5; }
        .frole { font-size:10px; color:#8e8e93; }
        .empty { text-align:center; color:#636366; font-size:12px; padding:20px 0; }
        </style></head><body>
        <div class="header">
          <div class="title">4x Live</div>
          <button class="open-btn" onclick="window.webkit.messageHandlers.fourx.postMessage('open')">Open ↗</button>
        </div>
        <div id="content"></div>
        <script>
        const data = \(json);
        const el = document.getElementById('content');
        if (!data) { el.innerHTML = '<div class="empty">Server not available</div>'; }
        else {
          let running=0, pending=0, todo=0, done=0, total=0;
          const activeFeatures = [];
          (Array.isArray(data) ? data : [data]).forEach(proj => {
            const features = proj.features || [];
            features.forEach(f => {
              total++;
              const st = f.status || f.state?.phase;
              const active = f.state?.active;
              if (active) { running++; activeFeatures.push({name:f.name||f.id, role:f.state?.role||'', id:f.id}); }
              else if (st==='ready-for-review'||st==='pending-review') pending++;
              else if (st==='done'||st==='abandoned') done++;
              else todo++;
            });
          });
          let html = '<div class="stats">';
          html += '<div class="stat"><div class="stat-label">● 執行中</div><div class="stat-value running">'+running+'</div></div>';
          html += '<div class="stat"><div class="stat-label">● 待處理</div><div class="stat-value pending">'+pending+'</div></div>';
          html += '<div class="stat"><div class="stat-label">● 已完成</div><div class="stat-value done">'+done+'</div></div>';
          html += '<div class="stat"><div class="stat-label">● 全部</div><div class="stat-value total">'+total+'</div></div>';
          html += '</div>';
          if (activeFeatures.length > 0) {
            html += '<div class="section">執行中的任務</div>';
            activeFeatures.forEach(f => {
              html += '<div class="feature"><div class="dot running"></div><div class="fname">'+f.name+'</div><div class="frole">'+f.role+'</div></div>';
            });
          } else {
            html += '<div class="empty">目前沒有執行中的任務</div>';
          }
          el.innerHTML = html;
        }
        </script></body></html>
        """
    }

    func startStatusTimer() {
        updateStatusIcon()
        statusTimer = Timer.scheduledTimer(withTimeInterval: 10.0, repeats: true) { [weak self] _ in
            self?.updateStatusIcon()
        }
    }

    func updateStatusIcon() {
        guard let button = statusItem?.button else { return }

        let url = URL(string: "http://localhost:\(serverPort)/api/projects")!
        let req = URLRequest(url: url, timeoutInterval: 3)
        URLSession.shared.dataTask(with: req) { [weak self] data, response, error in
            guard let self = self else { return }
            let state: String
            if let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 {
                var hasRunning = false
                if let data = data,
                   let json = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] {
                    for proj in json {
                        if let features = proj["features"] as? [[String: Any]] {
                            for f in features {
                                if let s = f["state"] as? [String: Any],
                                   let active = s["active"] as? Bool, active {
                                    hasRunning = true
                                    break
                                }
                            }
                        }
                        if hasRunning { break }
                    }
                }
                state = hasRunning ? "running" : "idle"
            } else {
                state = "stopped"
            }
            DispatchQueue.main.async {
                button.image = self.loadMenuBarIcon(state: state)
            }
        }.resume()
    }

    // MARK: - Embedded Server

    func launchEmbeddedServer() {
        let dir = execDir()
        let binary = URL(fileURLWithPath: dir).appendingPathComponent("4x")
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

    // MARK: - Server Polling

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

    // MARK: - WebView Delegates

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
        webView.evaluateJavaScript("window._isNativeApp = true;", completionHandler: nil)
    }

    func startTitleSync() {
        titleTimer?.invalidate()
        titleTimer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            guard let self = self, self.window != nil, self.window.isVisible else { return }
            self.webView.evaluateJavaScript(
                "activeProjectId ? (openTabs.find(t=>t.id===activeProjectId)||{}).name || '4x Live' : '4x Live'"
            ) { [weak self] result, _ in
                guard let self = self, self.window != nil, self.window.isVisible else { return }
                if let name = result as? String {
                    self.window.title = name == "4x Live" ? name : "\(name) — 4x Live"
                }
            }
        }
    }

    // MARK: - Lifecycle

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        sender.orderOut(nil)
        titleTimer?.invalidate()
        titleTimer = nil
        return false
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag {
            window.makeKeyAndOrderFront(nil)
            startTitleSync()
        }
        return true
    }

    func applicationWillTerminate(_ notification: Notification) {
        titleTimer?.invalidate()
        statusTimer?.invalidate()
        if let proc = embeddedServer, proc.isRunning {
            proc.terminate()
        }
    }

    // MARK: - Helpers

    func execDir() -> String {
        URL(fileURLWithPath: CommandLine.arguments[0])
            .deletingLastPathComponent().path
    }
}

class PopoverMessageHandler: NSObject, WKScriptMessageHandler {
    weak var delegate: AppDelegate?
    init(delegate: AppDelegate) { self.delegate = delegate; super.init() }
    func userContentController(_ uc: WKUserContentController, didReceive msg: WKScriptMessage) {
        if let body = msg.body as? String, body == "open", let d = delegate {
            d.popover.close()
            d.window.makeKeyAndOrderFront(nil)
            d.startTitleSync()
            NSApp.activate(ignoringOtherApps: true)
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
