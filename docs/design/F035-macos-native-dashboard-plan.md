# F035: macOS Native Dashboard — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enhance the existing WKWebView wrapper into a full native macOS app shell with server management, menu bar status item, popover, notifications, and Dock badge.

**Architecture:** The native app spawns `4x live` as a child process, loads the web UI via WKWebView, and uses HTTP/SSE to monitor feature state for notifications and Dock badge. Menu bar status item provides a quick-glance popover. All data comes from the Go server API — no direct filesystem access.

**Tech Stack:** Swift 5.9+, AppKit, WebKit, UserNotifications, macOS 13+ (Ventura)

**Note:** The spec says default port 4580, but `4x live` actually defaults to 4567 (`cmd/4x/live.go:81`). This plan uses 4567 to match the existing codebase.

---

### Task 1: Build System & Resources

**Files:**
- Create: `dashboard/macos/Makefile`
- Create: `dashboard/macos/Info.plist`
- Create: `dashboard/macos/Resources/` (directory)
- Delete: `dashboard/macos/Package.swift`
- Delete: `dashboard/macos/.build/` (SPM build cache)

- [ ] **Step 1: Create Makefile**

```makefile
APP = 4x-live
SRC = $(wildcard Sources/*.swift)
BUNDLE = 4x Live.app

.PHONY: run build clean app

run: build
	./$(APP)

build: $(APP)

$(APP): $(SRC)
	swiftc -framework WebKit -framework AppKit -framework UserNotifications \
	  -O $(SRC) -o $(APP)

app: build
	@mkdir -p "$(BUNDLE)/Contents/MacOS" "$(BUNDLE)/Contents/Resources"
	@cp -f Info.plist "$(BUNDLE)/Contents/"
	@cp -f $(APP) "$(BUNDLE)/Contents/MacOS/$(APP)"
	@cp -f Resources/popover.html "$(BUNDLE)/Contents/MacOS/"
	@test -f Resources/AppIcon.icns && cp -f Resources/AppIcon.icns "$(BUNDLE)/Contents/Resources/" || true
	@touch "$(BUNDLE)"
	@echo "Built: $(BUNDLE)"

clean:
	rm -f $(APP)
	rm -rf "$(BUNDLE)"
```

- [ ] **Step 2: Create Info.plist**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>zh_TW</string>
  <key>CFBundleDisplayName</key>
  <string>4x Live</string>
  <key>CFBundleExecutable</key>
  <string>4x-live</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon.icns</string>
  <key>CFBundleIdentifier</key>
  <string>io.github.ggwhite.4x-live</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>4x Live</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>1.0</string>
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>LSApplicationCategoryType</key>
  <string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>NSPrincipalClass</key>
  <string>NSApplication</string>
</dict>
</plist>
```

- [ ] **Step 3: Create Resources directory**

```bash
mkdir -p dashboard/macos/Resources
```

- [ ] **Step 4: Delete Package.swift and .build**

```bash
rm dashboard/macos/Package.swift
rm -rf dashboard/macos/.build
```

- [ ] **Step 5: Verify build compiles with existing main.swift**

Run: `cd dashboard/macos && make build`
Expected: Compiles successfully, produces `4x-live` binary.

- [ ] **Step 6: Commit**

```bash
git add dashboard/macos/Makefile dashboard/macos/Info.plist dashboard/macos/Resources/
git add dashboard/macos/Package.swift  # staged as delete
git commit -m "feat(F035): replace Package.swift with Makefile build system"
```

---

### Task 2: ServerManager

**Files:**
- Create: `dashboard/macos/Sources/ServerManager.swift`

- [ ] **Step 1: Create ServerManager.swift**

```swift
import Foundation

class ServerManager {
    let port: Int
    private var process: Process?
    private var restartCount = 0
    private let maxRestarts = 3
    private let restartDelays: [TimeInterval] = [1, 2, 4]
    private var onReady: (() -> Void)?
    private var onError: ((String) -> Void)?

    init(port: Int) {
        self.port = port
    }

    func start(onReady: @escaping () -> Void, onError: @escaping (String) -> Void) {
        self.onReady = onReady
        self.onError = onError
        launchServer()
    }

    func stop() {
        guard let proc = process, proc.isRunning else { return }
        proc.terminate()
        DispatchQueue.global().asyncAfter(deadline: .now() + 3) {
            if proc.isRunning {
                proc.interrupt()
            }
        }
        process = nil
    }

    private func launchServer() {
        guard let binaryPath = findBinary() else {
            onError?("Cannot find '4x' binary in PATH or GOPATH")
            return
        }

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: binaryPath)
        proc.arguments = ["live", "-p", "\(port)"]
        proc.standardOutput = FileHandle.nullDevice
        proc.standardError = FileHandle.nullDevice

        proc.terminationHandler = { [weak self] p in
            guard let self = self else { return }
            if p.terminationStatus != 0 {
                self.handleCrash()
            }
        }

        do {
            try proc.run()
            process = proc
            pollUntilReady()
        } catch {
            onError?("Failed to launch 4x live: \(error.localizedDescription)")
        }
    }

    private func pollUntilReady() {
        let url = URL(string: "http://localhost:\(port)/api/projects")!
        let task = URLSession.shared.dataTask(with: url) { [weak self] _, response, _ in
            guard let self = self else { return }
            if let http = response as? HTTPURLResponse, http.statusCode == 200 {
                DispatchQueue.main.async { self.onReady?() }
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    self.pollUntilReady()
                }
            }
        }
        task.resume()
    }

    private func handleCrash() {
        guard restartCount < maxRestarts else {
            DispatchQueue.main.async {
                self.onError?("Server crashed \(self.maxRestarts) times, giving up")
            }
            return
        }
        let delay = restartDelays[restartCount]
        restartCount += 1
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            self?.launchServer()
        }
    }

    private func findBinary() -> String? {
        let fm = FileManager.default
        let pathDirs = (ProcessInfo.processInfo.environment["PATH"] ?? "")
            .split(separator: ":").map(String.init)
        for dir in pathDirs {
            let candidate = (dir as NSString).appendingPathComponent("4x")
            if fm.isExecutableFile(atPath: candidate) {
                return candidate
            }
        }
        let pipe = Pipe()
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        proc.arguments = ["go", "env", "GOPATH"]
        proc.standardOutput = pipe
        proc.standardError = FileHandle.nullDevice
        do {
            try proc.run()
            proc.waitUntilExit()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            if let gopath = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
               !gopath.isEmpty {
                let candidate = (gopath as NSString).appendingPathComponent("bin/4x")
                if fm.isExecutableFile(atPath: candidate) {
                    return candidate
                }
            }
        } catch {}
        return nil
    }

    var baseURL: String {
        "http://localhost:\(port)"
    }
}
```

- [ ] **Step 2: Verify build**

Run: `cd dashboard/macos && make build`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add dashboard/macos/Sources/ServerManager.swift
git commit -m "feat(F035): add ServerManager for 4x live subprocess lifecycle"
```

---

### Task 3: AppDelegate — Window, Menu, Folder Picker

**Files:**
- Create: `dashboard/macos/Sources/AppDelegate.swift`
- Modify: `dashboard/macos/Sources/main.swift`

This refactors the existing `main.swift` into `AppDelegate.swift` with added menu support, and reduces `main.swift` to just the entry point.

- [ ] **Step 1: Create AppDelegate.swift**

```swift
import AppKit
import WebKit

class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate,
                   WKNavigationDelegate, WKScriptMessageHandler {
    var window: NSWindow!
    var webView: WKWebView!
    var serverManager: ServerManager!
    var statusItemController: StatusItemController!
    var eventListener: EventListener!

    var serverPort: Int = 4567

    func applicationDidFinishLaunching(_ notification: Notification) {
        parseArgs()

        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        NSApp.appearance = NSAppearance(named: .darkAqua)

        setupMenu()
        createWindow()
        setupStatusItem()

        serverManager = ServerManager(port: serverPort)
        serverManager.start(
            onReady: { [weak self] in self?.onServerReady() },
            onError: { [weak self] msg in self?.showError(msg) }
        )
    }

    // MARK: - Args

    func parseArgs() {
        let args = CommandLine.arguments
        for (i, arg) in args.enumerated() {
            if arg.starts(with: "--port="),
               let p = Int(arg.replacingOccurrences(of: "--port=", with: "")) {
                serverPort = p
            } else if arg == "--port", i + 1 < args.count,
                      let p = Int(args[i + 1]) {
                serverPort = p
            }
        }
    }

    // MARK: - Window

    func createWindow() {
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "nativeOpenFolder")
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")
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
        window.delegate = self
        window.center()
        window.setFrameAutosaveName("4xLiveWindow")
        window.makeKeyAndOrderFront(nil)
    }

    func onServerReady() {
        let url = URL(string: "http://localhost:\(serverPort)")!
        webView.load(URLRequest(url: url))

        eventListener = EventListener(baseURL: serverManager.baseURL)
        eventListener.start()
    }

    func showError(_ message: String) {
        let alert = NSAlert()
        alert.messageText = "4x Live"
        alert.informativeText = message
        alert.alertStyle = .critical
        alert.runModal()
    }

    // MARK: - Menu

    func setupMenu() {
        let mainMenu = NSMenu()

        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About 4x Live", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit 4x Live", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        let appMenuItem = NSMenuItem()
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        let fileMenu = NSMenu(title: "File")
        fileMenu.addItem(withTitle: "New Feature…", action: #selector(newFeature), keyEquivalent: "n")
        let fileMenuItem = NSMenuItem()
        fileMenuItem.submenu = fileMenu
        mainMenu.addItem(fileMenuItem)

        let viewMenu = NSMenu(title: "View")
        viewMenu.addItem(withTitle: "Reload", action: #selector(reloadPage), keyEquivalent: "r")
        let toggleItem = NSMenuItem(title: "Toggle Sidebar", action: #selector(toggleSidebar), keyEquivalent: "S")
        toggleItem.keyEquivalentModifierMask = [.command, .shift]
        viewMenu.addItem(toggleItem)
        let viewMenuItem = NSMenuItem()
        viewMenuItem.submenu = viewMenu
        mainMenu.addItem(viewMenuItem)

        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.miniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.zoom(_:)), keyEquivalent: "")
        let windowMenuItem = NSMenuItem()
        windowMenuItem.submenu = windowMenu
        mainMenu.addItem(windowMenuItem)

        NSApp.mainMenu = mainMenu
    }

    @objc func newFeature() {
        showMainWindow()
        webView.evaluateJavaScript("document.querySelector('[data-action=\"new-feature\"]')?.click()", completionHandler: nil)
    }

    @objc func reloadPage() {
        webView.reload()
    }

    @objc func toggleSidebar() {
        webView.evaluateJavaScript("toggleSidebar?.()", completionHandler: nil)
    }

    // MARK: - Window Delegate

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        window.orderOut(nil)
        return false
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { showMainWindow() }
        return true
    }

    func showMainWindow() {
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    // MARK: - Cleanup

    func applicationWillTerminate(_ notification: Notification) {
        eventListener?.stop()
        serverManager?.stop()
    }

    // MARK: - WebView Navigation

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        webView.evaluateJavaScript("window._isNativeApp = true;", completionHandler: nil)
        startTitleSync()
    }

    func startTitleSync() {
        Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            let js = "activeProjectId ? (openTabs.find(t=>t.id===activeProjectId)||{}).name || '4x Live' : '4x Live'"
            self.webView.evaluateJavaScript(js) { result, _ in
                if let name = result as? String {
                    DispatchQueue.main.async {
                        self.window.title = name == "4x Live" ? name : "\(name) — 4x Live"
                    }
                }
            }
        }
    }

    // MARK: - Native Folder Picker

    func userContentController(_ userContentController: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        if message.name == "nativeOpenFolder" {
            let panel = NSOpenPanel()
            panel.canChooseDirectories = true
            panel.canChooseFiles = false
            panel.allowsMultipleSelection = false
            panel.message = "Select a 4x project folder"

            if panel.runModal() == .OK, let url = panel.url {
                let path = url.path.replacingOccurrences(of: "'", with: "\\'")
                webView.evaluateJavaScript("addProjectFromNative('\(path)')", completionHandler: nil)
            }
        }
    }

    // MARK: - Status Item

    func setupStatusItem() {
        statusItemController = StatusItemController(port: serverPort, showMainWindow: { [weak self] in
            self?.showMainWindow()
        })
    }
}
```

- [ ] **Step 2: Rewrite main.swift as entry point**

Replace the entire content of `main.swift` with:

```swift
import AppKit

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
```

- [ ] **Step 3: Create stub files so build succeeds**

Create `dashboard/macos/Sources/StatusItemController.swift`:

```swift
import AppKit

class StatusItemController {
    let port: Int
    let showMainWindow: () -> Void

    init(port: Int, showMainWindow: @escaping () -> Void) {
        self.port = port
        self.showMainWindow = showMainWindow
    }
}
```

Create `dashboard/macos/Sources/EventListener.swift`:

```swift
import Foundation

class EventListener {
    let baseURL: String

    init(baseURL: String) {
        self.baseURL = baseURL
    }

    func start() {}
    func stop() {}
}
```

- [ ] **Step 4: Verify build and run**

Run: `cd dashboard/macos && make build`
Expected: Compiles successfully.

Run: `cd dashboard/macos && make run`
Expected: Window appears with "4x Live" title, menu bar shows File/View/Window menus. If `4x` is in PATH, server spawns and web UI loads. If not, error alert shows.

- [ ] **Step 5: Commit**

```bash
git add dashboard/macos/Sources/AppDelegate.swift dashboard/macos/Sources/main.swift
git add dashboard/macos/Sources/StatusItemController.swift dashboard/macos/Sources/EventListener.swift
git commit -m "feat(F035): refactor AppDelegate with menu, window delegate, server integration"
```

---

### Task 4: StatusItemController + Popover

**Files:**
- Modify: `dashboard/macos/Sources/StatusItemController.swift`
- Create: `dashboard/macos/Resources/popover.html`

- [ ] **Step 1: Create popover.html**

```html
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, sans-serif;
    font-size: 13px;
    color: #e5e5e5;
    background: transparent;
    padding: 16px;
    -webkit-user-select: none;
  }
  .header {
    display: flex; justify-content: space-between; align-items: center;
    margin-bottom: 14px;
  }
  .header h1 { font-size: 14px; font-weight: 700; }
  .header button {
    background: rgba(255,255,255,.1); border: none; color: #e5e5e5;
    padding: 4px 12px; border-radius: 6px; font-size: 12px; cursor: pointer;
  }
  .header button:hover { background: rgba(255,255,255,.18); }
  .cards {
    display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 8px;
    margin-bottom: 16px;
  }
  .card {
    background: rgba(255,255,255,.06); border-radius: 8px; padding: 10px;
    text-align: center;
  }
  .card .value { font-size: 22px; font-weight: 700; }
  .card .label { font-size: 10px; color: #a1a1aa; margin-top: 2px; }
  .card.running .value { color: #10b981; }
  .card.pending .value { color: #f59e0b; }
  .card.done .value { color: #6366f1; }
  .section-title {
    font-size: 10px; font-weight: 700; text-transform: uppercase;
    letter-spacing: .06em; color: #71717a; margin: 12px 0 8px;
  }
  .task-list { max-height: 240px; overflow-y: auto; }
  .task-item {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 8px; border-radius: 6px; margin-bottom: 2px;
  }
  .task-item:hover { background: rgba(255,255,255,.06); }
  .task-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .task-name { flex: 1; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .task-role {
    font-size: 10px; padding: 1px 6px; border-radius: 4px;
    background: rgba(255,255,255,.08); color: #a1a1aa; flex-shrink: 0;
  }
  .task-time { font-size: 10px; color: #71717a; flex-shrink: 0; }
  .empty { color: #52525b; font-size: 12px; text-align: center; padding: 20px; }
  .footer {
    margin-top: 12px; padding-top: 10px;
    border-top: 1px solid rgba(255,255,255,.08);
    display: flex; justify-content: flex-end;
  }
  .footer button {
    background: none; border: none; color: #71717a;
    font-size: 11px; cursor: pointer; padding: 2px 6px;
  }
  .footer button:hover { color: #e5e5e5; }
</style>
</head>
<body>
<div class="header">
  <h1>4x Live</h1>
  <button onclick="openDashboard()">Open</button>
</div>
<div class="cards">
  <div class="card running"><div class="value" id="v-running">0</div><div class="label">Running</div></div>
  <div class="card pending"><div class="value" id="v-pending">0</div><div class="label">Pending</div></div>
  <div class="card done"><div class="value" id="v-done">0</div><div class="label">Done</div></div>
  <div class="card"><div class="value" id="v-total">0</div><div class="label">Total</div></div>
</div>
<div class="section-title">Active Tasks</div>
<div class="task-list" id="task-list">
  <div class="empty">No active tasks</div>
</div>
<div class="footer">
  <button onclick="quitApp()">Quit 4x Live</button>
</div>
<script>
const ROLE_COLORS = {
  designer: '#a78bfa', coder: '#22d3ee',
  reviewer: '#34d399', tester: '#fb923c'
};

function updateData(data) {
  document.getElementById('v-running').textContent = data.running;
  document.getElementById('v-pending').textContent = data.pending;
  document.getElementById('v-done').textContent = data.done;
  document.getElementById('v-total').textContent = data.total;

  const list = document.getElementById('task-list');
  if (!data.tasks || data.tasks.length === 0) {
    list.innerHTML = '<div class="empty">No active tasks</div>';
    return;
  }
  list.innerHTML = data.tasks.map(t => {
    const color = ROLE_COLORS[t.role] || '#71717a';
    return `<div class="task-item">
      <div class="task-dot" style="background:${color}"></div>
      <div class="task-name">${esc(t.name)}</div>
      <div class="task-role">${t.role || '—'}</div>
      <div class="task-time">${t.elapsed || ''}</div>
    </div>`;
  }).join('');
}

function esc(s) {
  const d = document.createElement('div');
  d.textContent = s || '';
  return d.innerHTML;
}

function openDashboard() {
  window.webkit.messageHandlers.popover.postMessage({action: 'open'});
}

function quitApp() {
  window.webkit.messageHandlers.popover.postMessage({action: 'quit'});
}

/* {{DATA_INIT}} */
</script>
</body>
</html>
```

- [ ] **Step 2: Implement StatusItemController**

Replace `StatusItemController.swift` with:

```swift
import AppKit
import WebKit

class StatusItemController: NSObject, WKScriptMessageHandler {
    let port: Int
    let showMainWindow: () -> Void

    private var statusItem: NSStatusItem!
    private var popover: NSPopover!
    private var popoverWebView: WKWebView!
    private var popoverHTML: String = ""
    private var refreshTimer: Timer?

    init(port: Int, showMainWindow: @escaping () -> Void) {
        self.port = port
        self.showMainWindow = showMainWindow
        super.init()

        loadPopoverHTML()
        setupPopover()
        setupStatusItem()
    }

    // MARK: - Status Item

    private func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: .variableLength)
        guard let button = statusItem.button else { return }
        button.image = makeIcon(running: false)
        button.imagePosition = .imageLeading
        button.target = self
        button.action = #selector(statusItemClicked)
    }

    private func makeIcon(running: Bool) -> NSImage {
        let name = running ? "hexagon.fill" : "hexagon"
        if let img = NSImage(systemSymbolName: name, accessibilityDescription: "4x Live") {
            let config = NSImage.SymbolConfiguration(pointSize: 14, weight: .medium)
            let sized = img.withSymbolConfiguration(config) ?? img
            sized.isTemplate = true
            return sized
        }
        let img = NSImage(size: NSSize(width: 18, height: 18))
        img.isTemplate = true
        return img
    }

    @objc func statusItemClicked() {
        if popover.isShown {
            popover.performClose(nil)
            stopRefresh()
        } else {
            refreshPopover()
            guard let button = statusItem.button else { return }
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            startRefresh()
        }
    }

    // MARK: - Popover

    private func loadPopoverHTML() {
        let candidates = [
            Bundle.main.bundlePath + "/Contents/MacOS/popover.html",
            (CommandLine.arguments[0] as NSString)
                .deletingLastPathComponent + "/Resources/popover.html",
            (CommandLine.arguments[0] as NSString)
                .deletingLastPathComponent + "/../Resources/popover.html"
        ]
        for path in candidates {
            if let html = try? String(contentsOfFile: path, encoding: .utf8) {
                popoverHTML = html
                return
            }
        }
    }

    private func setupPopover() {
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "popover")
        config.preferences.setValue(true, forKey: "developerExtrasEnabled")

        popoverWebView = WKWebView(frame: NSRect(x: 0, y: 0, width: 320, height: 400), configuration: config)
        popoverWebView.setValue(false, forKey: "drawsBackground")

        let vc = NSViewController()
        vc.view = popoverWebView

        popover = NSPopover()
        popover.contentSize = NSSize(width: 320, height: 420)
        popover.behavior = .transient
        popover.animates = true
        popover.contentViewController = vc
        popover.appearance = NSAppearance(named: .darkAqua)
    }

    // MARK: - Data Refresh

    private func startRefresh() {
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 3.0, repeats: true) { [weak self] _ in
            self?.refreshPopover()
        }
    }

    private func stopRefresh() {
        refreshTimer?.invalidate()
        refreshTimer = nil
    }

    func refreshPopover() {
        fetchSummary { [weak self] data in
            guard let self = self, !self.popoverHTML.isEmpty else { return }
            let json = data ?? "{\"running\":0,\"pending\":0,\"done\":0,\"total\":0,\"tasks\":[]}"
            let html = self.popoverHTML.replacingOccurrences(
                of: "/* {{DATA_INIT}} */",
                with: "updateData(\(json));"
            )
            DispatchQueue.main.async {
                self.popoverWebView.loadHTMLString(html, baseURL: nil)
            }
        }
    }

    func updateStatusIcon() {
        fetchRunCount { [weak self] count in
            guard let self = self, let button = self.statusItem?.button else { return }
            DispatchQueue.main.async {
                let running = count > 0
                button.image = self.makeIcon(running: running)
                button.title = running ? " \(count)" : ""
                NSApp.dockTile.badgeLabel = running ? "\(count)" : nil
            }
        }
    }

    // MARK: - API

    private func fetchSummary(completion: @escaping (String?) -> Void) {
        let url = URL(string: "http://localhost:\(port)/api/tasks")!
        URLSession.shared.dataTask(with: url) { data, _, _ in
            guard let data = data,
                  let tasks = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
                completion(nil)
                return
            }

            var running = 0, pending = 0, done = 0
            var activeTasks: [[String: Any]] = []

            for t in tasks {
                let status = t["status"] as? String ?? ""
                let active = t["active"] as? Bool ?? false
                if status == "done" { done += 1 }
                else if active { running += 1 }
                else { pending += 1 }

                if active {
                    var entry: [String: Any] = [
                        "name": t["name"] as? String ?? t["id"] as? String ?? "?",
                        "role": t["role"] as? String ?? ""
                    ]
                    if let updated = t["updatedAt"] as? String {
                        entry["elapsed"] = self.formatElapsed(updated)
                    }
                    activeTasks.append(entry)
                }
            }

            let summary: [String: Any] = [
                "running": running, "pending": pending,
                "done": done, "total": tasks.count,
                "tasks": activeTasks
            ]
            if let json = try? JSONSerialization.data(withJSONObject: summary),
               let str = String(data: json, encoding: .utf8) {
                completion(str)
            } else {
                completion(nil)
            }
        }.resume()
    }

    private func fetchRunCount(completion: @escaping (Int) -> Void) {
        let url = URL(string: "http://localhost:\(port)/api/runs")!
        URLSession.shared.dataTask(with: url) { data, _, _ in
            guard let data = data,
                  let runs = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
                completion(0)
                return
            }
            completion(runs.count)
        }.resume()
    }

    private func formatElapsed(_ isoString: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: isoString)
                ?? ISO8601DateFormatter().date(from: isoString) else { return "" }
        let seconds = Int(-date.timeIntervalSinceNow)
        if seconds < 60 { return "\(seconds)s" }
        if seconds < 3600 { return "\(seconds / 60)m" }
        return "\(seconds / 3600)h\((seconds % 3600) / 60)m"
    }

    // MARK: - Popover Message Handler

    func userContentController(_ userContentController: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any],
              let action = body["action"] as? String else { return }
        switch action {
        case "open":
            popover.performClose(nil)
            stopRefresh()
            showMainWindow()
        case "quit":
            NSApp.terminate(nil)
        default:
            break
        }
    }
}
```

- [ ] **Step 3: Wire status icon refresh into AppDelegate**

Add this line to `AppDelegate.onServerReady()`, after `eventListener.start()`:

```swift
        startStatusRefresh()
```

Add this method to `AppDelegate`:

```swift
    private func startStatusRefresh() {
        statusItemController.updateStatusIcon()
        Timer.scheduledTimer(withTimeInterval: 5.0, repeats: true) { [weak self] _ in
            self?.statusItemController.updateStatusIcon()
        }
    }
```

- [ ] **Step 4: Verify build and run**

Run: `cd dashboard/macos && make build`
Expected: Compiles successfully.

Run: `cd dashboard/macos && make run`
Expected: Menu bar shows hexagon icon. Clicking it opens popover with summary cards and task list. "Open" button focuses main window. "Quit" terminates app. Active run count shows next to icon and on Dock badge.

- [ ] **Step 5: Commit**

```bash
git add dashboard/macos/Sources/StatusItemController.swift dashboard/macos/Resources/popover.html
git add dashboard/macos/Sources/AppDelegate.swift
git commit -m "feat(F035): add menu bar status item with popover and Dock badge"
```

---

### Task 5: EventListener — SSE + Notifications

**Files:**
- Modify: `dashboard/macos/Sources/EventListener.swift`

- [ ] **Step 1: Implement EventListener**

Uses polling-based change detection instead of SSE streaming. `URLSession.dataTask` doesn't support incremental SSE delivery — polling `/api/tasks` every 5 seconds is simpler and notification latency is acceptable.

Replace `EventListener.swift` with:

```swift
import Foundation
import UserNotifications

class EventListener: NSObject, UNUserNotificationCenterDelegate {
    let baseURL: String
    private var pollTimer: Timer?
    private var lastPhase: [String: String] = [:]
    private var lastActive: [String: Bool] = [:]

    init(baseURL: String) {
        self.baseURL = baseURL
        super.init()
        requestNotificationPermission()
        UNUserNotificationCenter.current().delegate = self
    }

    func start() {
        snapshotCurrent()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 5.0, repeats: true) { [weak self] _ in
            self?.checkForChanges()
        }
    }

    func stop() {
        pollTimer?.invalidate()
        pollTimer = nil
    }

    // MARK: - Notification Permission

    private func requestNotificationPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    // MARK: - Polling

    private func snapshotCurrent() {
        fetchTasks { [weak self] tasks in
            guard let self = self else { return }
            DispatchQueue.main.async {
                for t in tasks {
                    let id = t["id"] as? String ?? ""
                    guard !id.isEmpty else { continue }
                    self.lastPhase[id] = t["phase"] as? String ?? ""
                    self.lastActive[id] = t["active"] as? Bool ?? false
                }
            }
        }
    }

    private func checkForChanges() {
        fetchTasks { [weak self] tasks in
            guard let self = self else { return }
            DispatchQueue.main.async {
                for t in tasks {
                    let id = t["id"] as? String ?? ""
                    guard !id.isEmpty else { continue }
                    let phase = t["phase"] as? String ?? ""
                    let active = t["active"] as? Bool ?? false
                    let name = t["name"] as? String ?? id
                    let prevPhase = self.lastPhase[id] ?? ""
                    let prevActive = self.lastActive[id] ?? false

                    if phase == "pending-review" && prevPhase != "pending-review" {
                        self.sendNotification(id: id, title: "等待確認",
                                              body: "\(id) \(name) 等待你的確認")
                    }

                    if !active && prevActive {
                        let stopReason = t["stopReason"] as? String ?? ""
                        if stopReason == "error" || stopReason == "hard-error" {
                            self.sendNotification(id: id, title: "執行失敗",
                                                  body: "\(id) \(name) 執行失敗")
                        } else if phase == "pending-review" || phase == "done" {
                            if prevPhase != "pending-review" && prevPhase != "done" {
                                self.sendNotification(id: id, title: "完成",
                                                      body: "\(id) \(name) 完成")
                            }
                        }
                    }

                    self.lastPhase[id] = phase
                    self.lastActive[id] = active
                }
            }
        }
    }

    private func fetchTasks(completion: @escaping ([[String: Any]]) -> Void) {
        let url = URL(string: "\(baseURL)/api/tasks")!
        URLSession.shared.dataTask(with: url) { data, _, _ in
            guard let data = data,
                  let tasks = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
                completion([])
                return
            }
            completion(tasks)
        }.resume()
    }

    // MARK: - Notifications

    private func sendNotification(id: String, title: String, body: String) {
        let content = UNMutableNotificationContent()
        content.title = "4x Live — \(title)"
        content.body = body
        content.sound = .default
        content.userInfo = ["featureId": id]

        let request = UNNotificationRequest(
            identifier: "4x-\(id)-\(UUID().uuidString.prefix(8))",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request, withCompletionHandler: nil)
    }

    // MARK: - Notification Delegate

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        if let featureId = response.notification.request.content.userInfo["featureId"] as? String {
            DispatchQueue.main.async {
                NotificationCenter.default.post(
                    name: NSNotification.Name("openFeature"),
                    object: nil,
                    userInfo: ["featureId": featureId]
                )
            }
        }
        completionHandler()
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound])
    }
}
```

- [ ] **Step 2: Handle notification click in AppDelegate**

Add to the end of `AppDelegate.applicationDidFinishLaunching`:

```swift
        NotificationCenter.default.addObserver(forName: NSNotification.Name("openFeature"),
                                               object: nil, queue: .main) { [weak self] notif in
            guard let self = self,
                  let featureId = notif.userInfo?["featureId"] as? String else { return }
            self.showMainWindow()
            let js = "selectFeatureById?.('\(featureId.replacingOccurrences(of: "'", with: "\\'"))')"
            self.webView.evaluateJavaScript(js, completionHandler: nil)
        }
```

- [ ] **Step 3: Verify build**

Run: `cd dashboard/macos && make build`
Expected: Compiles successfully with UserNotifications framework.

- [ ] **Step 4: Commit**

```bash
git add dashboard/macos/Sources/EventListener.swift dashboard/macos/Sources/AppDelegate.swift
git commit -m "feat(F035): add SSE event listener with macOS notifications"
```

---

### Task 6: Integration Verification & Documentation

**Files:**
- Modify: `docs/guide/dashboard.md`

- [ ] **Step 1: Build the .app bundle**

Run: `cd dashboard/macos && make app`
Expected: `4x Live.app` directory created with proper structure.

Run: `ls -la "dashboard/macos/4x Live.app/Contents/MacOS/"`
Expected: `4x-live` binary and `popover.html` present.

- [ ] **Step 2: Full integration test**

Run: `open "dashboard/macos/4x Live.app"`

Manual checklist:
1. App launches, `4x live` server starts automatically
2. Web UI loads in main window after server is ready
3. Menu bar shows hexagon icon
4. Click icon → popover shows with summary cards
5. "Open" button in popover → main window comes to front
6. ⌘N → new feature modal opens in web UI
7. ⌘R → page reloads
8. Close window → app stays running (menu bar icon remains)
9. Click Dock icon → window reopens
10. Start a feature run → Dock badge shows "1", menu bar shows filled hexagon with count
11. Feature reaches pending-review → macOS notification appears
12. Click notification → main window opens, feature tab selected
13. Quit via popover or ⌘Q → server process terminates

- [ ] **Step 3: Test server crash recovery**

Run the app, then in another terminal:
```bash
pkill -f "4x live"
```
Expected: App detects crash, restarts server within 1 second, web UI reconnects.

- [ ] **Step 4: Update dashboard documentation**

Update `docs/guide/dashboard.md` — replace the Platforms table at the bottom:

```markdown
## Platforms

| Platform | Status |
|---|---|
| Web UI (embedded) | Available |
| macOS native (Swift) | Available |
| Electron (Windows/Linux) | Planned |

## macOS Native App

Build the native app:

```bash
cd dashboard/macos
make app      # Build 4x Live.app bundle
```

The app automatically starts the `4x live` server — no need to run it separately.

Features:
- Menu bar status item with quick-glance popover
- macOS notifications (feature completion, pending review)
- Dock badge showing active run count
- Native folder picker for adding projects
- Keyboard shortcuts (⌘N new feature, ⌘R reload)
```

- [ ] **Step 5: Commit**

```bash
git add docs/guide/dashboard.md
git commit -m "docs(F035): update dashboard guide with macOS native app instructions"
```

---

### Task 7: Update progress.md & Cleanup

**Files:**
- Modify: `progress.md`

- [ ] **Step 1: Update progress.md**

In the Dashboard row of the Current Status table, change:

```
| Dashboard | web UI (SSE + REST API) | — | macOS native (Swift), Electron |
```

to:

```
| Dashboard | web UI (SSE + REST API), macOS native (Swift) | — | Electron |
```

- [ ] **Step 2: Final commit**

```bash
git add progress.md
git commit -m "docs(F035): mark macOS native dashboard as done in progress"
```
