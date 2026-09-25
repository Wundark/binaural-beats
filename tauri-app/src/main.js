import { Timeline } from "./timeline.js";

const { invoke } = window.__TAURI__.core;
const { open, save } = window.__TAURI__.dialog;

// Android maps dialog filters to MIME types, and .yaml usually has none, so a
// filter there would hide every config file.
const isAndroid = /android/i.test(navigator.userAgent);

// DOM elements
const btnLoad = document.getElementById("btn-load");
const btnQueue = document.getElementById("btn-queue");
const btnPlay = document.getElementById("btn-play");
const btnStop = document.getElementById("btn-stop");
const btnExport = document.getElementById("btn-export");
const btnStretch = document.getElementById("btn-stretch");
const btnSettings = document.getElementById("btn-settings");
const btnExportPlaylist = document.getElementById("btn-export-playlist");
const btnClearPlaylist = document.getElementById("btn-clear-playlist");
const configName = document.getElementById("config-name");
const sessionDuration = document.getElementById("session-duration");
const sessionDescription = document.getElementById("session-description");
const presetList = document.getElementById("preset-list");
const playlistList = document.getElementById("playlist");
const playlistEmpty = document.getElementById("playlist-empty");
const playlistSummary = document.getElementById("playlist-summary");
const playlistControls = document.getElementById("playlist-controls");
const stretchSlider = document.getElementById("stretch-slider");
const stretchValue = document.getElementById("stretch-value");
const volumeSlider = document.getElementById("volume-slider");
const volumeValue = document.getElementById("volume-value");
const seekSlider = document.getElementById("seek-slider");
const nudgeButtons = document.querySelectorAll("[data-nudge]");
const timelineHover = document.getElementById("timeline-hover");
const timeElapsed = document.getElementById("time-elapsed");
const timeTotal = document.getElementById("time-total");
const statusSection = document.querySelector(".status-section");
const statusFreq = document.getElementById("status-freq");
const statusBeat = document.getElementById("status-beat");
const statusToneVol = document.getElementById("status-tone-vol");
const statusPinkVol = document.getElementById("status-pink-vol");
const messageBar = document.getElementById("message-bar");
const settingsDialog = document.getElementById("settings");
const settingCrossfade = document.getElementById("setting-crossfade");
const settingCrossfadeValue = document.getElementById("setting-crossfade-value");
const settingRepeat = document.getElementById("setting-repeat");
const settingKeepPlaylist = document.getElementById("setting-keep-playlist");
const settingShowStatus = document.getElementById("setting-show-status");

let statusInterval = null;
let messageTimer = null;
let exporting = false;
let seekDragging = false;
// Last status from the engine.
let current = { config_loaded: false, is_playing: false, is_paused: false, time: 0, total_duration: 0, playlist_index: -1 };
let playlist = { items: [], current: -1, crossfade: 10, loop: false };

// ─── Saved state ───
//
// Settings, volume and the playlist are kept in the page's local storage. The
// engine forgets them when the app closes.

const store = {
  get(key, fallback) {
    try {
      const v = localStorage.getItem(key);
      return v === null ? fallback : JSON.parse(v);
    } catch {
      return fallback;
    }
  },
  set(key, value) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // Storage full or unavailable: nothing is saved, which is fine.
    }
  },
};

const defaultSettings = { crossfade: 10, repeat: false, keepPlaylist: true, showStatus: true };
let settings = { ...defaultSettings, ...store.get("settings", {}) };

function formatTime(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60).toString().padStart(2, "0");
  return h > 0 ? `${h}:${m.toString().padStart(2, "0")}:${s}` : `${m}:${s}`;
}

// A file name from a session name: "Deep Sleep (90 min)" -> "deep-sleep-90-min".
function fileSlug(name) {
  return name
    .normalize("NFKD")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
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

// On Android, a foreground service keeps the session playing with the screen
// off (see PlaybackService.kt). Tell it when the state changes, the session
// changes, or a seek moves the end time.
const androidPlayback = window.AndroidPlayback;
const LOOPING_MS = 12 * 3600 * 1000; // a looping playlist has no end; renew often
let backgroundSync = { state: "stopped", endsAt: 0, title: "" };

function syncBackgroundPlayback(status) {
  if (!androidPlayback) return;
  const state = !status.is_playing ? "stopped" : status.is_paused ? "paused" : "playing";
  const remainingMs = status.remaining < 0 ? LOOPING_MS : Math.max(0, status.remaining * 1000);
  const endsAt = Date.now() + remainingMs;
  const title = configName.textContent;
  const moved = state === "playing" && Math.abs(endsAt - backgroundSync.endsAt) > 5000;
  if (state === backgroundSync.state && title === backgroundSync.title && !moved) return;
  backgroundSync = { state, endsAt, title };
  try {
    if (state === "stopped") {
      androidPlayback.stop();
    } else {
      androidPlayback.update(title, state === "playing", remainingMs);
    }
  } catch (e) {
    console.error("background playback:", e);
  }
}

function render(status) {
  // While playing, the playlist moving on changes the loaded session.
  const advanced = status.is_playing && status.playlist_index >= 0 && status.playlist_index !== current.playlist_index;
  current = status;
  if (advanced) syncSession().catch(() => {});
  syncBackgroundPlayback(status);
  const loaded = status.config_loaded;
  const playing = status.is_playing;

  btnPlay.disabled = !loaded || exporting;
  btnPlay.textContent = !playing ? "Play" : status.is_paused ? "Resume" : "Pause";
  btnStop.disabled = !playing;
  btnExport.disabled = !loaded || playing || exporting;
  btnLoad.disabled = playing || exporting;
  btnQueue.disabled = !loaded || status.playlist_index >= 0;
  btnExportPlaylist.disabled = playing || exporting;
  for (const button of presetList.querySelectorAll("button")) {
    button.disabled = playing || exporting;
  }
  btnStretch.disabled = playing || exporting;
  stretchSlider.disabled = playing || exporting;
  seekSlider.disabled = !loaded;
  for (const button of nudgeButtons) button.disabled = !loaded;
  renderPlaylistCurrent();

  if (!loaded) return;
  timeline.setTime(status.time);
  seekSlider.max = Math.floor(status.total_duration);
  if (!seekDragging) {
    seekSlider.value = Math.floor(status.time);
    timeElapsed.textContent = formatTime(status.time);
  }
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

// ─── Session ───

function showSession(info, presetId = null) {
  configName.textContent = info.name;
  sessionDuration.textContent = formatTime(info.total_duration);
  sessionDescription.textContent = info.description || "";
  for (const button of presetList.querySelectorAll("button")) {
    button.classList.toggle("selected", button.dataset.id === presetId);
  }
}

// Show the loaded session and its timeline, as the engine has it.
async function syncSession() {
  const loaded = await invoke("get_timeline");
  showSession(loaded);
  timeline.setData(loaded);
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
          await refreshTimeline();
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
    await refreshTimeline();
    await refresh();
    showMessage("Session loaded");
  })
);

// ─── Playlist ───

function savePlaylist() {
  store.set("playlist", settings.keepPlaylist ? playlist.items.map((i) => i.config) : []);
}

function setPlaylist(p) {
  playlist = p;
  savePlaylist();
  renderPlaylist();
}

function iconButton(label, title, onClick) {
  const b = document.createElement("button");
  b.className = "icon-btn small";
  b.textContent = label;
  b.title = title;
  b.setAttribute("aria-label", title);
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    run(onClick);
  });
  return b;
}

function renderPlaylist() {
  const items = playlist.items;
  playlistEmpty.classList.toggle("hidden", items.length > 0);
  playlistControls.classList.toggle("hidden", items.length === 0);
  const total = items.reduce((sum, i) => sum + i.total_duration, 0);
  playlistSummary.textContent = items.length
    ? `${items.length} session${items.length > 1 ? "s" : ""} · ${formatTime(total)}${playlist.loop ? " · repeats" : ""}`
    : "";
  playlistList.replaceChildren(
    ...items.map((item, i) => {
      const li = document.createElement("li");
      li.className = "playlist-item";
      li.tabIndex = 0;
      li.title = current.is_playing ? "Crossfade to this session" : "Load this session";
      const name = document.createElement("span");
      name.className = "playlist-name";
      name.textContent = item.name;
      const duration = document.createElement("span");
      duration.className = "playlist-duration";
      duration.textContent = formatTime(item.total_duration);
      const actions = document.createElement("span");
      actions.className = "playlist-actions";
      const up = iconButton("▲", "Move up", async () => setPlaylist(await invoke("playlist_move", { from: i, to: i - 1 })));
      up.disabled = i === 0;
      const down = iconButton("▼", "Move down", async () => setPlaylist(await invoke("playlist_move", { from: i, to: i + 1 })));
      down.disabled = i === items.length - 1;
      const remove = iconButton("✕", "Remove", async () => {
        setPlaylist(await invoke("playlist_remove", { index: i }));
        await refresh();
      });
      actions.append(up, down, remove);
      li.append(name, duration, actions);
      const select = () =>
        run(async () => {
          await invoke("playlist_select", { index: i });
          await syncSession();
          await refresh();
        });
      li.addEventListener("click", select);
      li.addEventListener("keydown", (e) => {
        if (e.key === "Enter") select();
      });
      return li;
    })
  );
  renderPlaylistCurrent();
}

function renderPlaylistCurrent() {
  playlistList.querySelectorAll(".playlist-item").forEach((li, i) => {
    li.classList.toggle("current", i === current.playlist_index);
    li.classList.toggle("playing", i === current.playlist_index && current.is_playing);
  });
}

btnQueue.addEventListener("click", () =>
  run(async () => {
    setPlaylist(await invoke("playlist_add"));
    await refresh();
    showMessage("Added to playlist");
  })
);

btnClearPlaylist.addEventListener("click", () =>
  run(async () => {
    setPlaylist(await invoke("playlist_clear"));
    await refresh();
  })
);

// Put back a saved playlist when the engine starts empty (a fresh launch).
async function restorePlaylist() {
  playlist = await invoke("get_playlist");
  const saved = settings.keepPlaylist ? store.get("playlist", []) : [];
  if (playlist.items.length === 0 && saved.length > 0) {
    let failed = 0;
    for (const config of saved) {
      try {
        playlist = await invoke("playlist_add", { config });
      } catch {
        failed++;
      }
    }
    if (failed) showMessage(`${failed} saved playlist session(s) could not be restored`, "error");
  }
  renderPlaylist();
}

// ─── Playback ───

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

btnStop.addEventListener("click", () =>
  run(async () => {
    await invoke("stop");
    await refresh();
  })
);

async function seekTo(time) {
  await invoke("seek", { time: Math.max(0, Math.min(time, current.total_duration)) });
  await refresh();
}

// Timeline: tap, click or drag to seek (while stopped, this sets where Play starts)
const timeline = new Timeline(document.getElementById("timeline"), {
  onSeek: (time) => run(() => seekTo(time)),
  onHover: (info) => {
    timelineHover.textContent = info
      ? `${formatTime(info.time)} · ${info.beat.toFixed(1)} Hz ${info.band.toLowerCase()}`
      : "";
  },
});

async function refreshTimeline() {
  timeline.setData(await invoke("get_timeline"));
}

// Position slider for fine adjustment: preview while dragging, seek on release.
seekSlider.addEventListener("input", () => {
  seekDragging = true;
  const t = Number(seekSlider.value);
  timeElapsed.textContent = formatTime(t);
  timeline.setPreview(t);
});
seekSlider.addEventListener("change", () =>
  run(async () => {
    seekDragging = false;
    timeline.setPreview(null);
    await seekTo(Number(seekSlider.value));
  })
);

for (const button of nudgeButtons) {
  button.addEventListener("click", () => run(() => seekTo(current.time + Number(button.dataset.nudge))));
}

// Volume
volumeSlider.addEventListener("input", () =>
  run(async () => {
    volumeValue.textContent = `${volumeSlider.value}%`;
    store.set("volume", Number(volumeSlider.value));
    await invoke("set_volume", { volume: volumeSlider.value / 100 });
  })
);

// ─── Export ───

async function exportWav(playlistExport) {
  const name = playlistExport ? "binaural-playlist" : fileSlug(configName.textContent) || "binaural-beats";
  const path = await save({
    filters: [{ name: "WAV Audio", extensions: ["wav"] }],
    defaultPath: `${name}.wav`,
  });
  if (!path) return;

  const button = playlistExport ? btnExportPlaylist : btnExport;
  const label = button.textContent;
  exporting = true;
  button.textContent = "Exporting...";
  render(current);
  try {
    await invoke("export_wav", { path, playlist: playlistExport });
    showMessage("WAV exported successfully");
  } finally {
    exporting = false;
    button.textContent = label;
    render(current);
  }
}

btnExport.addEventListener("click", () => run(() => exportWav(false)));
btnExportPlaylist.addEventListener("click", () => run(() => exportWav(true)));

// ─── Time stretch ───

stretchSlider.addEventListener("input", () => {
  stretchValue.textContent = `${stretchSlider.value}x`;
});

btnStretch.addEventListener("click", () =>
  run(async () => {
    const factor = parseFloat(stretchSlider.value);
    await invoke("set_stretch", { factor });
    await refreshTimeline();
    setPlaylist(await invoke("get_playlist"));
    await refresh();
    showMessage(`Stretch set to ${factor}x`);
  })
);

// ─── Settings ───

function renderSettings() {
  settingCrossfade.value = settings.crossfade;
  settingCrossfadeValue.textContent = `${settings.crossfade} s`;
  settingRepeat.checked = settings.repeat;
  settingKeepPlaylist.checked = settings.keepPlaylist;
  settingShowStatus.checked = settings.showStatus;
  statusSection.classList.toggle("hidden", !settings.showStatus);
}

async function applySettings() {
  store.set("settings", settings);
  renderSettings();
  playlist = await invoke("set_playlist_options", { crossfade: settings.crossfade, repeat: settings.repeat });
  savePlaylist();
  renderPlaylist();
}

btnSettings.addEventListener("click", () => settingsDialog.showModal());
// Clicking the backdrop closes the dialog.
settingsDialog.addEventListener("click", (e) => {
  if (e.target === settingsDialog) settingsDialog.close();
});
settingCrossfade.addEventListener("input", () => {
  settingCrossfadeValue.textContent = `${settingCrossfade.value} s`;
});
settingCrossfade.addEventListener("change", () => {
  settings.crossfade = Number(settingCrossfade.value);
  run(applySettings);
});
settingRepeat.addEventListener("change", () => {
  settings.repeat = settingRepeat.checked;
  run(applySettings);
});
settingKeepPlaylist.addEventListener("change", () => {
  settings.keepPlaylist = settingKeepPlaylist.checked;
  run(applySettings);
});
settingShowStatus.addEventListener("change", () => {
  settings.showStatus = settingShowStatus.checked;
  run(applySettings);
});

// Space toggles play/pause (desktop keyboards)
document.addEventListener("keydown", (event) => {
  if (event.code === "Space" && event.target === document.body && !btnPlay.disabled) {
    event.preventDefault();
    btnPlay.click();
  }
});

// Initialize: the engine may still be starting, so retry briefly.
(async function init() {
  renderSettings();
  render(current);
  window.__TAURI__.app
    ?.getVersion()
    .then((v) => {
      document.getElementById("app-version").textContent = `Binaural Beats ${v}`;
      document.getElementById("settings-version").textContent = `Version ${v}`;
    })
    .catch(() => {});
  for (let i = 0; i < 20; i++) {
    let status;
    try {
      status = await refresh();
    } catch (e) {
      await new Promise((r) => setTimeout(r, 250));
      continue;
    }
    try {
      // A fresh engine has the default volume; use the one last chosen.
      const savedVolume = store.get("volume", null);
      if (!status.is_playing && !status.config_loaded && savedVolume !== null && Math.round(status.volume * 100) !== savedVolume) {
        await invoke("set_volume", { volume: savedVolume / 100 });
        status.volume = savedVolume / 100;
      }
      // Match the controls to the engine, which keeps its settings when the
      // page reloads.
      volumeSlider.value = Math.round(status.volume * 100);
      volumeValue.textContent = `${volumeSlider.value}%`;
      stretchSlider.value = status.stretch;
      stretchValue.textContent = `${stretchSlider.value}x`;
      await loadPresets();
      await restorePlaylist();
      await applySettings();
      if (status.config_loaded) {
        // The page was reloaded (e.g. an Android rotation) with a session loaded.
        await syncSession();
      }
    } catch (e) {
      showMessage(`Error: ${e}`, "error");
    }
    return;
  }
})();
