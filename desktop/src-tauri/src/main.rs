// Tauri host. The stage holds no business logic: it is a window onto Yui Core
// (ADR-007). Native pieces live here: the tray, global hotkeys and the
// click-through mode of the detached avatar window.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;

use serde::Serialize;
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager, Runtime, WebviewUrl, WebviewWindowBuilder, WindowEvent, Wry};
use tauri_plugin_global_shortcut::{Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutState};

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
        base: std::env::var("YUI_CORE_URL").unwrap_or_else(|_| "http://127.0.0.1:8766".into()),
        token: std::env::var("YUI_LOOPBACK_TOKEN").unwrap_or_default(),
    }
}

/// Tray items kept so their labels can follow the interface language.
struct Tray {
    show: MenuItem<Wry>,
    avatar: MenuItem<Wry>,
    through: CheckMenuItem<Wry>,
    mic: MenuItem<Wry>,
    quit: MenuItem<Wry>,
}

struct State {
    click_through: AtomicBool,
    tray: Mutex<Option<Tray>>,
}

const AVATAR: &str = "avatar";

fn labels(lang: &str) -> [&'static str; 5] {
    if lang == "en" {
        ["Show Yui", "Show / hide avatar", "Click through the avatar", "Microphone", "Quit"]
    } else {
        ["Показать Yui", "Показать / скрыть аватар", "Клики сквозь аватар", "Микрофон", "Выход"]
    }
}

#[tauri::command]
fn set_tray_language(app: AppHandle, lang: String) {
    let state = app.state::<State>();
    let tray = state.tray.lock().unwrap();
    if let Some(tray) = tray.as_ref() {
        let [show, avatar, through, mic, quit] = labels(&lang);
        let _ = tray.show.set_text(show);
        let _ = tray.avatar.set_text(avatar);
        let _ = tray.through.set_text(through);
        let _ = tray.mic.set_text(mic);
        let _ = tray.quit.set_text(quit);
    }
}

/// Clicks pass through the transparent avatar window to the desktop below.
/// The window cannot be clicked while this is on, so the tray and
/// Ctrl+Shift+Y are the ways back.
#[tauri::command]
fn set_click_through(app: AppHandle, enabled: bool) {
    apply_click_through(&app, enabled);
}

#[tauri::command]
fn click_through(app: AppHandle) -> bool {
    app.state::<State>().click_through.load(Ordering::SeqCst)
}

fn apply_click_through<R: Runtime>(app: &AppHandle<R>, enabled: bool) {
    let enabled = enabled && app.get_webview_window(AVATAR).is_some();
    if let Some(window) = app.get_webview_window(AVATAR) {
        let _ = window.set_ignore_cursor_events(enabled);
    }
    let state = app.state::<State>();
    state.click_through.store(enabled, Ordering::SeqCst);
    if let Some(tray) = state.tray.lock().unwrap().as_ref() {
        let _ = tray.through.set_checked(enabled);
    }
    let _ = app.emit("yui://click-through", enabled);
}

fn toggle_avatar<R: Runtime>(app: &AppHandle<R>) {
    if let Some(window) = app.get_webview_window(AVATAR) {
        let _ = window.close();
        apply_click_through(app, false);
        return;
    }
    let built = WebviewWindowBuilder::new(app, AVATAR, WebviewUrl::App("avatar.html?view=avatar".into()))
        .title("Yui")
        .inner_size(420.0, 640.0)
        .min_inner_size(240.0, 300.0)
        .transparent(true)
        .decorations(false)
        .shadow(false)
        .always_on_top(true)
        .build();
    if let Err(error) = built {
        eprintln!("cannot open the avatar window: {error}");
    }
}

fn show_main<R: Runtime>(app: &AppHandle<R>) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

fn shortcut(code: Code) -> Shortcut {
    Shortcut::new(Some(Modifiers::CONTROL | Modifiers::SHIFT), code)
}

fn main() {
    let ptt = shortcut(Code::Space);
    let avatar = shortcut(Code::KeyA);
    let through = shortcut(Code::KeyY);

    tauri::Builder::default()
        .manage(State { click_through: AtomicBool::new(false), tray: Mutex::new(None) })
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(move |app, pressed, event| {
                    if event.state() != ShortcutState::Pressed {
                        return;
                    }
                    if pressed == &ptt {
                        // The main window owns the conversation socket and mic.
                        let _ = app.emit_to("main", "yui://ptt", ());
                    } else if pressed == &avatar {
                        toggle_avatar(app);
                    } else if pressed == &through {
                        let on = app.state::<State>().click_through.load(Ordering::SeqCst);
                        apply_click_through(app, !on);
                    }
                })
                .build(),
        )
        .invoke_handler(tauri::generate_handler![core_endpoint, set_tray_language, set_click_through, click_through])
        .setup(move |app| {
            // Another program may own a combination; the app still works.
            for key in [ptt, avatar, through] {
                if let Err(error) = app.global_shortcut().register(key) {
                    eprintln!("global shortcut {key} unavailable: {error}");
                }
            }

            let [show_label, avatar_label, through_label, mic_label, quit_label] = labels("ru");
            let show = MenuItem::with_id(app, "show", show_label, true, None::<&str>)?;
            let avatar_item = MenuItem::with_id(app, "avatar", avatar_label, true, Some("Ctrl+Shift+A"))?;
            let through_item = CheckMenuItem::with_id(app, "through", through_label, true, false, Some("Ctrl+Shift+Y"))?;
            let mic = MenuItem::with_id(app, "mic", mic_label, true, Some("Ctrl+Shift+Space"))?;
            let quit = MenuItem::with_id(app, "quit", quit_label, true, None::<&str>)?;
            let separator = PredefinedMenuItem::separator(app)?;
            let menu = Menu::with_items(app, &[&show, &avatar_item, &through_item, &mic, &separator, &quit])?;

            let mut tray = TrayIconBuilder::with_id("yui")
                .tooltip("Yui")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "show" => show_main(app),
                    "avatar" => toggle_avatar(app),
                    "through" => {
                        let on = app.state::<State>().click_through.load(Ordering::SeqCst);
                        apply_click_through(app, !on);
                    }
                    "mic" => {
                        let _ = app.emit_to("main", "yui://ptt", ());
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. } = event {
                        show_main(tray.app_handle());
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            tray.build(app)?;
            *app.state::<State>().tray.lock().unwrap() =
                Some(Tray { show, avatar: avatar_item, through: through_item, mic, quit });
            Ok(())
        })
        .on_window_event(|window, event| {
            let app = window.app_handle();
            match event {
                // With the avatar on the desktop, closing the main window
                // hides it to the tray instead of ending the session.
                WindowEvent::CloseRequested { api, .. }
                    if window.label() == "main" && app.get_webview_window(AVATAR).is_some() =>
                {
                    api.prevent_close();
                    let _ = window.hide();
                }
                WindowEvent::Destroyed if window.label() == AVATAR => {
                    let state = app.state::<State>();
                    state.click_through.store(false, Ordering::SeqCst);
                    if let Some(tray) = state.tray.lock().unwrap().as_ref() {
                        let _ = tray.through.set_checked(false);
                    }
                    // Nothing left on screen: a hidden main window comes back.
                    if let Some(main) = app.get_webview_window("main") {
                        if !main.is_visible().unwrap_or(true) {
                            let _ = main.show();
                        }
                    }
                }
                _ => {}
            }
        })
        .run(tauri::generate_context!())
        .expect("failed to start the Yui stage");
}
