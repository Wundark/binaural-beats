const { invoke } = window.__TAURI__.core;
const { open, save } = window.__TAURI__.dialog;

// Android maps dialog filters to MIME types, and .yaml usually has none, so a
// filter there would hide every config file.
const isAndroid = /android/i.test(navigator.userAgent);

// DOM elements
const btnLoad = document.getElementById("btn-load");
const btnPlay = document.getElementById("btn-play");
const btnStop = document.getElementById("btn-stop");
const btnExport = document.getElementById("btn-export");
const btnStretch = document.getElementById("btn-stretch");
const configName = document.getElementById("config-name");
const sessionDuration = document.getElementById("session-duration");
const sessionDescription = document.getElementById("session-description");
const presetList = document.getElementById("preset-list");
const stretchSlider = document.getElementById("stretch-slider");
const stretchValue = document.getElementById("stretch-value");
const volumeSlider = document.getElementById("volume-slider");
const volumeValue = document.getElementById("volume-value");
const progressBar = document.getElementById("progress-bar");
const progressFill = document.getElementById("progress-fill");
const timeElapsed = document.getElementById("time-elapsed");
const timeTotal = document.getElementById("time-total");
const statusFreq = document.getElementById("status-freq");
const statusBeat = document.getElementById("status-beat");
const statusToneVol = document.getElementById("status-tone-vol");
const statusPinkVol = document.getElementById("status-pink-vol");
const messageBar = document.getElementById("message-bar");

let statusInterval = null;
let messageTimer = null;
let exporting = false;
// Last status from the engine.
let current = { config_loaded: false, is_playing: false, is_paused: false, time: 0, total_duration: 0 };

function formatTime(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60).toString().padStart(2, "0");
  return h > 0 ? `${h}:${m.toString().padStart(2, "0")}:${s}` : `${m}:${s}`;
}

function showMessage(text, type = "success") {
  messageBar.textContent = text;
  messageBar.className = `message-bar ${type}`;
  clearTimeout(messageTimer);
  messageTimer = setTimeout(() => {
    messageBar.className = "message-bar hidden";
  }, 3000);
}

async function run(action) {
  try {
    await action();
  } catch (e) {
    showMessage(`Error: ${e}`, "error");
  }
}

function render(status) {
  current = status;
  const loaded = status.config_loaded;
  const playing = status.is_playing;

  btnPlay.disabled = !loaded || exporting;
  btnPlay.textContent = !playing ? "Play" : status.is_paused ? "Resume" : "Pause";
  btnStop.disabled = !playing;
  btnExport.disabled = !loaded || playing || exporting;
  btnLoad.disabled = playing || exporting;
  for (const button of presetList.querySelectorAll("button")) {
    button.disabled = playing || exporting;
  }
  btnStretch.disabled = playing || exporting;
  stretchSlider.disabled = playing || exporting;
  progressBar.classList.toggle("seekable", loaded);

  if (!loaded) return;
  const pct = status.total_duration > 0 ? (status.time / status.total_duration) * 100 : 0;
  progressFill.style.width = `${pct}%`;
  timeElapsed.textContent = formatTime(status.time);
  timeTotal.textContent = `/ ${formatTime(status.total_duration)}`;
  statusFreq.textContent = `${status.frequency.toFixed(1)} Hz`;
  statusBeat.textContent = `${status.beat_frequency.toFixed(1)} Hz`;
  statusToneVol.textContent = `${(status.tone_volume * 100).toFixed(0)}%`;
  statusPinkVol.textContent = `${(status.pink_noise_volume * 100).toFixed(0)}%`;
}

async function refresh() {
  const status = await invoke("get_status");
  render(status);
  // Poll only while playing; stop once the session ends.
  if (status.is_playing && !statusInterval) {
    statusInterval = setInterval(() => refresh().catch(() => {}), 500);
  } else if (!status.is_playing && statusInterval) {
    clearInterval(statusInterval);
    statusInterval = null;
  }
  return status;
}

// Session loading
function showSession(info, presetId = null) {
  configName.textContent = info.name;
  sessionDuration.textContent = formatTime(info.total_duration);
  sessionDescription.textContent = info.description || "";
  for (const button of presetList.querySelectorAll("button")) {
    button.classList.toggle("selected", button.dataset.id === presetId);
  }
}

async function loadPresets() {
  const presets = await invoke("list_presets");
  presetList.replaceChildren(
    ...presets.map((p) => {
      const button = document.createElement("button");
      button.className = "preset";
      button.dataset.id = p.id;
      button.title = p.description;
      const name = document.createElement("span");
      name.className = "preset-name";
      name.textContent = p.name;
      const duration = document.createElement("span");
      duration.className = "preset-duration";
      duration.textContent = formatTime(p.total_duration);
      button.append(name, duration);
      button.addEventListener("click", () =>
        run(async () => {
          showSession(await invoke("load_preset", { id: p.id }), p.id);
          await refresh();
        })
      );
      return button;
    })
  );
  render(current);
}

btnLoad.addEventListener("click", () =>
  run(async () => {
    const path = await open({
      filters: isAndroid
        ? []
        : [{ name: "Sessions (YAML, SBaGen)", extensions: ["yaml", "yml", "sbg"] }],
      multiple: false,
    });
    if (!path) return;

    showSession(await invoke("load_config", { path }));
    await refresh();
    showMessage("Session loaded");
  })
);

// Play / Pause / Resume
btnPlay.addEventListener("click", () =>
  run(async () => {
    if (!current.is_playing) {
      await invoke("play");
    } else if (current.is_paused) {
      await invoke("resume");
    } else {
      await invoke("pause");
    }
    await refresh();
  })
);

// Stop
btnStop.addEventListener("click", () =>
  run(async () => {
    await invoke("stop");
    await refresh();
  })
);

// Seek by clicking the progress bar (while stopped, this sets where Play starts)
progressBar.addEventListener("click", (event) =>
  run(async () => {
    if (!current.config_loaded || current.total_duration <= 0) return;
    const rect = progressBar.getBoundingClientRect();
    const fraction = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    await invoke("seek", { time: fraction * current.total_duration });
    await refresh();
  })
);

// Volume
volumeSlider.addEventListener("input", () =>
  run(async () => {
    volumeValue.textContent = `${volumeSlider.value}%`;
    await invoke("set_volume", { volume: volumeSlider.value / 100 });
  })
);

// Export WAV
btnExport.addEventListener("click", () =>
  run(async () => {
    const path = await save({
      filters: [{ name: "WAV Audio", extensions: ["wav"] }],
      defaultPath: "binaural-beats.wav",
    });
    if (!path) return;

    exporting = true;
    btnExport.textContent = "Exporting...";
    render(current);
    try {
      await invoke("export_wav", { path });
      showMessage("WAV exported successfully");
    } finally {
      exporting = false;
      btnExport.textContent = "Export WAV";
      render(current);
    }
  })
);

// Stretch slider
stretchSlider.addEventListener("input", () => {
  stretchValue.textContent = `${stretchSlider.value}x`;
});

btnStretch.addEventListener("click", () =>
  run(async () => {
    const factor = parseFloat(stretchSlider.value);
    await invoke("set_stretch", { factor });
    await refresh();
    showMessage(`Stretch set to ${factor}x`);
  })
);

// Space toggles play/pause (desktop keyboards)
document.addEventListener("keydown", (event) => {
  if (event.code === "Space" && event.target === document.body && !btnPlay.disabled) {
    event.preventDefault();
    btnPlay.click();
  }
});

// Initialize: the engine may still be starting, so retry briefly.
(async function init() {
  render(current);
  for (let i = 0; i < 20; i++) {
    try {
      const status = await refresh();
      volumeSlider.value = Math.round(status.volume * 100);
      volumeValue.textContent = `${volumeSlider.value}%`;
      await loadPresets();
      return;
    } catch (e) {
      await new Promise((r) => setTimeout(r, 250));
    }
  }
})();
