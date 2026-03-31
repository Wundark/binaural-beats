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
            let response = CStr::from_ptr(c_response)
                .to_string_lossy()
                .into_owned();
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
            self.stdout
                .read_line(&mut line)
                .await
                .map_err(|e| format!("Read from sidecar failed: {}", e))?;
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
    config_loaded: bool,
}

// ─── Backend abstraction ───

struct Backend {
    #[cfg(not(target_os = "android"))]
    sidecar: Option<sidecar::Process>,
    next_id: u64,
}

type BackendState = Arc<Mutex<Backend>>;

impl Backend {
    fn new() -> Self {
        Backend {
            #[cfg(not(target_os = "android"))]
            sidecar: None,
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

        let resp: RpcResponse = serde_json::from_str(&response_json)
            .map_err(|e| format!("Invalid response: {}", e))?;

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
        let proc = self.sidecar.as_mut().ok_or("Sidecar not running")?;
        proc.call_rpc(request_json).await
    }
}

// ─── Tauri commands ───

#[tauri::command]
async fn load_config(state: tauri::State<'_, BackendState>, path: String) -> Result<String, String> {
    let mut guard = state.lock().await;
    guard
        .call("load_config", Some(serde_json::json!({ "path": path })))
        .await?;
    Ok("Config loaded".to_string())
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
async fn get_status(state: tauri::State<'_, BackendState>) -> Result<PlaybackStatus, String> {
    let mut guard = state.lock().await;
    let result = guard.call("get_status", None).await?;
    serde_json::from_value(result).map_err(|e| format!("Failed to parse status: {}", e))
}

#[tauri::command]
async fn export_wav(state: tauri::State<'_, BackendState>, path: String) -> Result<String, String> {
    let mut guard = state.lock().await;
    guard
        .call("export_wav", Some(serde_json::json!({ "path": path })))
        .await?;
    Ok("Export complete".to_string())
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

#[cfg(not(target_os = "android"))]
fn setup_desktop(app: &tauri::App, backend_state: BackendState) {
    use tokio::io::BufReader;

    let resource_path = app
        .path()
        .resource_dir()
        .expect("Failed to get resource dir");
    let sidecar_name = if cfg!(target_os = "windows") {
        "binaural-beats.exe"
    } else {
        "binaural-beats"
    };
    let sidecar_path = resource_path.join("binaries").join(sidecar_name);

    tauri::async_runtime::spawn(async move {
        let mut child = tokio::process::Command::new(&sidecar_path)
            .arg("-rpc")
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::inherit())
            .spawn()
            .unwrap_or_else(|e| panic!("Failed to spawn sidecar at {:?}: {}", sidecar_path, e));

        let stdin = child.stdin.take().expect("Failed to get sidecar stdin");
        let stdout = child.stdout.take().expect("Failed to get sidecar stdout");

        let process = sidecar::Process {
            child,
            stdin,
            stdout: BufReader::new(stdout),
        };

        let mut guard = backend_state.lock().await;
        guard.sidecar = Some(process);
    });
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let backend_state: BackendState = Arc::new(Mutex::new(Backend::new()));

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .setup({
            let state = backend_state.clone();
            move |app| {
                app.manage(state.clone());

                #[cfg(not(target_os = "android"))]
                setup_desktop(app, state);

                Ok(())
            }
        })
        .invoke_handler(tauri::generate_handler![
            load_config,
            play,
            stop,
            get_status,
            export_wav,
            set_stretch,
        ])
        .run(tauri::generate_context!())
        .expect("Error running Tauri application");
}
