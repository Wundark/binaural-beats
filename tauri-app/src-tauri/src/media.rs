//! System media controls on the desktop: the session shows in the media
//! overlay or notification (MPRIS on Linux, the media flyout on Windows, Now
//! Playing on macOS), and its buttons and the keyboard media keys control
//! playback.
//!
//! The controls are created on the main thread and only used there; updates
//! are posted to it. Button presses call the engine directly, so they work
//! while the window is hidden, then tell the page to refresh.

use crate::{BackendState, PlaybackStatus};
use souvlaki::{
    MediaControlEvent, MediaControls, MediaMetadata, MediaPlayback, MediaPosition, PlatformConfig,
    SeekDirection,
};
use std::cell::RefCell;
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter, Manager};

/// How far the seek forward and back buttons move, like the page's +10s and -10s.
const SEEK_STEP: f64 = 10.0;

/// How often to refresh the position while playing, in seconds.
const POSITION_REFRESH: f64 = 5.0;

thread_local! {
    static CONTROLS: RefCell<Option<MediaControls>> = const { RefCell::new(None) };
}

/// What the controls last showed, to skip updates that change nothing.
#[derive(Clone, PartialEq)]
struct Shown {
    title: String,
    duration: u64,
    state: u8,
    position: f64,
    at: Instant,
}

static SHOWN: Mutex<Option<Shown>> = Mutex::new(None);

/// Sets up the controls. Must run on the main thread (as `setup` does).
pub fn init(app: &tauri::App) {
    #[cfg(target_os = "windows")]
    let hwnd = match app.get_webview_window("main").map(|w| w.hwnd()) {
        Some(Ok(hwnd)) => Some(hwnd.0 as *mut std::ffi::c_void),
        _ => return,
    };
    #[cfg(not(target_os = "windows"))]
    let hwnd = None;

    let config = PlatformConfig {
        display_name: "Binaural Beats",
        dbus_name: "binaural_beats",
        hwnd,
    };
    let mut controls = match MediaControls::new(config) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Media controls unavailable: {:?}", e);
            return;
        }
    };
    let handle = app.handle().clone();
    let attached = controls.attach(move |event| {
        let app = handle.clone();
        tauri::async_runtime::spawn(async move { handle_event(app, event).await });
    });
    if let Err(e) = attached {
        eprintln!("Media controls unavailable: {:?}", e);
        return;
    }
    let _ = controls.set_playback(MediaPlayback::Stopped);
    CONTROLS.with(|c| *c.borrow_mut() = Some(controls));
}

/// Shows `status` in the controls.
pub fn update(app: &AppHandle, status: &PlaybackStatus) {
    let state = if !status.is_playing {
        0
    } else if status.is_paused {
        1
    } else {
        2
    };
    let next = Shown {
        title: if status.name.is_empty() {
            "Binaural Beats".to_string()
        } else {
            status.name.clone()
        },
        duration: status.total_duration.round() as u64,
        state,
        position: status.time,
        at: Instant::now(),
    };

    let mut shown = SHOWN.lock().unwrap_or_else(|e| e.into_inner());
    let (metadata_changed, playback_changed) = match shown.as_ref() {
        None => (true, true),
        Some(prev) => {
            // Most systems extrapolate the position while playing, so a seek
            // needs telling; MPRIS (souvlaki) reports it as last set, so
            // refresh it every few seconds too.
            let since = prev.at.elapsed().as_secs_f64();
            let expected = if prev.state == 2 {
                prev.position + since
            } else {
                prev.position
            };
            let metadata = prev.title != next.title || prev.duration != next.duration;
            let playback = metadata
                || prev.state != next.state
                || (next.position - expected).abs() > 1.5
                || (next.state == 2 && since > POSITION_REFRESH);
            (metadata, playback)
        }
    };
    if !metadata_changed && !playback_changed {
        return;
    }
    *shown = Some(next.clone());
    drop(shown);

    let loaded = status.config_loaded;
    let _ = app.run_on_main_thread(move || {
        CONTROLS.with(|c| {
            let mut c = c.borrow_mut();
            let Some(controls) = c.as_mut() else { return };
            if metadata_changed {
                let _ = controls.set_metadata(if loaded {
                    MediaMetadata {
                        title: Some(&next.title),
                        artist: Some("Binaural Beats"),
                        duration: Some(Duration::from_secs(next.duration)),
                        ..Default::default()
                    }
                } else {
                    MediaMetadata::default()
                });
            }
            if playback_changed {
                let progress = Some(MediaPosition(Duration::from_secs_f64(
                    next.position.max(0.0),
                )));
                let _ = controls.set_playback(match next.state {
                    0 => MediaPlayback::Stopped,
                    1 => MediaPlayback::Paused { progress },
                    _ => MediaPlayback::Playing { progress },
                });
            }
        });
    });
}

async fn handle_event(app: AppHandle, event: MediaControlEvent) {
    if event == MediaControlEvent::Raise {
        if let Some(window) = app.get_webview_window("main") {
            let _ = window.unminimize();
            let _ = window.show();
            let _ = window.set_focus();
        }
        return;
    }

    let state = app.state::<BackendState>().inner().clone();
    let mut backend = state.lock().await;
    let Ok(status) = backend.status().await else {
        return;
    };
    let playing = status.is_playing && !status.is_paused;
    let seek = |t: f64| Some(("seek", Some(serde_json::json!({ "time": t.max(0.0) }))));
    let call = match event {
        MediaControlEvent::Play if !status.is_playing && status.config_loaded => Some(("play", None)),
        MediaControlEvent::Play if status.is_paused => Some(("resume", None)),
        MediaControlEvent::Pause if playing => Some(("pause", None)),
        MediaControlEvent::Toggle if playing => Some(("pause", None)),
        MediaControlEvent::Toggle if status.is_paused => Some(("resume", None)),
        MediaControlEvent::Toggle if status.config_loaded => Some(("play", None)),
        MediaControlEvent::Stop if status.is_playing => Some(("stop", None)),
        MediaControlEvent::Next => Some(("next", None)),
        MediaControlEvent::Previous => Some(("previous", None)),
        MediaControlEvent::Seek(dir) => seek(status.time + step(dir, SEEK_STEP)),
        MediaControlEvent::SeekBy(dir, by) => seek(status.time + step(dir, by.as_secs_f64())),
        MediaControlEvent::SetPosition(MediaPosition(at)) => seek(at.as_secs_f64()),
        _ => None,
    };
    let Some((method, params)) = call else { return };
    if let Err(e) = backend.call(method, params).await {
        eprintln!("Media control {}: {}", method, e);
    }
    if let Ok(status) = backend.status().await {
        update(&app, &status);
    }
    drop(backend);
    // The page polls only while playing; have it catch up now.
    let _ = app.emit("media-control", ());
}

fn step(dir: SeekDirection, by: f64) -> f64 {
    match dir {
        SeekDirection::Forward => by,
        SeekDirection::Backward => -by,
    }
}
