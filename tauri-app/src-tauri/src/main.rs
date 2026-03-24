// Prevents additional console window on Windows in release
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tauri::Manager;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin, ChildStdout};
use tokio::sync::Mutex;

/// JSON-RPC 2.0 request
#[derive(Serialize)]
struct RpcRequest {
    jsonrpc: &'static str,
    method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
    id: u64,
}

/// JSON-RPC 2.0 response
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

/// Manages the sidecar process and RPC communication
struct Sidecar {
    #[allow(dead_code)]
    child: Child,
    stdin: ChildStdin,
    stdout: BufReader<ChildStdout>,
    next_id: u64,
}

type SidecarState = Arc<Mutex<Option<Sidecar>>>;

impl Sidecar {
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

        let mut data = serde_json::to_vec(&req).map_err(|e| e.to_string())?;
        data.push(b'\n');

        self.stdin
            .write_all(&data)
            .await
            .map_err(|e| format!("Failed to write to sidecar: {}", e))?;
        self.stdin
            .flush()
            .await
            .map_err(|e| format!("Failed to flush sidecar stdin: {}", e))?;

        let mut line = String::new();
        self.stdout
            .read_line(&mut line)
            .await
            .map_err(|e| format!("Failed to read from sidecar: {}", e))?;

        let resp: RpcResponse =
            serde_json::from_str(&line).map_err(|e| format!("Invalid response: {}", e))?;

        if let Some(err) = resp.error {
            return Err(err.message);
        }

        Ok(resp.result.unwrap_or(serde_json::Value::Null))
    }
}

/// Playback status from the Go engine
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

#[tauri::command]
async fn load_config(state: tauri::State<'_, SidecarState>, path: String) -> Result<String, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    sidecar
        .call(
            "load_config",
            Some(serde_json::json!({ "path": path })),
        )
        .await?;
    Ok("Config loaded".to_string())
}

#[tauri::command]
async fn play(state: tauri::State<'_, SidecarState>) -> Result<String, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    sidecar.call("play", None).await?;
    Ok("Playing".to_string())
}

#[tauri::command]
async fn stop(state: tauri::State<'_, SidecarState>) -> Result<String, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    sidecar.call("stop", None).await?;
    Ok("Stopped".to_string())
}

#[tauri::command]
async fn get_status(state: tauri::State<'_, SidecarState>) -> Result<PlaybackStatus, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    let result = sidecar.call("get_status", None).await?;
    let status: PlaybackStatus =
        serde_json::from_value(result).map_err(|e| format!("Failed to parse status: {}", e))?;
    Ok(status)
}

#[tauri::command]
async fn export_wav(state: tauri::State<'_, SidecarState>, path: String) -> Result<String, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    sidecar
        .call("export_wav", Some(serde_json::json!({ "path": path })))
        .await?;
    Ok("Export complete".to_string())
}

#[tauri::command]
async fn set_stretch(
    state: tauri::State<'_, SidecarState>,
    factor: f64,
) -> Result<String, String> {
    let mut guard = state.lock().await;
    let sidecar = guard.as_mut().ok_or("Sidecar not running")?;
    sidecar
        .call(
            "set_stretch",
            Some(serde_json::json!({ "factor": factor })),
        )
        .await?;
    Ok(format!("Stretch set to {:.1}x", factor))
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            let sidecar_state: SidecarState = Arc::new(Mutex::new(None));
            app.manage(sidecar_state.clone());

            // Resolve the sidecar binary path
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

            // Spawn the sidecar process
            tauri::async_runtime::spawn(async move {
                let mut child = tokio::process::Command::new(&sidecar_path)
                    .arg("-rpc")
                    .stdin(std::process::Stdio::piped())
                    .stdout(std::process::Stdio::piped())
                    .stderr(std::process::Stdio::inherit())
                    .spawn()
                    .unwrap_or_else(|e| {
                        panic!("Failed to spawn sidecar at {:?}: {}", sidecar_path, e)
                    });

                let stdin = child.stdin.take().expect("Failed to get sidecar stdin");
                let stdout = child.stdout.take().expect("Failed to get sidecar stdout");

                let sidecar = Sidecar {
                    child,
                    stdin,
                    stdout: BufReader::new(stdout),
                    next_id: 1,
                };

                let mut guard = sidecar_state.lock().await;
                *guard = Some(sidecar);
            });

            Ok(())
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
