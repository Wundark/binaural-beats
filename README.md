# **Binaural Beats Generator with Pink Noise and Configurable Parameters**

## **Introduction**

Go-based application that generates binaural beats with optional pink noise. It allows for customized audio sessions by specifying various parameters such as base frequency, beat frequency, volume levels, and pink noise settings over time through a YAML configuration file.

Binaural beats are auditory illusions perceived when two slightly different frequencies are presented to each ear separately. They are believed to influence brainwave patterns and can aid in relaxation, meditation, sleep, and focus.

---

## **Features**

- **Customizable Frequencies**: Set base frequencies and beat frequencies that change over time.
- **Volume Control**: Adjust the volume of the tones and pink noise independently.
- **Pink Noise Integration**: Optionally include pink noise in your audio sessions.
- **Time-Based Configuration**: Specify frequency and volume changes at specific times.
- **Smooth Transitions**: Linear interpolation between frequency and volume changes for seamless transitions.
- **Command-Line Interface**: Run the application from the command line with a specified configuration file.

---

## **Installation**

### **Prerequisites**

- **Go Programming Language**: Version 1.20 or higher is recommended.

### **Clone the Repository**

```bash
git clone https://github.com/Wundark/binaural-beats.git
cd binaural-beats
```

---

## **Usage**

### **Playing a config**

Ensure you are in the project directory and have Go installed.

```bash
go run cmd/binaural-beats/main.go -config example_config/insomniac.yaml
```

#### Command line options

* `-config` - Path to the YAML config
* `-output` - (OPTIONAL) Path for the WAV to be saved
* `-stretch` - (OPTIONAL) Stretch factor for playback time (default 1.0)

### **Export a config to WAV**

WAV output files will be large. Around 400MB

```bash
go run cmd/binaural-beats/main.go -config example_config/insomniac.yaml -output insomniac.wav
```

### **Converting from SBG to YAML**

Ensure you are in the project directory and have Go installed.

```bash
go run cmd/converter/main.go -input insomniac.sbg -output config/insomniac.yaml
```

#### Command line options

* `-input` - Path to the SBG file
* `-output` - (OPTIONAL) Path to YAML output (default output to stdout)

---

## **YAML Configuration Guide**

The configuration file is written in YAML format and defines how the binaural beats and pink noise change over time.

### **Configuration Structure**

```yaml
frequency_changes:
  - time: <float>               # Time in seconds from the start of playback
    frequency: <float>          # Base frequency in Hz
    beat_frequency: <float>     # Beat frequency in Hz
    pink_noise_volume: <float>  # Pink noise volume (0.0 to 1.0)
    tone_volume: <float>        # Tone volume (0.0 to 1.0)
```

### **Parameter Descriptions**

- **time**: The point in time (in seconds) when the specified settings take effect. The time should be in ascending order.
- **frequency**: The base frequency of the tone in Hertz (Hz).
- **beat_frequency**: The frequency difference between the left and right channels, creating the binaural beat effect.
- **pink_noise_volume**: The volume level of the pink noise, ranging from 0.0 (silent) to 1.0 (maximum volume).
- **tone_volume**: The volume level of the tone, ranging from 0.0 to 1.0.

### **Example Configuration**

```yaml
frequency_changes:
  - time: 0
    frequency: 300.0
    beat_frequency: 10.0
    pink_noise_volume: 0.4
    tone_volume: 0.1
  - time: 900
    frequency: 300.0
    beat_frequency: 10.0
    pink_noise_volume: 0.4
    tone_volume: 0.1
  - time: 1200
    frequency: 150.0
    beat_frequency: 6.0
    pink_noise_volume: 0.2
    tone_volume: 0.15
  - time: 1800
    frequency: 150.0
    beat_frequency: 6.0
    pink_noise_volume: 0.2
    tone_volume: 0.15
  - time: 2100
    frequency: 150.0
    beat_frequency: 2.0
    pink_noise_volume: 0.05
    tone_volume: 0.2
  - time: 2400
    frequency: 150.0
    beat_frequency: 2.0
    pink_noise_volume: 0.05
    tone_volume: 0.2
  - time: 2700
    frequency: 0.0
    beat_frequency: 0.0
    pink_noise_volume: 0.0
    tone_volume: 0.0
```

This example replicates the ["Insomniac" file](https://github.com/brainbang/sbagen_idoser/blob/master/sbg/insomniac.sbg) from the SBAGen format, converted into the YAML configuration for this application.

---

## **Project Structure**

- **cmd/binaural-beats/main.go**: The binaural beats player.
- **cmd/converter/main.go**: Convert from SBG to YAML
- **example_config/lucid_dream.yaml**: The Lucid Dream SBG converted to YAML
- **example_config/insomniac.yaml**: The Insomniac SBG converted to YAML

---

## **Acknowledgments**

- **GoPXL Beep Library**: For providing audio playback and manipulation capabilities.
- **SBAGen**: Inspiration for the audio session configurations.