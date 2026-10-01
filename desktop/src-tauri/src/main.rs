// Minimal Tauri host. The stage holds no business logic: it is a window onto
// Yui Core (ADR-007). Native integrations (overlay, global hotkey, tray) are
// added here later without touching the web layer.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::Serialize;

#[derive(Serialize)]
struct CoreEndpoint {
    base: String,
    token: String,
}

/// Reads the core endpoint from the environment the launcher sets, so the
/// loopback token never has to be pasted by hand.
#[tauri::command]
fn core_endpoint() -> CoreEndpoint {
    CoreEndpoint {
        base: std::env::var("YUI_CORE_URL").unwrap_or_else(|_| "http://127.0.0.1:8765".into()),
        token: std::env::var("YUI_LOOPBACK_TOKEN").unwrap_or_default(),
    }
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![core_endpoint])
        .run(tauri::generate_context!())
        .expect("failed to start the Yui stage");
}
