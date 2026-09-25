use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tauri::Manager;
use tokio::sync::Mutex;

// ─── FFI bindings for the Go shared library (Android) ───

#[cfg(target_os = "android")]
mod ffi {
    use std::ffi::{CStr, CString};
    use std::os::raw::c_char;

    extern "C" {
        pub fn BinauralProcessRPC(input: *const c_char) -> *mut c_char;
        pub fn BinauralFreeString(s: *mut c_char);
    }

    pub fn call_rpc(request: &str) -> Result<String, String> {
        let c_request = CString::new(request).map_err(|e| e.to_string())?;
        unsafe {
            let c_response = BinauralProcessRPC(c_request.as_ptr());
            if c_response.is_null() {
                return Err("Null response from engine".to_string());
            }
            let response = CStr::from_ptr(c_response).to_string_lossy().into_owned();
            BinauralFreeString(c_response);
            Ok(response)
        }
    }
}

// ─── Sidecar process (Desktop: Windows, macOS, Linux) ───

#[cfg(not(target_os = "android"))]
mod sidecar {
    use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
    use tokio::process::{Child, ChildStdin, ChildStdout};

    pub struct Process {
        #[allow(dead_code)]
        pub child: Child,
        pub stdin: ChildStdin,
        pub stdout: BufReader<ChildStdout>,
    }

    impl Process {
        pub async fn call_rpc(&mut self, request: &str) -> Result<String, String> {
            let mut data = request.as_bytes().to_vec();
            data.push(b'\n');

            self.stdin
                .write_all(&data)
                .await
                .map_err(|e| format!("Write to sidecar failed: {}", e))?;
            self.stdin
                .flush()
                .await
                .map_err(|e| format!("Flush sidecar stdin failed: {}", e))?;

            let mut line = String::new();
            let n = self
                .stdout
                .read_line(&mut line)
                .await
                .map_err(|e| format!("Read from sidecar failed: {}", e))?;
            if n == 0 {
                return Err("Audio engine exited unexpectedly".to_string());
            }
            Ok(line)
        }
    }
}

// ─── Shared types ───

#[derive(Serialize)]
struct RpcRequest {
    jsonrpc: &'static str,
    method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
    id: u64,
}

#[derive(Deserialize, Debug)]
struct RpcResponse {
    #[allow(dead_code)]
    jsonrpc: String,
    result: Option<serde_json::Value>,
    error: Option<RpcError>,
    #[allow(dead_code)]
    id: Option<u64>,
}

#[derive(Deserialize, Debug)]
struct RpcError {
    #[allow(dead_code)]
    code: i64,
    message: String,
}

#[derive(Serialize, Deserialize, Clone)]
struct PlaybackStatus {
    time: f64,
    frequency: f64,
    beat_frequency: f64,
    tone_volume: f64,
    pink_noise_volume: f64,
    total_duration: f64,
    is_playing: bool,
    is_paused: bool,
    volume: f64,
    config_loaded: bool,
}

// ─── Backend abstraction ───

struct Backend {
    #[cfg(not(target_os = "android"))]
    sidecar: Option<sidecar::Process>,
    /// Why the sidecar could not be started, reported to the UI on each call.
    #[cfg(not(target_os = "android"))]
    sidecar_error: Option<String>,
    next_id: u64,
}

type BackendState = Arc<Mutex<Backend>>;

impl Backend {
    fn new() -> Self {
        Backend {
            #[cfg(not(target_os = "android"))]
            sidecar: None,
            #[cfg(not(target_os = "android"))]
            sidecar_error: None,
            next_id: 1,
        }
    }

    async fn call(
        &mut self,
        method: &str,
        params: Option<serde_json::Value>,
    ) -> Result<serde_json::Value, String> {
        let req = RpcRequest {
            jsonrpc: "2.0",
            method: method.to_string(),
            params,
            id: self.next_id,
        };
        self.next_id += 1;

        let request_json = serde_json::to_string(&req).map_err(|e| e.to_string())?;

        let response_json = self.send_request(&request_json).await?;

        let resp: RpcResponse =
            serde_json::from_str(&response_json).map_err(|e| format!("Invalid response: {}", e))?;

        if let Some(err) = resp.error {
            return Err(err.message);
        }

        Ok(resp.result.unwrap_or(serde_json::Value::Null))
    }

    #[cfg(target_os = "android")]
    async fn send_request(&mut self, request_json: &str) -> Result<String, String> {
        ffi::call_rpc(request_json)
    }

    #[cfg(not(target_os = "android"))]
    async fn send_request(&mut self, request_json: &str) -> Result<String, String> {
        match self.sidecar.as_mut() {
            Some(proc) => proc.call_rpc(request_json).await,
            None => Err(self
                .sidecar_error
                .clone()
                .unwrap_or_else(|| "Audio engine is still starting".to_string())),
        }
    }
}

// ─── Tauri commands ───

#[tauri::command]
async fn load_config(
    app: tauri::AppHandle,
    state: tauri::State<'_, BackendState>,
    path: String,
) -> Result<serde_json::Value, String> {
    let path = engine_readable_path(&app, path)?;
    let mut guard = state.lock().await;
    guard
        .call("load_config", Some(serde_json::json!({ "path": path })))
        .await
}

#[tauri::command]
async fn list_presets(state: tauri::State<'_, BackendState>) -> Result<serde_json::Value, String> {
    state.lock().await.call("list_presets", None).await
}

#[tauri::command]
async fn load_preset(
    state: tauri::State<'_, BackendState>,
    id: String,
) -> Result<serde_json::Value, String> {
    state
        .lock()
        .await
        .call("load_preset", Some(serde_json::json!({ "id": id })))
        .await
}

#[tauri::command]
async fn play(state: tauri::State<'_, BackendState>) -> Result<String, String> {
    let mut guard = state.lock().await;
    guard.call("play", None).await?;
    Ok("Playing".to_string())
}

#[tauri::command]
async fn stop(state: tauri::State<'_, BackendState>) -> Result<String, String> {
    let mut guard = state.lock().await;
    guard.call("stop", None).await?;
    Ok("Stopped".to_string())
}

#[tauri::command]
async fn pause(state: tauri::State<'_, BackendState>) -> Result<(), String> {
    state.lock().await.call("pause", None).await.map(|_| ())
}

#[tauri::command]
async fn resume(state: tauri::State<'_, BackendState>) -> Result<(), String> {
    state.lock().await.call("resume", None).await.map(|_| ())
}

#[tauri::command]
async fn seek(state: tauri::State<'_, BackendState>, time: f64) -> Result<(), String> {
    state
        .lock()
        .await
        .call("seek", Some(serde_json::json!({ "time": time })))
        .await
        .map(|_| ())
}

#[tauri::command]
async fn set_volume(state: tauri::State<'_, BackendState>, volume: f64) -> Result<(), String> {
    state
        .lock()
        .await
        .call("set_volume", Some(serde_json::json!({ "volume": volume })))
        .await
        .map(|_| ())
}

#[tauri::command]
async fn get_status(state: tauri::State<'_, BackendState>) -> Result<PlaybackStatus, String> {
    let mut guard = state.lock().await;
    let result = guard.call("get_status", None).await?;
    serde_json::from_value(result).map_err(|e| format!("Failed to parse status: {}", e))
}

#[tauri::command]
async fn export_wav(
    app: tauri::AppHandle,
    state: tauri::State<'_, BackendState>,
    path: String,
) -> Result<String, String> {
    let engine_path = engine_writable_path(&app, &path)?;
    let mut guard = state.lock().await;
    guard
        .call(
            "export_wav",
            Some(serde_json::json!({ "path": engine_path })),
        )
        .await?;
    drop(guard);
    publish_export(&app, &engine_path, &path)?;
    Ok("Export complete".to_string())
}

// ─── File access ───
//
// On Android the dialog plugin returns content:// URIs, which the Go engine
// cannot open. Stage files through the app cache directory, using the fs
// plugin to read from and write to the URIs.

#[cfg(target_os = "android")]
fn cache_file(app: &tauri::AppHandle, name: &str) -> Result<String, String> {
    let dir = app.path().app_cache_dir().map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&dir).map_err(|e| format!("Failed to create cache dir: {}", e))?;
    Ok(dir.join(name).to_string_lossy().into_owned())
}

#[cfg(target_os = "android")]
fn engine_readable_path(app: &tauri::AppHandle, path: String) -> Result<String, String> {
    use std::str::FromStr;
    use tauri_plugin_fs::{FilePath, FsExt};

    let source = FilePath::from_str(&path).map_err(|e| e.to_string())?;
    let data = app
        .fs()
        .read(source)
        .map_err(|e| format!("Failed to read config: {}", e))?;
    // No extension: the engine detects YAML or SBaGen from the content.
    let staged = cache_file(app, "session")?;
    std::fs::write(&staged, data).map_err(|e| format!("Failed to stage config: {}", e))?;
    Ok(staged)
}

#[cfg(not(target_os = "android"))]
fn engine_readable_path(_app: &tauri::AppHandle, path: String) -> Result<String, String> {
    Ok(path)
}

#[cfg(target_os = "android")]
fn engine_writable_path(app: &tauri::AppHandle, _path: &str) -> Result<String, String> {
    cache_file(app, "export.wav")
}

#[cfg(not(target_os = "android"))]
fn engine_writable_path(_app: &tauri::AppHandle, path: &str) -> Result<String, String> {
    Ok(path.to_string())
}

#[cfg(target_os = "android")]
fn publish_export(app: &tauri::AppHandle, staged: &str, dest: &str) -> Result<(), String> {
    use std::str::FromStr;
    use tauri_plugin_fs::{FilePath, FsExt, OpenOptions};

    let result = (|| -> std::io::Result<()> {
        let mut src = std::fs::File::open(staged)?;
        let mut opts = OpenOptions::new();
        opts.read(false).write(true).truncate(true).create(true);
        let target = FilePath::from_str(dest).map_err(std::io::Error::other)?;
        let mut out = app.fs().open(target, opts)?;
        std::io::copy(&mut src, &mut out)?;
        Ok(())
    })();
    let _ = std::fs::remove_file(staged);
    result.map_err(|e| format!("Failed to save WAV: {}", e))
}

#[cfg(not(target_os = "android"))]
fn publish_export(_app: &tauri::AppHandle, _staged: &str, _dest: &str) -> Result<(), String> {
    Ok(())
}

#[tauri::command]
async fn set_stretch(state: tauri::State<'_, BackendState>, factor: f64) -> Result<String, String> {
    let mut guard = state.lock().await;
    guard
        .call("set_stretch", Some(serde_json::json!({ "factor": factor })))
        .await?;
    Ok(format!("Stretch set to {:.1}x", factor))
}

// ─── App setup ───

/// Name of the Go engine binary bundled via `bundle.externalBin`. Tauri
/// installs it next to the app executable, without the target-triple suffix.
#[cfg(not(target_os = "android"))]
const SIDECAR_NAME: &str = "binaural-engine";

#[cfg(not(target_os = "android"))]
fn sidecar_path() -> Result<std::path::PathBuf, String> {
    let exe =
        std::env::current_exe().map_err(|e| format!("Cannot locate app executable: {}", e))?;
    let dir = exe
        .parent()
        .ok_or("App executable has no parent directory")?;
    Ok(dir.join(format!("{}{}", SIDECAR_NAME, std::env::consts::EXE_SUFFIX)))
}

#[cfg(not(target_os = "android"))]
async fn spawn_sidecar() -> Result<sidecar::Process, String> {
    use tokio::io::BufReader;

    let path = sidecar_path()?;
    let mut child = tokio::process::Command::new(&path)
        .arg("-rpc")
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::inherit())
        .kill_on_drop(true)
        .spawn()
        .map_err(|e| format!("Failed to start audio engine at {}: {}", path.display(), e))?;

    let stdin = child.stdin.take().ok_or("Audio engine has no stdin")?;
    let stdout = child.stdout.take().ok_or("Audio engine has no stdout")?;

    Ok(sidecar::Process {
        child,
        stdin,
        stdout: BufReader::new(stdout),
    })
}

#[cfg(not(target_os = "android"))]
fn setup_desktop(backend_state: BackendState) {
    tauri::async_runtime::spawn(async move {
        let result = spawn_sidecar().await;
        let mut guard = backend_state.lock().await;
        match result {
            Ok(process) => guard.sidecar = Some(process),
            Err(e) => {
                eprintln!("{}", e);
                guard.sidecar_error = Some(e);
            }
        }
    });
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let backend_state: BackendState = Arc::new(Mutex::new(Backend::new()));

    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        .setup({
            let state = backend_state.clone();
            move |app| {
                app.manage(state.clone());

                #[cfg(not(target_os = "android"))]
                setup_desktop(state);

                Ok(())
            }
        })
        .invoke_handler(tauri::generate_handler![
            load_config,
            list_presets,
            load_preset,
            play,
            stop,
            pause,
            resume,
            seek,
            set_volume,
            get_status,
            export_wav,
            set_stretch,
        ])
        .run(tauri::generate_context!())
        .expect("Error running Tauri application");
}
