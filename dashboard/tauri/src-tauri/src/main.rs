// 4x Live — Windows/Linux 桌面殼。
//
// 功能：
//   1. 以 sidecar 方式 spawn `4x live --port=<PORT>`
//   2. 視窗載入 http://localhost:<PORT>
//   3. System tray icon（點擊顯示/隱藏視窗）
//   4. App menu（Settings / Reload / Quit）
//   5. 關窗隱藏不退出（從 tray 重開）
#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder},
    tray::TrayIconBuilder,
    Manager, WindowEvent,
};
use tauri_plugin_shell::ShellExt;

const PORT: u16 = 4567;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            let sidecar = app.shell().sidecar("4x")?;
            sidecar
                .args(["live", &format!("--port={PORT}")])
                .spawn()?;

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
            let quit = MenuItemBuilder::with_id("quit", "Quit 4x Live")
                .accelerator("CmdOrCtrl+Q")
                .build(app)?;

            let file_menu = SubmenuBuilder::new(app, "4x Live")
                .item(&settings)
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
                    if let tauri::tray::TrayIconEvent::Click { .. } = event {
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

            // Navigate to server
            if let Some(window) = app.get_webview_window("main") {
                let url = format!("http://localhost:{PORT}")
                    .parse()
                    .expect("valid localhost url");
                window.navigate(url)?;
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
        .run(tauri::generate_context!())
        .expect("error while running 4x Live");
}
