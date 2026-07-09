// 4x Live — Windows/Linux 桌面殼。
//
// 功能：
//   1. 以 sidecar 方式 spawn `4x live --port=<PORT>`
//   2. 視窗載入 http://localhost:<PORT>
//   3. System tray icon（點擊顯示/隱藏視窗）
//   4. App menu（Settings / Reload / Quit）
//   5. 關窗隱藏不退出（從 tray 重開）
//   6. 單例保護：重複開啟時聚焦既有視窗
#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

use std::net::TcpListener;
use std::time::{Duration, Instant};
use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder},
    tray::TrayIconBuilder,
    Manager, WindowEvent,
};
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_shell::process::CommandChild;
use tauri_plugin_shell::ShellExt;

struct SidecarChild(std::sync::Mutex<Option<CommandChild>>);

// 預設值須與 internal/server.DefaultPort（單一事實來源）保持一致，
// 由 internal/server/port_sync_test.go 自動驗證。
const DEFAULT_PORT: u16 = 4567;

/// notify 顯示一則原生系統通知，供前端在 Tauri 環境下推播 run 結束事件。
/// 失敗時回傳錯誤字串，前端可忽略（優雅降級）。
#[tauri::command]
fn notify(app: tauri::AppHandle, title: String, body: String) -> Result<(), String> {
    app.notification()
        .builder()
        .title(title)
        .body(body)
        .show()
        .map_err(|e| e.to_string())
}

/// home_dir 跨平台解析使用者 home 目錄，與 Go server 端 `os.UserHomeDir()` 的落點對齊：
/// Windows 上與 Go 相同優先 `USERPROFILE`（缺時退回 `HOMEDRIVE` + `HOMEPATH` 組合）、
/// 不採用 `HOME`；非 Windows 只認 `HOME`。皆缺時回 None。
fn home_dir() -> Option<std::path::PathBuf> {
    if cfg!(windows) {
        if let Some(profile) = std::env::var_os("USERPROFILE") {
            if !profile.is_empty() {
                return Some(std::path::PathBuf::from(profile));
            }
        }
        let drive = std::env::var_os("HOMEDRIVE")?;
        let path = std::env::var_os("HOMEPATH")?;
        if drive.is_empty() || path.is_empty() {
            return None;
        }
        let mut combined = std::ffi::OsString::from(drive);
        combined.push(path);
        return Some(std::path::PathBuf::from(combined));
    }
    let home = std::env::var_os("HOME")?;
    if home.is_empty() {
        return None;
    }
    Some(std::path::PathBuf::from(home))
}

/// read_live_token 讀取 ~/.4x/live-token（4x live session 的 bearer token，權限 0600），
/// trim 換行後回傳。讀不到（auth 停用或尚未落地）或內容為空時回 None。
fn read_live_token() -> Option<String> {
    let home = home_dir()?;
    let path = home.join(".4x").join("live-token");
    let raw = std::fs::read_to_string(path).ok()?;
    let trimmed = raw.trim().to_string();
    if trimmed.is_empty() {
        None
    } else {
        Some(trimmed)
    }
}

fn find_available_port() -> u16 {
    if TcpListener::bind(("127.0.0.1", DEFAULT_PORT)).is_ok() {
        return DEFAULT_PORT;
    }
    TcpListener::bind(("127.0.0.1", 0))
        .map(|l| l.local_addr().unwrap().port())
        .unwrap_or(DEFAULT_PORT)
}

fn wait_for_server(port: u16, timeout: Duration) -> bool {
    let start = Instant::now();
    while start.elapsed() < timeout {
        if TcpListener::bind(("127.0.0.1", port)).is_err() {
            return true;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    false
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_notification::init())
        .invoke_handler(tauri::generate_handler![notify])
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(w) = app.get_webview_window("main") {
                let _ = w.show();
                let _ = w.unminimize();
                let _ = w.set_focus();
            }
        }))
        .setup(|app| {
            let port = find_available_port();

            let sidecar = app.shell().sidecar("4x")?;
            let (_, child) = sidecar
                .args(["live", &format!("--port={port}")])
                .spawn()?;
            app.manage(SidecarChild(std::sync::Mutex::new(Some(child))));

            // Menu
            let settings = MenuItemBuilder::with_id("settings", "Settings")
                .accelerator("CmdOrCtrl+,")
                .build(app)?;
            let reload = MenuItemBuilder::with_id("reload", "Reload")
                .accelerator("CmdOrCtrl+R")
                .build(app)?;
            let search = MenuItemBuilder::with_id("search", "Search")
                .accelerator("CmdOrCtrl+K")
                .build(app)?;
            let shortcuts = MenuItemBuilder::with_id("shortcuts", "Keyboard Shortcuts")
                .accelerator("CmdOrCtrl+/")
                .build(app)?;
            let check_update = MenuItemBuilder::with_id("check-update", "Check for Updates...")
                .build(app)?;
            let quit = MenuItemBuilder::with_id("quit", "Quit 4x Live")
                .accelerator("CmdOrCtrl+Q")
                .build(app)?;

            let file_menu = SubmenuBuilder::new(app, "4x Live")
                .item(&settings)
                .item(&check_update)
                .separator()
                .item(&quit)
                .build()?;
            let view_menu = SubmenuBuilder::new(app, "View")
                .item(&reload)
                .separator()
                .item(&search)
                .item(&shortcuts)
                .build()?;
            let edit_menu = SubmenuBuilder::new(app, "Edit")
                .items(&[
                    &PredefinedMenuItem::undo(app, None)?,
                    &PredefinedMenuItem::redo(app, None)?,
                    &PredefinedMenuItem::separator(app)?,
                    &PredefinedMenuItem::cut(app, None)?,
                    &PredefinedMenuItem::copy(app, None)?,
                    &PredefinedMenuItem::paste(app, None)?,
                    &PredefinedMenuItem::select_all(app, None)?,
                ])
                .build()?;

            let menu = MenuBuilder::new(app)
                .item(&file_menu)
                .item(&edit_menu)
                .item(&view_menu)
                .build()?;
            app.set_menu(menu)?;

            // System tray
            let tray_icon = Image::from_bytes(include_bytes!("../icons/tray-icon.png"))
                .expect("tray icon");

            let show = MenuItemBuilder::with_id("tray-show", "Show 4x Live").build(app)?;
            let tray_quit = MenuItemBuilder::with_id("tray-quit", "Quit").build(app)?;
            let tray_menu = MenuBuilder::new(app)
                .item(&show)
                .separator()
                .item(&tray_quit)
                .build()?;

            TrayIconBuilder::new()
                .icon(tray_icon)
                .tooltip("4x Live")
                .menu(&tray_menu)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "tray-show" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.set_focus();
                        }
                    }
                    "tray-quit" => {
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let tauri::tray::TrayIconEvent::Click { button_state, .. } = event {
                        if button_state != tauri::tray::MouseButtonState::Up {
                            return;
                        }
                        if let Some(w) = tray.app_handle().get_webview_window("main") {
                            if w.is_visible().unwrap_or(false) {
                                let _ = w.hide();
                            } else {
                                let _ = w.show();
                                let _ = w.set_focus();
                            }
                        }
                    }
                })
                .build(app)?;

            // Navigate after server is ready
            if let Some(window) = app.get_webview_window("main") {
                std::thread::spawn(move || {
                    wait_for_server(port, Duration::from_secs(15));
                    // 4x live 在 bind port 之前寫入 token 檔（~/.4x/live-token），故 wait_for_server
                    // 偵測到 port 被占用時，token 檔（若 auth 啟用）必已落地——「port 被占用」
                    // happens-after「token 已落地」，port-readiness 即 token-readiness 的有效代理。
                    // 讀得到就帶 ?token=；讀不到代表 auth 停用（未產 token），維持無 token URL。
                    let url_str = match read_live_token() {
                        Some(token) => format!("http://localhost:{port}/?token={token}"),
                        None => format!("http://localhost:{port}"),
                    };
                    let url = url_str.parse().expect("valid localhost url");
                    let _ = window.navigate(url);
                });
            }

            Ok(())
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "settings" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.eval("activeProjectId?openProjectSettings():openGlobalSettings()");
                }
            }
            "reload" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.eval("location.reload()");
                }
            }
            "search" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.eval("openSearch()");
                }
            }
            "shortcuts" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.eval("showShortcutsHelp('shortcuts')");
                }
            }
            "check-update" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.eval("typeof checkForUpdates==='function'&&checkForUpdates()");
                }
            }
            "quit" => {
                app.exit(0);
            }
            _ => {}
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building 4x Live")
        .run(|app, event| {
            if let tauri::RunEvent::ExitRequested { .. } = event {
                if let Some(state) = app.try_state::<SidecarChild>() {
                    if let Ok(mut guard) = state.0.lock() {
                        if let Some(child) = guard.take() {
                            let _ = child.kill();
                        }
                    }
                }
            }
        });
}
