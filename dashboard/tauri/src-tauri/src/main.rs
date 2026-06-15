// 4x Live — Windows/Linux 桌面殼。
//
// 本殼僅做兩件事，不含任何業務邏輯：
//   1. 以 sidecar 方式 spawn 內嵌的 `4x live --port=<PORT>`，提供 dashboard server。
//   2. 將視窗導向 http://localhost:<PORT>，載入與 macOS 殼相同的前端（dashboard/web）。
#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

use tauri::Manager;
use tauri_plugin_shell::ShellExt;

const PORT: u16 = 4567;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            // 1. spawn sidecar：4x live --port=<PORT>
            let sidecar = app.shell().sidecar("4x")?;
            sidecar
                .args(["live", &format!("--port={PORT}")])
                .spawn()?;

            // 2. 視窗載入內嵌 server 的 URL（與 macOS WKWebView 行為一致）
            if let Some(window) = app.get_webview_window("main") {
                let url = format!("http://localhost:{PORT}")
                    .parse()
                    .expect("valid localhost url");
                window.navigate(url)?;
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running 4x Live");
}
