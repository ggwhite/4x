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

        // 啟動 10 秒後靜默檢查更新
        DispatchQueue.main.asyncAfter(deadline: .now() + 10) { [weak self] in
            self?.silentCheckForUpdates()
        }
    }

    // MARK: - App Icon

    func setupAppIcon() {
        if let icon = resolveResource("AppIcon", ext: "icns") ?? resolveResource("AppIcon", ext: "png") {
            NSApp.applicationIconImage = icon
        }
    }

    private func resolveResource(_ name: String, ext: String) -> NSImage? {
        if let url = Bundle.main.url(forResource: name, withExtension: ext),
           let img = NSImage(contentsOf: url) { return img }

        let dir = URL(fileURLWithPath: execDir())
        var cur = dir
        for _ in 0..<6 {
            let candidate = cur.appendingPathComponent("Resources/\(name).\(ext)")
            if FileManager.default.fileExists(atPath: candidate.path),
               let img = NSImage(contentsOf: candidate) { return img }
            cur = cur.deletingLastPathComponent()
        }
        return nil
    }

    // MARK: - Menu

    func setupMenu() {
        let mainMenu = NSMenu()

        // App menu
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About 4x Live", action: #selector(showAbout), keyEquivalent: "")
        appMenu.addItem(.separator())
        let settingsItem = NSMenuItem(title: "Settings\u{2026}", action: #selector(openSettings), keyEquivalent: ",")
        settingsItem.image = NSImage(systemSymbolName: "gearshape", accessibilityDescription: nil)
        appMenu.addItem(settingsItem)
        let globalSettingsItem = NSMenuItem(title: "Global Settings\u{2026}", action: #selector(openGlobalSettingsCmd), keyEquivalent: ",")
        globalSettingsItem.keyEquivalentModifierMask = [.command, .shift]
        globalSettingsItem.image = NSImage(systemSymbolName: "gearshape.2", accessibilityDescription: nil)
        appMenu.addItem(globalSettingsItem)
        let checkUpdateItem = NSMenuItem(title: "Check for Updates\u{2026}", action: #selector(checkForUpdates), keyEquivalent: "")
        checkUpdateItem.image = NSImage(systemSymbolName: "arrow.triangle.2.circlepath", accessibilityDescription: nil)
        appMenu.addItem(checkUpdateItem)
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

    var aboutWindow: NSWindow?

    @objc func showAbout() {
        if let w = aboutWindow, w.isVisible {
            w.makeKeyAndOrderFront(nil)
            return
        }

        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String
        let ver = (version != nil && !version!.isEmpty) ? version! : (fetchVersionFromServer() ?? "dev")

        let w: CGFloat = 320, h: CGFloat = 380
        let win = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: w, height: h),
            styleMask: [.titled, .closable],
            backing: .buffered, defer: false
        )
        win.title = "About 4x Live"
        win.center()
        win.isReleasedWhenClosed = false
        win.backgroundColor = NSColor(white: 0.12, alpha: 1.0)
        win.titlebarAppearsTransparent = true
        win.appearance = NSAppearance(named: .darkAqua)

        let container = NSView(frame: NSRect(x: 0, y: 0, width: w, height: h))

        let iconView = NSImageView(frame: NSRect(x: (w - 96) / 2, y: h - 130, width: 96, height: 96))
        iconView.image = NSApp.applicationIconImage
        iconView.imageScaling = .scaleProportionallyUpOrDown
        container.addSubview(iconView)

        let nameLabel = NSTextField(labelWithString: "4x Live")
        nameLabel.font = NSFont.boldSystemFont(ofSize: 20)
        nameLabel.textColor = .white
        nameLabel.alignment = .center
        nameLabel.frame = NSRect(x: 0, y: h - 160, width: w, height: 24)
        container.addSubview(nameLabel)

        let verLabel = NSTextField(labelWithString: "Version \(ver)")
        verLabel.font = NSFont.systemFont(ofSize: 12)
        verLabel.textColor = .secondaryLabelColor
        verLabel.alignment = .center
        verLabel.frame = NSRect(x: 0, y: h - 182, width: w, height: 16)
        container.addSubview(verLabel)

        let sep = NSBox(frame: NSRect(x: 40, y: h - 200, width: w - 80, height: 1))
        sep.boxType = .separator
        container.addSubview(sep)

        let desc = NSTextField(wrappingLabelWithString: "Multi-Role AI Development Loop\n\nOrchestrate multiple AI agents through a structured protocol — design, code, review, test — with built-in guardrails and real-time dashboard.")
        desc.font = NSFont.systemFont(ofSize: 11)
        desc.textColor = NSColor(white: 0.7, alpha: 1.0)
        desc.alignment = .center
        desc.frame = NSRect(x: 24, y: h - 296, width: w - 48, height: 80)
        container.addSubview(desc)

        let link = NSTextField(labelWithString: "github.com/ggwhite/4x")
        link.font = NSFont.systemFont(ofSize: 11)
        link.textColor = NSColor(red: 0.04, green: 0.52, blue: 1.0, alpha: 1.0)
        link.alignment = .center
        link.frame = NSRect(x: 0, y: 50, width: w, height: 16)
        container.addSubview(link)

        let copyright = NSTextField(labelWithString: "© 2025 ggwhite. MIT License.")
        copyright.font = NSFont.systemFont(ofSize: 10)
        copyright.textColor = NSColor(white: 0.45, alpha: 1.0)
        copyright.alignment = .center
        copyright.frame = NSRect(x: 0, y: 30, width: w, height: 14)
        container.addSubview(copyright)

        win.contentView = container
        win.makeKeyAndOrderFront(nil)
        aboutWindow = win
    }

    private func fetchVersionFromServer() -> String? {
        let url = URL(string: "http://localhost:\(serverPort)/api/version")!
        var req = URLRequest(url: url, timeoutInterval: 2)
        req.cachePolicy = .reloadIgnoringLocalCacheData
        var result: String?
        let sem = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { data, _, _ in
            defer { sem.signal() }
            guard let data = data,
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let ver = json["version"] as? String else { return }
            result = ver
        }.resume()
        sem.wait()
        return result
    }

    // MARK: - Update Check

    /// 使用者手動觸發的更新檢查，無論結果都會顯示對話框
    @objc func checkForUpdates() {
        fetchVersionInfo { [weak self] result in
            guard let self = self else { return }
            DispatchQueue.main.async {
                switch result {
                case .success(let info):
                    if info.updateAvailable {
                        self.showUpdateAvailableAlert(version: info.version, latest: info.latest, releaseUrl: info.releaseUrl)
                    } else {
                        let alert = NSAlert()
                        alert.messageText = "You're Up to Date"
                        alert.informativeText = "4x Live v\(info.version) is the latest version."
                        alert.addButton(withTitle: "OK")
                        alert.alertStyle = .informational
                        if let icon = NSApp.applicationIconImage { alert.icon = icon }
                        alert.runModal()
                    }
                case .failure:
                    let alert = NSAlert()
                    alert.messageText = "Update Check Failed"
                    alert.informativeText = "Could not check for updates. Please try again later."
                    alert.addButton(withTitle: "OK")
                    alert.alertStyle = .warning
                    if let icon = NSApp.applicationIconImage { alert.icon = icon }
                    alert.runModal()
                }
            }
        }
    }

    /// 啟動後靜默檢查，只在有新版本時才顯示提示
    func silentCheckForUpdates() {
        fetchVersionInfo { [weak self] result in
            guard let self = self else { return }
            if case .success(let info) = result, info.updateAvailable {
                DispatchQueue.main.async {
                    self.showUpdateAvailableAlert(version: info.version, latest: info.latest, releaseUrl: info.releaseUrl)
                }
            }
        }
    }

    /// 顯示「有新版本可用」的對話框，提供下載或稍後選項
    private func showUpdateAvailableAlert(version: String, latest: String, releaseUrl: String) {
        let alert = NSAlert()
        alert.messageText = "Update Available"
        alert.informativeText = "A new version (v\(latest)) is available. You are running v\(version)."
        alert.addButton(withTitle: "Download")
        alert.addButton(withTitle: "Later")
        alert.alertStyle = .informational
        if let icon = NSApp.applicationIconImage { alert.icon = icon }
        if alert.runModal() == .alertFirstButtonReturn {
            if let url = URL(string: releaseUrl) {
                NSWorkspace.shared.open(url)
            }
        }
    }

    private struct VersionInfo {
        let version: String
        let latest: String
        let updateAvailable: Bool
        let releaseUrl: String
    }

    /// 向 server 查詢版本資訊，透過 callback 回傳結果
    private func fetchVersionInfo(completion: @escaping (Result<VersionInfo, Error>) -> Void) {
        let url = URL(string: "http://localhost:\(serverPort)/api/version?check=true")!
        let req = URLRequest(url: url, timeoutInterval: 10)
        URLSession.shared.dataTask(with: req) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            guard let data = data,
                  let httpResp = response as? HTTPURLResponse,
                  httpResp.statusCode == 200,
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let version = json["version"] as? String,
                  let latest = json["latest"] as? String,
                  let updateAvailable = json["updateAvailable"] as? Bool,
                  let releaseUrl = json["releaseUrl"] as? String
            else {
                completion(.failure(NSError(domain: "4xLive", code: -1, userInfo: [NSLocalizedDescriptionKey: "Invalid response"])))
                return
            }
            completion(.success(VersionInfo(version: version, latest: latest, updateAvailable: updateAvailable, releaseUrl: releaseUrl)))
        }.resume()
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
        let baseName: String
        switch state {
        case "running": baseName = "MenuBarIconRunningTemplate"
        case "stopped": baseName = "MenuBarIconStoppedTemplate"
        default:        baseName = "MenuBarIconTemplate"
        }

        let img: NSImage?
        if let hi = resolveResource(baseName + "@2x", ext: "png") {
            hi.size = NSSize(width: 18, height: 18)
            img = hi
        } else {
            img = resolveResource(baseName, ext: "png")
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
        DispatchQueue.main.async { [self] in
            let base = "http://localhost:\(serverPort)"
            popoverWebView.loadHTMLString(popoverHTML(base), baseURL: URL(string: base))
            guard let button = statusItem.button else { return }
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        }
    }

    func setupPopover() {
        let config = WKWebViewConfiguration()
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
        let handler = PopoverMessageHandler(delegate: self)
        config.userContentController.add(handler, name: "fourx")
        popoverWebView = WKWebView(frame: NSRect(x: 0, y: 0, width: 360, height: 520), configuration: config)
        popoverWebView.setValue(false, forKey: "drawsBackground")

        let vc = NSViewController()
        vc.view = popoverWebView

        popover = NSPopover()
        popover.contentSize = NSSize(width: 360, height: 520)
        popover.behavior = .transient
        popover.animates = true
        popover.contentViewController = vc
        popover.appearance = NSAppearance(named: .darkAqua)
    }

    func popoverHTML(_ baseURL: String) -> String {
        return """
        <!DOCTYPE html>
        <html><head><meta charset="utf-8">
        <style>
        * { margin:0; padding:0; box-sizing:border-box; }
        body { font-family:-apple-system,BlinkMacSystemFont,'SF Pro Text',sans-serif;
               background:transparent; color:#f5f5f7; padding:16px; -webkit-font-smoothing:antialiased;
               overflow-y:auto; overflow-x:hidden; }

        .header { display:flex; justify-content:space-between; align-items:center; margin-bottom:16px; }
        .header-left { display:flex; align-items:center; gap:8px; }
        .header-right { display:flex; align-items:center; gap:6px; }
        .status-dot { width:8px; height:8px; border-radius:50%; background:#30d158; flex-shrink:0; }
        .status-dot.offline { background:#ff453a; }
        .title { font-size:16px; font-weight:700; }
        .hdr-btn { background:rgba(255,255,255,0.08); border:1px solid rgba(255,255,255,0.12);
                   color:#c0c0c5; cursor:pointer; font-size:11px; font-weight:600;
                   height:28px; padding:0 10px; border-radius:6px; font-family:inherit; transition:all .15s;
                   display:flex; align-items:center; justify-content:center; gap:4px; }
        .hdr-btn:hover { background:rgba(10,132,255,0.2); color:#0a84ff; border-color:rgba(10,132,255,0.3); }
        .hdr-btn.icon-btn { width:28px; padding:0; font-size:16px; }

        .stats { display:grid; grid-template-columns:1fr 1fr 1fr 1fr; gap:6px; margin-bottom:16px; }
        .stat { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.08);
                border-radius:10px; padding:10px 8px; text-align:center; }
        .stat-value { font-size:20px; font-weight:700; line-height:1.2; }
        .stat-label { font-size:9px; color:#8e8e93; margin-top:2px; letter-spacing:0.3px; }
        .c-green { color:#30d158; } .c-orange { color:#ff9f0a; }
        .c-red { color:#ff453a; } .c-blue { color:#0a84ff; }

        .project { margin-bottom:12px; }
        .proj-header { display:flex; align-items:center; justify-content:space-between;
                       padding:6px 0; cursor:default; }
        .proj-name { font-size:13px; font-weight:600; color:#e5e5ea; }
        .proj-badge { font-size:10px; color:#8e8e93; background:rgba(255,255,255,0.06);
                      padding:2px 8px; border-radius:8px; }
        .proj-tasks { padding-left:4px; }

        .task-item { display:flex; align-items:center; gap:8px; padding:5px 0;
                     border-bottom:1px solid rgba(255,255,255,0.04); font-size:12px; }
        .task-item:last-child { border-bottom:none; }
        .task-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
        .task-dot.in-progress { background:#30d158; }
        .task-dot.active { background:#30d158; animation:pulse 2s infinite; }
        .task-dot.ready-for-review { background:#0a84ff; }
        .task-dot.needs-attention { background:#ff453a; }
        .task-dot.not-started { background:#636366; }
        .task-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:#c0c0c5; }
        .task-status { font-size:10px; color:#8e8e93; flex-shrink:0; }

        .section { font-size:10px; color:#8e8e93; margin:14px 0 6px; font-weight:600;
                   text-transform:uppercase; letter-spacing:0.5px; }
        .empty { text-align:center; color:#636366; font-size:12px; padding:24px 0; }
        .loading { text-align:center; color:#636366; font-size:12px; padding:40px 0; }


        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.4} }
        </style></head><body>
        <div class="header">
          <div class="header-left">
            <div class="status-dot" id="statusDot"></div>
            <div class="title">4x Live</div>
          </div>
          <div class="header-right">
            <button class="hdr-btn icon-btn" onclick="window.webkit.messageHandlers.fourx.postMessage('settings')" title="Settings"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg></button>
            <button class="hdr-btn icon-btn" onclick="window.webkit.messageHandlers.fourx.postMessage('open')" title="Open Dashboard"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg></button>
          </div>
        </div>
        <div class="stats" id="stats">
          <div class="stat"><div class="stat-value c-green" id="s-active">-</div><div class="stat-label">Active</div></div>
          <div class="stat"><div class="stat-value c-orange" id="s-review">-</div><div class="stat-label">Review</div></div>
          <div class="stat"><div class="stat-value c-red" id="s-attention">-</div><div class="stat-label">Attention</div></div>
          <div class="stat"><div class="stat-value c-blue" id="s-done">-</div><div class="stat-label">Done</div></div>
        </div>
        <div id="content"><div class="loading">Loading…</div></div>
        <script>
        const BASE = '\(baseURL)';
        const STATUS_ORDER = { 'in-progress':0, 'needs-attention':1, 'ready-for-review':2, 'not-started':3 };
        const STATUS_LABEL = { 'in-progress':'進行中', 'needs-attention':'需關注', 'ready-for-review':'待審查', 'not-started':'未開始' };

        async function load() {
          try {
            const projRes = await fetch(BASE + '/api/projects');
            if (!projRes.ok) throw new Error('offline');
            const projects = await projRes.json();

            let totalActive=0, totalReview=0, totalAttention=0, totalDone=0;
            const projectData = [];
            const seenNames = new Set();

            await Promise.all(projects.map(async (proj) => {
              const key = proj.name + ':' + proj.taskCount;
              if (seenNames.has(key)) return;
              seenNames.add(key);
              try {
                const res = await fetch(BASE + '/api/project/' + proj.id + '/api/tasks');
                if (!res.ok) return;
                const tasks = await res.json();
                let active=0, review=0, attention=0, done=0, notStarted=0;
                const highlights = [];
                tasks.forEach(t => {
                  const s = t.status || '';
                  if (t.active || s === 'in-progress') { active++; highlights.push(t); }
                  else if (s === 'ready-for-review') { review++; highlights.push(t); }
                  else if (s === 'needs-attention') { attention++; highlights.push(t); }
                  else if (s === 'done' || s === 'abandoned') { done++; }
                  else if (s === 'not-started') { notStarted++; if (highlights.length < 5) highlights.push(t); }
                });
                totalActive += active; totalReview += review;
                totalAttention += attention; totalDone += done;
                highlights.sort((a,b) => (STATUS_ORDER[a.status]??9) - (STATUS_ORDER[b.status]??9));
                if (tasks.length > 0) {
                  projectData.push({ name: proj.name, id: proj.id, total: tasks.length,
                    active, review, attention, done, notStarted, highlights: highlights.slice(0, 5) });
                }
              } catch(e) {}
            }));

            document.getElementById('s-active').textContent = totalActive;
            document.getElementById('s-review').textContent = totalReview;
            document.getElementById('s-attention').textContent = totalAttention;
            document.getElementById('s-done').textContent = totalDone;

            const el = document.getElementById('content');
            if (projectData.length === 0) {
              el.innerHTML = '<div class="empty">No projects</div>';
              return;
            }
            projectData.sort((a,b) => (b.active+b.attention+b.review) - (a.active+a.attention+a.review));

            let html = '<div class="section">Projects</div>';
            projectData.forEach(p => {
              html += '<div class="project">';
              html += '<div class="proj-header"><span class="proj-name">' + esc(p.name) + '</span>';
              html += '<span class="proj-badge">' + p.total + ' tasks</span></div>';
              if (p.highlights.length > 0) {
                html += '<div class="proj-tasks">';
                p.highlights.forEach(t => {
                  const st = t.active ? 'active' : (t.status || 'not-started');
                  const label = STATUS_LABEL[t.status] || t.status || '';
                  html += '<div class="task-item">';
                  html += '<div class="task-dot ' + st + '"></div>';
                  html += '<div class="task-name">' + esc(t.name || t.id) + '</div>';
                  html += '<div class="task-status">' + label + '</div>';
                  html += '</div>';
                });
                html += '</div>';
              }
              html += '</div>';
            });
            el.innerHTML = html;
          } catch(e) {
            document.getElementById('statusDot').classList.add('offline');
            document.getElementById('content').innerHTML = '<div class="empty">Server not available</div>';
          }
        }

        function esc(s) { const d=document.createElement('div'); d.textContent=s; return d.innerHTML; }
        load().then(() => {
          const h = Math.min(520, document.body.scrollHeight + 20);
          window.webkit.messageHandlers.fourx.postMessage('resize:' + h);
        });
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

        serverPort = findAvailablePort(from: serverPort)

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

    private func findAvailablePort(from preferred: Int) -> Int {
        for port in preferred..<(preferred + 100) {
            let sock = socket(AF_INET, SOCK_STREAM, 0)
            guard sock >= 0 else { continue }
            defer { close(sock) }

            var addr = sockaddr_in()
            addr.sin_family = sa_family_t(AF_INET)
            addr.sin_port = in_port_t(port).bigEndian
            addr.sin_addr.s_addr = inet_addr("127.0.0.1")

            let result = withUnsafePointer(to: &addr) { ptr in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockPtr in
                    Darwin.bind(sock, sockPtr, socklen_t(MemoryLayout<sockaddr_in>.size))
                }
            }
            if result == 0 {
                return port
            }
        }
        return preferred
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
        guard let body = msg.body as? String, let d = delegate else { return }
        if body.hasPrefix("resize:"), let h = Double(body.replacingOccurrences(of: "resize:", with: "")) {
            d.popover.contentSize = NSSize(width: 360, height: min(520, max(200, h)))
            return
        }
        d.popover.close()
        d.window.makeKeyAndOrderFront(nil)
        d.startTitleSync()
        NSApp.activate(ignoringOtherApps: true)
        if body == "settings" {
            d.openSettings()
        }
    }
}

let app = NSApplication.shared

let bundleID = Bundle.main.bundleIdentifier ?? "com.ggwhite.4x.live"
let running = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
if running.count > 1 {
    running.first { $0 != NSRunningApplication.current }?.activate()
    exit(0)
}

let delegate = AppDelegate()
app.delegate = delegate
app.run()
