const { invoke } = window.__TAURI__.core;
const { open, save } = window.__TAURI__.dialog;

// DOM elements
const btnLoad = document.getElementById("btn-load");
const btnPlay = document.getElementById("btn-play");
const btnStop = document.getElementById("btn-stop");
const btnExport = document.getElementById("btn-export");
const btnStretch = document.getElementById("btn-stretch");
const configName = document.getElementById("config-name");
const stretchSlider = document.getElementById("stretch-slider");
const stretchValue = document.getElementById("stretch-value");
const progressFill = document.getElementById("progress-fill");
const timeElapsed = document.getElementById("time-elapsed");
const timeTotal = document.getElementById("time-total");
const statusFreq = document.getElementById("status-freq");
const statusBeat = document.getElementById("status-beat");
const statusToneVol = document.getElementById("status-tone-vol");
const statusPinkVol = document.getElementById("status-pink-vol");
const messageBar = document.getElementById("message-bar");

let statusInterval = null;
let configLoaded = false;

function formatTime(seconds) {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

function showMessage(text, type = "success") {
  messageBar.textContent = text;
  messageBar.className = `message-bar ${type}`;
  setTimeout(() => {
    messageBar.className = "message-bar hidden";
  }, 3000);
}

function updateButtons(isPlaying) {
  btnPlay.disabled = !configLoaded || isPlaying;
  btnStop.disabled = !isPlaying;
  btnExport.disabled = !configLoaded || isPlaying;
  btnLoad.disabled = isPlaying;
  btnStretch.disabled = isPlaying;
  stretchSlider.disabled = isPlaying;
}

async function pollStatus() {
  try {
    const status = await invoke("get_status");

    if (status.config_loaded && !configLoaded) {
      configLoaded = true;
      updateButtons(status.is_playing);
    }

    if (status.is_playing) {
      const pct =
        status.total_duration > 0
          ? (status.time / status.total_duration) * 100
          : 0;
      progressFill.style.width = `${pct}%`;
      timeElapsed.textContent = formatTime(status.time);
      timeTotal.textContent = `/ ${formatTime(status.total_duration)}`;
      statusFreq.textContent = `${status.frequency.toFixed(1)} Hz`;
      statusBeat.textContent = `${status.beat_frequency.toFixed(1)} Hz`;
      statusToneVol.textContent = `${(status.tone_volume * 100).toFixed(0)}%`;
      statusPinkVol.textContent = `${(status.pink_noise_volume * 100).toFixed(0)}%`;
    }

    updateButtons(status.is_playing);

    // If playback stopped, stop polling
    if (!status.is_playing && statusInterval) {
      clearInterval(statusInterval);
      statusInterval = null;
    }
  } catch (e) {
    // Sidecar may not be ready yet
  }
}

function startPolling() {
  if (statusInterval) clearInterval(statusInterval);
  statusInterval = setInterval(pollStatus, 500);
}

// Load config
btnLoad.addEventListener("click", async () => {
  try {
    const path = await open({
      filters: [{ name: "YAML Config", extensions: ["yaml", "yml"] }],
      multiple: false,
    });
    if (!path) return;

    await invoke("load_config", { path });
    configLoaded = true;
    configName.textContent = path.split(/[/\\]/).pop();

    // Get initial status to show duration
    const status = await invoke("get_status");
    timeTotal.textContent = `/ ${formatTime(status.total_duration)}`;

    updateButtons(false);
    showMessage("Config loaded successfully");
  } catch (e) {
    showMessage(`Error: ${e}`, "error");
  }
});

// Play
btnPlay.addEventListener("click", async () => {
  try {
    await invoke("play");
    updateButtons(true);
    startPolling();
    showMessage("Playback started");
  } catch (e) {
    showMessage(`Error: ${e}`, "error");
  }
});

// Stop
btnStop.addEventListener("click", async () => {
  try {
    await invoke("stop");
    updateButtons(false);
    showMessage("Playback stopped");
  } catch (e) {
    showMessage(`Error: ${e}`, "error");
  }
});

// Export WAV
btnExport.addEventListener("click", async () => {
  try {
    const path = await save({
      filters: [{ name: "WAV Audio", extensions: ["wav"] }],
      defaultPath: "binaural-beats.wav",
    });
    if (!path) return;

    btnExport.textContent = "Exporting...";
    btnExport.disabled = true;

    await invoke("export_wav", { path });

    btnExport.textContent = "Export WAV";
    btnExport.disabled = false;
    showMessage("WAV exported successfully");
  } catch (e) {
    btnExport.textContent = "Export WAV";
    btnExport.disabled = false;
    showMessage(`Error: ${e}`, "error");
  }
});

// Stretch slider
stretchSlider.addEventListener("input", () => {
  stretchValue.textContent = `${stretchSlider.value}x`;
});

btnStretch.addEventListener("click", async () => {
  try {
    const factor = parseFloat(stretchSlider.value);
    await invoke("set_stretch", { factor });
    showMessage(`Stretch set to ${factor}x`);
  } catch (e) {
    showMessage(`Error: ${e}`, "error");
  }
});

// Initialize
updateButtons(false);
