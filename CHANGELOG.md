# Changelog

## 0.1.0

First test release of the app for desktop and Android, alongside the command-line player and converter. Expect rough edges; please report problems as GitHub issues.

### App (Windows, macOS, Linux, Android)
- Built-in session library: Unwind, Focus, Power Nap, Meditation, Lucid Dream and Insomniac.
- Opens YAML sessions and SBaGen (`.sbg`) files directly.
- Timeline of the session: beat frequency over the brainwave bands, tone and pink noise volumes, a playhead, hover readout and click-to-seek.
- Play, pause, resume, seek and volume, with click-free transitions.
- Time stretch and WAV export.
- Android: keeps playing with the screen off, with a playback notification.

### Command-line tools
- `binaural-beats` plays or exports sessions; `-preset`, `-list-presets`, `-volume`, `-start` and `-version`.
- Linux binaries play through PulseAudio or PipeWire, falling back to ALSA, with no build-time audio dependencies.
- `converter` turns SBaGen files into YAML, keeping holds, fades (`-fade`), slides (`->`), clock times and the file's title and description.
- Session files are validated on load, with errors that name the offending entry.

### Fixes since the first Android builds
- Removed crackle caused by the audio generator reusing stale buffer samples, and a pink-noise volume of 0 silencing the tones.
- Android APK builds, links and loads its sessions correctly.
