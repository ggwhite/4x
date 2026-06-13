# F036: Electron Cross-Platform Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 為 Windows/Linux 提供與 F035 macOS native app 功能對等的 Electron 桌面 dashboard。

**Architecture:** Electron main process 管理 `4x live` Go server subprocess，BrowserWindow 載入 localhost web UI。系統整合（tray、通知、badge、auto-update）全在 main process，renderer 只負責顯示。每個功能封裝為獨立 manager 模組，main.js 組裝。

**Tech Stack:** Electron 35+, electron-builder, electron-updater, eventsource, electron-window-state, vitest

---

### Task 1: Project Scaffolding

**Files:**
- Create: `dashboard/electron/package.json`
- Create: `dashboard/electron/electron-builder.yml`
- Create: `dashboard/electron/src/main.js`
- Create: `dashboard/electron/src/preload.js`

- [ ] **Step 1: 建立 package.json**

```json
{
  "name": "4x-live",
  "version": "0.1.0",
  "description": "4x Live Dashboard for Windows and Linux",
  "main": "src/main.js",
  "scripts": {
    "dev": "electron .",
    "dist": "electron-builder --win --linux",
    "dist:win": "electron-builder --win",
    "dist:linux": "electron-builder --linux",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "devDependencies": {
    "electron": "^35.0.0",
    "electron-builder": "^25.0.0",
    "vitest": "^3.0.0"
  },
  "dependencies": {
    "electron-updater": "^6.0.0",
    "electron-window-state": "^5.0.0",
    "eventsource": "^3.0.0"
  }
}
```

- [ ] **Step 2: 建立 electron-builder.yml**

```yaml
appId: com.4x.live
productName: 4x Live
directories:
  output: dist

win:
  target:
    - target: nsis
      arch: [x64]
  icon: assets/icon.ico

nsis:
  oneClick: false
  allowToChangeInstallationDirectory: true
  deleteAppDataOnUninstall: true

linux:
  target:
    - target: AppImage
      arch: [x64]
    - target: deb
      arch: [x64]
  icon: assets/icon.png
  category: Development

publish:
  provider: github
  owner: ggwhite
  repo: 4x
```

- [ ] **Step 3: 建立最小 main.js（可啟動的空殼）**

```js
const { app, BrowserWindow } = require('electron')

let mainWindow = null

app.whenReady().then(() => {
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    title: '4x Live',
  })
  mainWindow.loadURL('about:blank')
})

app.on('window-all-closed', () => {
  app.quit()
})
```

- [ ] **Step 4: 建立空的 preload.js**

```js
const { contextBridge } = require('electron')

contextBridge.exposeInMainWorld('fourx', {
  isElectronApp: true,
})
```

- [ ] **Step 5: 安裝依賴並驗證啟動**

```bash
cd dashboard/electron && npm install
```

確認 `node_modules/` 產生，無錯誤。將 `node_modules/` 和 `dist/` 加入 `.gitignore`。

- [ ] **Step 6: 建立 .gitignore**

建立 `dashboard/electron/.gitignore`：

```
node_modules/
dist/
```

- [ ] **Step 7: Commit**

```bash
git add dashboard/electron/package.json dashboard/electron/package-lock.json \
  dashboard/electron/electron-builder.yml dashboard/electron/src/main.js \
  dashboard/electron/src/preload.js dashboard/electron/.gitignore
git commit -m "feat(F036): scaffold Electron project structure"
```

---

### Task 2: ServerManager

**Files:**
- Create: `dashboard/electron/src/server-manager.js`
- Create: `dashboard/electron/test/server-manager.test.js`

- [ ] **Step 1: 寫 ServerManager 測試 — binary 查找**

```js
// test/server-manager.test.js
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock child_process and http before importing
vi.mock('child_process', () => ({
  execSync: vi.fn(),
  spawn: vi.fn(() => ({
    pid: 12345,
    on: vi.fn(),
    stdout: { on: vi.fn() },
    stderr: { on: vi.fn() },
    kill: vi.fn(),
  })),
}))

const { ServerManager } = await import('../src/server-manager.js')
const { execSync } = await import('child_process')

describe('ServerManager', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    delete process.env.FOURX_BIN
  })

  describe('findBinary', () => {
    it('uses FOURX_BIN env if set', async () => {
      process.env.FOURX_BIN = '/custom/path/4x'
      const mgr = new ServerManager()
      const result = mgr.findBinary()
      expect(result).toBe('/custom/path/4x')
    })

    it('falls back to which/where', () => {
      execSync.mockReturnValue(Buffer.from('/usr/local/bin/4x\n'))
      const mgr = new ServerManager()
      const result = mgr.findBinary()
      expect(result).toBe('/usr/local/bin/4x')
    })

    it('falls back to GOPATH', () => {
      execSync.mockImplementation((cmd) => {
        if (cmd.startsWith('which') || cmd.startsWith('where')) {
          throw new Error('not found')
        }
        if (cmd === 'go env GOPATH') {
          return Buffer.from('/home/user/go\n')
        }
        throw new Error('unknown')
      })
      const mgr = new ServerManager()
      const result = mgr.findBinary()
      expect(result).toBe('/home/user/go/bin/4x')
    })

    it('returns null when no binary found', () => {
      execSync.mockImplementation(() => { throw new Error('not found') })
      const mgr = new ServerManager()
      const result = mgr.findBinary()
      expect(result).toBeNull()
    })
  })
})
```

- [ ] **Step 2: 跑測試確認失敗**

```bash
cd dashboard/electron && npx vitest run test/server-manager.test.js
```

預期：FAIL（模組不存在）

- [ ] **Step 3: 實作 ServerManager**

```js
// src/server-manager.js
const { execSync, spawn } = require('child_process')
const path = require('path')
const http = require('http')
const EventEmitter = require('events')

class ServerManager extends EventEmitter {
  constructor(opts = {}) {
    super()
    this.port = opts.port || 4567
    this.maxRestarts = 3
    this.restartDelays = [1000, 2000, 4000]
    this.process = null
    this.restartCount = 0
    this.stopping = false
  }

  findBinary() {
    if (process.env.FOURX_BIN) {
      return process.env.FOURX_BIN
    }

    const whichCmd = process.platform === 'win32' ? 'where' : 'which'
    try {
      const result = execSync(`${whichCmd} 4x`, { stdio: 'pipe' })
      return result.toString().trim().split('\n')[0]
    } catch (_) {}

    try {
      const gopath = execSync('go env GOPATH', { stdio: 'pipe' }).toString().trim()
      const bin = path.join(gopath, 'bin', '4x')
      return bin
    } catch (_) {}

    return null
  }

  async start() {
    const binary = this.findBinary()
    if (!binary) {
      this.emit('error', new Error('4x binary not found'))
      return false
    }

    this.stopping = false
    this.process = spawn(binary, ['live', '-p', String(this.port)], {
      stdio: ['ignore', 'pipe', 'pipe'],
    })

    this.process.on('exit', (code) => {
      this.process = null
      if (!this.stopping) {
        this.emit('unexpected-exit', code)
        this._tryRestart()
      }
    })

    this.process.stderr.on('data', (data) => {
      this.emit('log', data.toString())
    })

    const ready = await this.waitForReady()
    if (ready) {
      this.restartCount = 0
      this.emit('ready')
    }
    return ready
  }

  waitForReady(timeoutMs = 30000) {
    const startTime = Date.now()
    return new Promise((resolve) => {
      const poll = () => {
        if (Date.now() - startTime > timeoutMs) {
          this.emit('error', new Error('Server ready timeout'))
          resolve(false)
          return
        }
        const req = http.get(
          `http://localhost:${this.port}/api/projects`,
          (res) => {
            if (res.statusCode === 200) {
              res.resume()
              resolve(true)
            } else {
              res.resume()
              setTimeout(poll, 500)
            }
          }
        )
        req.on('error', () => setTimeout(poll, 500))
        req.end()
      }
      poll()
    })
  }

  async stop() {
    this.stopping = true
    if (!this.process) return

    const proc = this.process
    const pid = proc.pid

    proc.kill('SIGTERM')

    await new Promise((resolve) => {
      const timeout = setTimeout(() => {
        try {
          if (process.platform === 'win32') {
            execSync(`taskkill /F /PID ${pid}`, { stdio: 'ignore' })
          } else {
            proc.kill('SIGKILL')
          }
        } catch (_) {}
        resolve()
      }, 3000)

      proc.on('exit', () => {
        clearTimeout(timeout)
        resolve()
      })
    })

    this.process = null
  }

  _tryRestart() {
    if (this.restartCount >= this.maxRestarts) {
      this.emit('error', new Error('Max restarts exceeded'))
      return
    }
    const delay = this.restartDelays[this.restartCount] || 4000
    this.restartCount++
    this.emit('restart', this.restartCount)
    setTimeout(() => this.start(), delay)
  }
}

module.exports = { ServerManager }
```

- [ ] **Step 4: 跑測試確認通過**

```bash
cd dashboard/electron && npx vitest run test/server-manager.test.js
```

預期：全部 PASS

- [ ] **Step 5: 加入 restart 邏輯測試**

在 `test/server-manager.test.js` 加入：

```js
describe('restart', () => {
  it('tracks restart count', () => {
    const mgr = new ServerManager()
    mgr.restartCount = 2
    expect(mgr.restartCount).toBe(2)
  })

  it('stops restarting after maxRestarts', () => {
    const mgr = new ServerManager()
    mgr.restartCount = 3
    const errorSpy = vi.fn()
    mgr.on('error', errorSpy)
    mgr._tryRestart()
    expect(errorSpy).toHaveBeenCalledWith(
      expect.objectContaining({ message: 'Max restarts exceeded' })
    )
  })
})
```

- [ ] **Step 6: 跑測試確認通過**

```bash
cd dashboard/electron && npx vitest run test/server-manager.test.js
```

預期：全部 PASS

- [ ] **Step 7: Commit**

```bash
git add dashboard/electron/src/server-manager.js dashboard/electron/test/server-manager.test.js
git commit -m "feat(F036): add ServerManager with binary discovery and auto-restart"
```

---

### Task 3: Main Window 與 Preload

**Files:**
- Modify: `dashboard/electron/src/main.js`
- Modify: `dashboard/electron/src/preload.js`

- [ ] **Step 1: 更新 main.js 整合 ServerManager 和 BrowserWindow**

```js
// src/main.js
const { app, BrowserWindow, dialog } = require('electron')
const path = require('path')
const windowState = require('electron-window-state')
const { ServerManager } = require('./server-manager')

let mainWindow = null
let serverManager = null

const PORT = parseInt(process.argv.find(a => a.startsWith('--port='))?.split('=')[1]) || 4567

function createWindow() {
  const state = windowState({
    defaultWidth: 1200,
    defaultHeight: 800,
  })

  mainWindow = new BrowserWindow({
    x: state.x,
    y: state.y,
    width: state.width,
    height: state.height,
    title: '4x Live',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })

  state.manage(mainWindow)
  return mainWindow
}

async function startServer() {
  serverManager = new ServerManager({ port: PORT })

  serverManager.on('error', (err) => {
    dialog.showErrorBox('4x Live', err.message)
  })

  serverManager.on('restart', (count) => {
    console.log(`Server restarting (attempt ${count})...`)
  })

  const ok = await serverManager.start()
  if (!ok) {
    dialog.showErrorBox(
      '4x Live',
      '4x binary not found.\n\nInstall with: go install github.com/ggwhite/4x/cmd/4x@latest'
    )
    app.quit()
    return false
  }
  return true
}

app.whenReady().then(async () => {
  createWindow()
  mainWindow.loadURL('about:blank')

  const ok = await startServer()
  if (ok) {
    mainWindow.loadURL(`http://localhost:${PORT}`)
  }
})

app.on('window-all-closed', () => {
  app.quit()
})

app.on('before-quit', async () => {
  if (serverManager) {
    await serverManager.stop()
  }
})

module.exports = { getMainWindow: () => mainWindow, getPort: () => PORT }
```

- [ ] **Step 2: 更新 preload.js 加入 native folder picker bridge**

```js
// src/preload.js
const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('fourx', {
  isElectronApp: true,
  openFolder: () => ipcRenderer.invoke('open-folder'),
})
```

- [ ] **Step 3: 在 main.js 加入 IPC handler for folder picker**

在 `app.whenReady()` callback 裡、`createWindow()` 後加入：

```js
const { ipcMain } = require('electron')

// 加在 main.js 頂部 require 區
// const { app, BrowserWindow, dialog, ipcMain } = require('electron')

ipcMain.handle('open-folder', async () => {
  const result = await dialog.showOpenDialog(mainWindow, {
    properties: ['openDirectory'],
    message: 'Select a 4x project folder',
  })
  if (result.canceled) return null
  return result.filePaths[0]
})
```

將 `ipcMain` 加入頂部的 require destructure。完整的 main.js 頂部改為：

```js
const { app, BrowserWindow, dialog, ipcMain } = require('electron')
```

- [ ] **Step 4: 手動驗證**

確認 `4x live` server 在本機有裝且可執行。在終端機跑：

```bash
cd dashboard/electron && npm run dev
```

預期：視窗開啟 → 短暫空白頁 → 載入 `4x live` web UI。關閉視窗 → app 退出 → server process 也一起停。

- [ ] **Step 5: Commit**

```bash
git add dashboard/electron/src/main.js dashboard/electron/src/preload.js
git commit -m "feat(F036): main window with server lifecycle and folder picker bridge"
```

---

### Task 4: TrayManager

**Files:**
- Create: `dashboard/electron/src/tray-manager.js`
- Create: `dashboard/electron/test/tray-manager.test.js`

- [ ] **Step 1: 建立佔位 icon 檔案**

在 `dashboard/electron/assets/` 建立最小 PNG 檔作為開發用佔位圖示。用 Node.js 腳本產生 16x16 灰色和綠色 PNG：

```bash
cd dashboard/electron && mkdir -p assets
```

建立 `dashboard/electron/scripts/gen-placeholder-icons.js`：

```js
// 產生最小佔位 icon（1x1 PNG，開發用）
const fs = require('fs')
const path = require('path')

// 最小合法 PNG（1x1 灰色像素）
const gray = Buffer.from(
  '89504e470d0a1a0a0000000d49484452000000100000001008060000001ff3ff' +
  '610000001849444154789c6260f8cf80001249009a0c005ef600014424c5e500' +
  '00000049454e44ae426082', 'hex'
)

const dir = path.join(__dirname, '..', 'assets')
fs.writeFileSync(path.join(dir, 'tray-idle.png'), gray)
fs.writeFileSync(path.join(dir, 'tray-running-1.png'), gray)
fs.writeFileSync(path.join(dir, 'tray-running-2.png'), gray)
fs.writeFileSync(path.join(dir, 'icon.png'), gray)
console.log('Placeholder icons created')
```

```bash
node scripts/gen-placeholder-icons.js
```

- [ ] **Step 2: 寫 TrayManager 測試**

```js
// test/tray-manager.test.js
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockTray = {
  setToolTip: vi.fn(),
  setContextMenu: vi.fn(),
  setImage: vi.fn(),
  on: vi.fn(),
  destroy: vi.fn(),
}

const mockMenu = { buildFromTemplate: vi.fn(() => 'mock-menu') }

vi.mock('electron', () => ({
  Tray: vi.fn(() => mockTray),
  Menu: mockMenu,
  nativeImage: {
    createFromPath: vi.fn((p) => ({ path: p })),
  },
}))

const { TrayManager } = await import('../src/tray-manager.js')

describe('TrayManager', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('creates tray with idle icon', () => {
    const mgr = new TrayManager({ assetsPath: '/assets' })
    mgr.create()
    expect(mockTray.setToolTip).toHaveBeenCalledWith('4x Live — idle')
  })

  it('updates tooltip with stats', () => {
    const mgr = new TrayManager({ assetsPath: '/assets' })
    mgr.create()
    mgr.update({ running: 2, pending: 1, done: 5 })
    expect(mockTray.setToolTip).toHaveBeenCalledWith(
      '4x Live — Running: 2 / Pending: 1 / Done: 5'
    )
  })

  it('switches to running icon when running > 0', () => {
    const mgr = new TrayManager({ assetsPath: '/assets' })
    mgr.create()
    mgr.update({ running: 1, pending: 0, done: 0 })
    expect(mgr.isAnimating).toBe(true)
  })

  it('switches to idle icon when running = 0', () => {
    const mgr = new TrayManager({ assetsPath: '/assets' })
    mgr.create()
    mgr.update({ running: 0, pending: 0, done: 3 })
    expect(mgr.isAnimating).toBe(false)
  })

  it('destroy cleans up', () => {
    const mgr = new TrayManager({ assetsPath: '/assets' })
    mgr.create()
    mgr.destroy()
    expect(mockTray.destroy).toHaveBeenCalled()
  })
})
```

- [ ] **Step 3: 跑測試確認失敗**

```bash
cd dashboard/electron && npx vitest run test/tray-manager.test.js
```

預期：FAIL（模組不存在）

- [ ] **Step 4: 實作 TrayManager**

```js
// src/tray-manager.js
const { Tray, Menu, nativeImage } = require('electron')
const path = require('path')

class TrayManager {
  constructor(opts = {}) {
    this.assetsPath = opts.assetsPath || path.join(__dirname, '..', 'assets')
    this.onShowWindow = opts.onShowWindow || (() => {})
    this.onQuit = opts.onQuit || (() => {})
    this.tray = null
    this.isAnimating = false
    this.animationTimer = null
    this.animationFrame = 0
    this.stats = { running: 0, pending: 0, done: 0 }
  }

  create() {
    const icon = nativeImage.createFromPath(
      path.join(this.assetsPath, 'tray-idle.png')
    )
    this.tray = new Tray(icon)
    this.tray.setToolTip('4x Live — idle')
    this._rebuildMenu()

    this.tray.on('click', () => {
      this.onShowWindow()
    })
  }

  update(stats) {
    this.stats = stats
    const { running, pending, done } = stats

    if (running > 0) {
      this.tray.setToolTip(
        `4x Live — Running: ${running} / Pending: ${pending} / Done: ${done}`
      )
      if (!this.isAnimating) this._startAnimation()
    } else {
      const parts = []
      if (pending > 0) parts.push(`Pending: ${pending}`)
      if (done > 0) parts.push(`Done: ${done}`)
      const suffix = parts.length > 0
        ? `Running: 0 / ${parts.join(' / ')}`
        : 'idle'
      this.tray.setToolTip(`4x Live — ${suffix}`)
      if (this.isAnimating) this._stopAnimation()
    }

    this._rebuildMenu()
  }

  _rebuildMenu() {
    const { running, pending, done } = this.stats
    const template = [
      {
        label: `Running: ${running} / Pending: ${pending} / Done: ${done}`,
        enabled: false,
      },
      { type: 'separator' },
      {
        label: 'Open Dashboard',
        click: () => this.onShowWindow(),
      },
      { type: 'separator' },
      {
        label: 'Quit 4x Live',
        click: () => this.onQuit(),
      },
    ]
    this.tray.setContextMenu(Menu.buildFromTemplate(template))
  }

  _startAnimation() {
    this.isAnimating = true
    this.animationFrame = 0
    this.animationTimer = setInterval(() => {
      this.animationFrame = (this.animationFrame + 1) % 2
      const file = `tray-running-${this.animationFrame + 1}.png`
      const icon = nativeImage.createFromPath(
        path.join(this.assetsPath, file)
      )
      this.tray.setImage(icon)
    }, 800)
  }

  _stopAnimation() {
    this.isAnimating = false
    if (this.animationTimer) {
      clearInterval(this.animationTimer)
      this.animationTimer = null
    }
    const icon = nativeImage.createFromPath(
      path.join(this.assetsPath, 'tray-idle.png')
    )
    this.tray.setImage(icon)
  }

  destroy() {
    this._stopAnimation()
    if (this.tray) {
      this.tray.destroy()
      this.tray = null
    }
  }
}

module.exports = { TrayManager }
```

- [ ] **Step 5: 跑測試確認通過**

```bash
cd dashboard/electron && npx vitest run test/tray-manager.test.js
```

預期：全部 PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/electron/src/tray-manager.js dashboard/electron/test/tray-manager.test.js \
  dashboard/electron/assets/ dashboard/electron/scripts/
git commit -m "feat(F036): add TrayManager with icon animation and context menu"
```

---

### Task 5: Status Polling 與 Badge

**Files:**
- Create: `dashboard/electron/src/status-poller.js`
- Create: `dashboard/electron/test/status-poller.test.js`

- [ ] **Step 1: 寫 StatusPoller 測試**

```js
// test/status-poller.test.js
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.useFakeTimers()

const { StatusPoller } = await import('../src/status-poller.js')

describe('StatusPoller', () => {
  let fetchMock

  beforeEach(() => {
    fetchMock = vi.fn()
    global.fetch = fetchMock
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('emits stats on successful poll', async () => {
    const runs = [
      { featureId: 'F001', status: 'running' },
      { featureId: 'F002', status: 'running' },
      { featureId: 'F003', status: 'done' },
    ]
    fetchMock.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(runs),
    })

    const poller = new StatusPoller({ port: 4567 })
    const spy = vi.fn()
    poller.on('stats', spy)

    await poller._poll()

    expect(spy).toHaveBeenCalledWith({
      running: 2,
      pending: 0,
      done: 1,
      total: 3,
    })
  })

  it('handles fetch errors gracefully', async () => {
    fetchMock.mockRejectedValue(new Error('connection refused'))
    const poller = new StatusPoller({ port: 4567 })
    const errorSpy = vi.fn()
    poller.on('error', errorSpy)

    await poller._poll()

    expect(errorSpy).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: 跑測試確認失敗**

```bash
cd dashboard/electron && npx vitest run test/status-poller.test.js
```

預期：FAIL

- [ ] **Step 3: 實作 StatusPoller**

```js
// src/status-poller.js
const EventEmitter = require('events')

class StatusPoller extends EventEmitter {
  constructor(opts = {}) {
    super()
    this.port = opts.port || 4567
    this.interval = opts.interval || 5000
    this.timer = null
  }

  start() {
    this._poll()
    this.timer = setInterval(() => this._poll(), this.interval)
  }

  stop() {
    if (this.timer) {
      clearInterval(this.timer)
      this.timer = null
    }
  }

  async _poll() {
    try {
      const resp = await fetch(`http://localhost:${this.port}/api/runs`)
      if (!resp.ok) {
        this.emit('error', new Error(`HTTP ${resp.status}`))
        return
      }
      const runs = await resp.json()
      const stats = {
        running: 0,
        pending: 0,
        done: 0,
        total: runs.length,
      }
      for (const run of runs) {
        if (run.status === 'running') stats.running++
        else if (run.status === 'pending') stats.pending++
        else if (run.status === 'done') stats.done++
      }
      this.emit('stats', stats)
    } catch (err) {
      this.emit('error', err)
    }
  }
}

module.exports = { StatusPoller }
```

- [ ] **Step 4: 跑測試確認通過**

```bash
cd dashboard/electron && npx vitest run test/status-poller.test.js
```

預期：全部 PASS

- [ ] **Step 5: 建立 badge 工具函式**

建立 `dashboard/electron/src/badge.js`：

```js
// src/badge.js
const { nativeImage } = require('electron')

function updateBadge(mainWindow, count) {
  if (!mainWindow) return

  if (process.platform === 'win32') {
    if (count === 0) {
      mainWindow.setOverlayIcon(null, '')
      return
    }
    const canvas = createBadgeIcon(count)
    mainWindow.setOverlayIcon(canvas, `${count} active`)
  } else if (process.platform === 'linux') {
    const { app } = require('electron')
    app.setBadgeCount(count)
  }
}

function createBadgeIcon(count) {
  const size = 16
  const canvas = nativeImage.createFromBuffer(
    renderBadgeBuffer(size, Math.min(count, 99))
  )
  return canvas
}

function renderBadgeBuffer(size, count) {
  // Electron 沒有 canvas API，用 data URL 建立簡單的紅色圓形 badge
  // 實務上用預生成的 icon set (1-9, 9+) 更可靠
  // 這裡先用 nativeImage.createFromDataURL 方式
  const text = count > 9 ? '9+' : String(count)
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}">
    <circle cx="${size/2}" cy="${size/2}" r="${size/2}" fill="#e53e3e"/>
    <text x="${size/2}" y="${size/2 + 4}" text-anchor="middle" fill="white"
      font-size="10" font-family="sans-serif">${text}</text>
  </svg>`
  return Buffer.from(svg)
}

module.exports = { updateBadge }
```

- [ ] **Step 6: Commit**

```bash
git add dashboard/electron/src/status-poller.js dashboard/electron/test/status-poller.test.js \
  dashboard/electron/src/badge.js
git commit -m "feat(F036): add StatusPoller and taskbar badge support"
```

---

### Task 6: NotificationManager

**Files:**
- Create: `dashboard/electron/src/notification-manager.js`
- Create: `dashboard/electron/test/notification-manager.test.js`

- [ ] **Step 1: 寫 NotificationManager 測試**

```js
// test/notification-manager.test.js
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockNotification = vi.fn()
mockNotification.prototype.show = vi.fn()
mockNotification.prototype.on = vi.fn()

vi.mock('electron', () => ({
  Notification: mockNotification,
}))

// Mock eventsource
const mockEventSource = vi.fn()
mockEventSource.prototype.close = vi.fn()
mockEventSource.prototype.addEventListener = vi.fn()
Object.defineProperty(mockEventSource, 'OPEN', { value: 1 })

vi.mock('eventsource', () => ({ default: mockEventSource }))

const { NotificationManager } = await import('../src/notification-manager.js')

describe('NotificationManager', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    global.fetch = vi.fn()
  })

  it('subscribes to active features', async () => {
    const tasks = [
      { featureId: 'F001', name: 'Login', phase: 'coding' },
      { featureId: 'F002', name: 'Auth', phase: 'done' },
    ]
    global.fetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(tasks),
    })

    const mgr = new NotificationManager({ port: 4567 })
    await mgr._syncFeatures()

    // F001 is active (not done), F002 is done — only F001 subscribed
    expect(mgr.connections.size).toBe(1)
    expect(mgr.connections.has('F001')).toBe(true)
  })

  it('unsubscribes features that become inactive', async () => {
    global.fetch
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve([
          { featureId: 'F001', name: 'Login', phase: 'coding' },
        ]),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve([
          { featureId: 'F001', name: 'Login', phase: 'done' },
        ]),
      })

    const mgr = new NotificationManager({ port: 4567 })
    await mgr._syncFeatures()
    expect(mgr.connections.size).toBe(1)

    await mgr._syncFeatures()
    expect(mgr.connections.size).toBe(0)
  })
})
```

- [ ] **Step 2: 跑測試確認失敗**

```bash
cd dashboard/electron && npx vitest run test/notification-manager.test.js
```

預期：FAIL

- [ ] **Step 3: 實作 NotificationManager**

```js
// src/notification-manager.js
const { Notification } = require('electron')
const EventSource = require('eventsource')

const DONE_PHASES = new Set(['done', 'cancelled', 'archived'])

class NotificationManager {
  constructor(opts = {}) {
    this.port = opts.port || 4567
    this.onClickFeature = opts.onClickFeature || (() => {})
    this.connections = new Map()
    this.syncTimer = null
    this.syncInterval = opts.syncInterval || 30000
  }

  start() {
    this._syncFeatures()
    this.syncTimer = setInterval(() => this._syncFeatures(), this.syncInterval)
  }

  stop() {
    if (this.syncTimer) {
      clearInterval(this.syncTimer)
      this.syncTimer = null
    }
    for (const [id, es] of this.connections) {
      es.close()
    }
    this.connections.clear()
  }

  async _syncFeatures() {
    let tasks
    try {
      const resp = await fetch(`http://localhost:${this.port}/api/tasks`)
      if (!resp.ok) return
      tasks = await resp.json()
    } catch (_) {
      return
    }

    const activeIds = new Set()
    const nameMap = new Map()

    for (const task of tasks) {
      if (!DONE_PHASES.has(task.phase)) {
        activeIds.add(task.featureId)
        nameMap.set(task.featureId, task.name || task.featureId)
      }
    }

    // Unsubscribe inactive
    for (const [id, es] of this.connections) {
      if (!activeIds.has(id)) {
        es.close()
        this.connections.delete(id)
      }
    }

    // Subscribe new
    for (const id of activeIds) {
      if (!this.connections.has(id)) {
        this._subscribe(id, nameMap.get(id))
      }
    }
  }

  _subscribe(featureId, name) {
    const url = `http://localhost:${this.port}/sse/events/${featureId}`
    const es = new EventSource(url)

    es.addEventListener('message', (evt) => {
      let event
      try {
        event = JSON.parse(evt.data)
      } catch (_) {
        return
      }
      this._handleEvent(featureId, name, event)
    })

    es.addEventListener('error', () => {
      // EventSource 會自動重連，不需手動處理
    })

    this.connections.set(featureId, es)
  }

  _handleEvent(featureId, name, event) {
    let title, body

    if (event.type === 'transition' && event.phase === 'pending-review') {
      title = '等待確認'
      body = `${featureId} ${name} 等待你的確認`
    } else if (event.type === 'run-error') {
      title = '執行失敗'
      body = `${featureId} ${name} 執行失敗`
    } else if (event.type === 'run-complete') {
      title = '執行完成'
      body = `${featureId} ${name} 完成`
    } else {
      return
    }

    const notification = new Notification({ title, body })
    notification.on('click', () => {
      this.onClickFeature(featureId)
    })
    notification.show()
  }
}

module.exports = { NotificationManager }
```

- [ ] **Step 4: 跑測試確認通過**

```bash
cd dashboard/electron && npx vitest run test/notification-manager.test.js
```

預期：全部 PASS

- [ ] **Step 5: Commit**

```bash
git add dashboard/electron/src/notification-manager.js \
  dashboard/electron/test/notification-manager.test.js
git commit -m "feat(F036): add NotificationManager with SSE subscription"
```

---

### Task 7: Menu 與 Keyboard Shortcuts

**Files:**
- Create: `dashboard/electron/src/menu.js`

- [ ] **Step 1: 實作 menu builder**

```js
// src/menu.js
const { Menu, app } = require('electron')

function buildAppMenu(mainWindow) {
  const template = [
    {
      label: 'File',
      submenu: [
        {
          label: 'New Feature…',
          accelerator: 'CmdOrCtrl+N',
          click: () => {
            if (mainWindow) {
              mainWindow.show()
              mainWindow.webContents.executeJavaScript(
                "document.querySelector('[data-action=\"new-feature\"]')?.click()"
              )
            }
          },
        },
        { type: 'separator' },
        {
          label: 'Quit 4x Live',
          accelerator: 'CmdOrCtrl+Q',
          click: () => app.quit(),
        },
      ],
    },
    {
      label: 'View',
      submenu: [
        {
          label: 'Reload',
          accelerator: 'CmdOrCtrl+R',
          click: () => mainWindow?.webContents.reload(),
        },
        {
          label: 'Toggle Sidebar',
          accelerator: 'CmdOrCtrl+Shift+S',
          click: () => {
            mainWindow?.webContents.executeJavaScript(
              "document.querySelector('[data-action=\"toggle-sidebar\"]')?.click()"
            )
          },
        },
        { type: 'separator' },
        {
          label: 'Toggle DevTools',
          accelerator: 'F12',
          click: () => mainWindow?.webContents.toggleDevTools(),
        },
      ],
    },
    {
      label: 'Window',
      submenu: [
        {
          label: 'Minimize',
          accelerator: 'CmdOrCtrl+M',
          click: () => mainWindow?.minimize(),
        },
        {
          label: 'Close',
          accelerator: 'CmdOrCtrl+W',
          click: () => mainWindow?.hide(),
        },
      ],
    },
  ]

  return Menu.buildFromTemplate(template)
}

module.exports = { buildAppMenu }
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/electron/src/menu.js
git commit -m "feat(F036): add application menu with keyboard shortcuts"
```

---

### Task 8: AutoUpdater

**Files:**
- Create: `dashboard/electron/src/updater.js`

- [ ] **Step 1: 實作 updater**

```js
// src/updater.js
const { autoUpdater } = require('electron-updater')
const { Notification } = require('electron')

const CHECK_INTERVAL = 6 * 60 * 60 * 1000 // 6 hours

function setupAutoUpdater() {
  autoUpdater.autoDownload = true
  autoUpdater.autoInstallOnAppQuit = true

  autoUpdater.on('update-downloaded', (info) => {
    const notification = new Notification({
      title: '4x Live 更新就緒',
      body: `版本 ${info.version} 已下載完畢，重啟後自動套用。`,
    })
    notification.on('click', () => {
      autoUpdater.quitAndInstall()
    })
    notification.show()
  })

  autoUpdater.on('error', (err) => {
    console.error('Auto-update error:', err.message)
  })

  autoUpdater.checkForUpdates().catch(() => {})

  setInterval(() => {
    autoUpdater.checkForUpdates().catch(() => {})
  }, CHECK_INTERVAL)
}

module.exports = { setupAutoUpdater }
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/electron/src/updater.js
git commit -m "feat(F036): add auto-updater with GitHub Release provider"
```

---

### Task 9: 整合 main.js

**Files:**
- Modify: `dashboard/electron/src/main.js`

- [ ] **Step 1: 重寫 main.js 整合所有 manager**

```js
// src/main.js
const { app, BrowserWindow, dialog, ipcMain, Menu } = require('electron')
const path = require('path')
const windowState = require('electron-window-state')
const { ServerManager } = require('./server-manager')
const { TrayManager } = require('./tray-manager')
const { StatusPoller } = require('./status-poller')
const { NotificationManager } = require('./notification-manager')
const { buildAppMenu } = require('./menu')
const { updateBadge } = require('./badge')
const { setupAutoUpdater } = require('./updater')

let mainWindow = null
let serverManager = null
let trayManager = null
let statusPoller = null
let notificationManager = null

const PORT = parseInt(
  process.argv.find(a => a.startsWith('--port='))?.split('=')[1]
) || 4567

function createWindow() {
  const state = windowState({
    defaultWidth: 1200,
    defaultHeight: 800,
  })

  mainWindow = new BrowserWindow({
    x: state.x,
    y: state.y,
    width: state.width,
    height: state.height,
    title: '4x Live',
    show: false,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })

  state.manage(mainWindow)

  mainWindow.on('close', (e) => {
    if (!app.isQuitting) {
      e.preventDefault()
      mainWindow.hide()
    }
  })

  mainWindow.on('ready-to-show', () => {
    mainWindow.show()
  })

  return mainWindow
}

function showOrFocusWindow() {
  if (!mainWindow) return
  if (mainWindow.isMinimized()) mainWindow.restore()
  mainWindow.show()
  mainWindow.focus()
}

function setupTray() {
  trayManager = new TrayManager({
    assetsPath: path.join(__dirname, '..', 'assets'),
    onShowWindow: showOrFocusWindow,
    onQuit: () => {
      app.isQuitting = true
      app.quit()
    },
  })
  trayManager.create()
}

function setupStatusPoller() {
  statusPoller = new StatusPoller({ port: PORT })
  statusPoller.on('stats', (stats) => {
    trayManager?.update(stats)
    updateBadge(mainWindow, stats.running)
  })
  statusPoller.start()
}

function setupNotifications() {
  notificationManager = new NotificationManager({
    port: PORT,
    onClickFeature: (featureId) => {
      showOrFocusWindow()
      mainWindow?.webContents.executeJavaScript(
        `switchToFeature && switchToFeature('${featureId}')`
      )
    },
  })
  notificationManager.start()
}

function setupIpc() {
  ipcMain.handle('open-folder', async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openDirectory'],
      message: 'Select a 4x project folder',
    })
    if (result.canceled) return null
    return result.filePaths[0]
  })
}

app.whenReady().then(async () => {
  createWindow()

  const menu = buildAppMenu(mainWindow)
  Menu.setApplicationMenu(menu)

  setupIpc()
  setupTray()

  serverManager = new ServerManager({ port: PORT })

  serverManager.on('error', (err) => {
    dialog.showErrorBox('4x Live', err.message)
  })

  serverManager.on('restart', (count) => {
    console.log(`Server restarting (attempt ${count})...`)
  })

  const ok = await serverManager.start()
  if (!ok) {
    dialog.showErrorBox(
      '4x Live',
      '4x binary not found.\n\nInstall with:\n  go install github.com/ggwhite/4x/cmd/4x@latest'
    )
    app.quit()
    return
  }

  mainWindow.loadURL(`http://localhost:${PORT}`)
  setupStatusPoller()
  setupNotifications()
  setupAutoUpdater()
})

app.on('before-quit', async () => {
  app.isQuitting = true
  statusPoller?.stop()
  notificationManager?.stop()
  trayManager?.destroy()
  if (serverManager) {
    await serverManager.stop()
  }
})

app.on('window-all-closed', () => {
  // 不退出，靠 tray 保持 alive
})
```

- [ ] **Step 2: 手動驗證完整流程**

```bash
cd dashboard/electron && npm run dev
```

驗證清單：
1. 視窗開啟 → server 自動起來 → web UI 載入
2. System tray icon 出現
3. 右鍵 tray → 看到 context menu
4. 左鍵 tray → toggle 主視窗
5. 關閉視窗 → app 不退出（tray 仍在）
6. Tray → Quit → app 退出 → server 也停

- [ ] **Step 3: Commit**

```bash
git add dashboard/electron/src/main.js
git commit -m "feat(F036): integrate all managers in main entry point"
```

---

### Task 10: CI/CD 與 Release Workflow

**Files:**
- Create: `dashboard/electron/.github/workflows/release.yml`

- [ ] **Step 1: 建立 GitHub Actions workflow**

```yaml
# dashboard/electron/.github/workflows/release.yml
name: Release Electron App

on:
  push:
    tags:
      - 'electron-v*'

jobs:
  build:
    strategy:
      matrix:
        include:
          - os: windows-latest
            args: --win
          - os: ubuntu-latest
            args: --linux

    runs-on: ${{ matrix.os }}

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: 20

      - name: Install dependencies
        working-directory: dashboard/electron
        run: npm ci

      - name: Build and publish
        working-directory: dashboard/electron
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: npx electron-builder ${{ matrix.args }} --publish always
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/electron/.github/workflows/release.yml
git commit -m "ci(F036): add GitHub Actions release workflow for Electron app"
```

---

### Task 11: 跑完整測試並最終驗證

**Files:** 無新建

- [ ] **Step 1: 跑全部測試**

```bash
cd dashboard/electron && npx vitest run
```

預期：所有測試 PASS

- [ ] **Step 2: 確認 Go 專案測試不受影響**

```bash
cd /Users/white/github/4x && go test ./... && go vet ./...
```

預期：全部 PASS

- [ ] **Step 3: 試打包（不 publish）**

```bash
cd dashboard/electron && npx electron-builder --linux --dir
```

預期：`dist/linux-unpacked/` 產出，確認 binary 存在

- [ ] **Step 4: 最終 commit（如有遺漏修正）**

```bash
git add -A dashboard/electron/
git commit -m "chore(F036): final cleanup and verification"
```
