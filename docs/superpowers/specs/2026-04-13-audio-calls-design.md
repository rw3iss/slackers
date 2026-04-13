# Audio Calling System — Design Spec

## Goal

Enable P2P voice calls between friends in slackers with audio effects (EQ + compressor), call management UI, and persistent effect profiles.

## Scope

Slack has no public API for programmatic voice calls. All audio calls are **P2P-only** via the existing libp2p infrastructure. Only friends with established P2P connections can call each other.

## Dependencies

| Library | Purpose | Install |
|---------|---------|---------|
| `github.com/gen2brain/malgo` | Audio capture/playback (miniaudio C binding) | `go get` (CGo, no system deps) |
| `github.com/hraban/opus` | Opus codec encode/decode | `go get` + `sudo dnf install opus-devel` |

Both are CGo. The binary grows ~2-3MB. Zero runtime cost when no call is active.

## Architecture

### Package Structure

```
internal/audio/
  engine.go        — Engine type: manages devices, codec, capture/playback goroutines
  codec.go         — Opus encoder/decoder wrapper with configurable bitrate/frame size
  jitter.go        — Jitter buffer for reordering/smoothing incoming audio frames
  effects.go       — EQ + compressor DSP chain, applied per-sample
  profile.go       — Effect profile persistence (load/save JSON)
  vad.go           — Voice Activity Detection (silence suppression)

internal/secure/
  p2p.go           — Add P2PAudioProtocol, call signaling message types, audio stream handler

internal/tui/
  audiocall.go     — AudioCallModel overlay (call UI, mute, effects, timer)
  audiocall_taskbar.go — Active call taskbar badge (like notifications/downloads)
  sidebaroptions.go — Add "Start Audio Call" option for friend channels
  handlers_ui.go   — Handle SidebarActionStartAudioCall
  model.go         — Add overlayAudioCall, audioEngine field, call state, shortcut
```

## Phase A: Call Signaling + Basic Audio Streaming

### P2P Protocol

New protocol for audio data streams (separate from messaging to avoid blocking):

```go
const P2PAudioProtocol = protocol.ID("/slackers/audio/1.0.0")
```

### Signaling Messages

Call signaling uses the existing P2P message system (JSON over `/slackers/msg/1.0.0`):

```go
MsgTypeCallRequest  = "call_request"   // {CallID, CallerName}
MsgTypeCallAccept   = "call_accept"    // {CallID}
MsgTypeCallReject   = "call_reject"    // {CallID, Reason}
MsgTypeCallEnd      = "call_end"       // {CallID}
MsgTypeCallMute     = "call_mute"      // {CallID, Muted bool} — informational
```

### Call Flow

```
Caller                              Callee
  |                                   |
  |--- call_request {CallID} -------->|
  |                                   | (ring overlay appears)
  |                                   |
  |<-- call_accept {CallID} ----------| (user pressed Accept)
  |                                   |
  | [Both open audio stream on P2PAudioProtocol]
  |                                   |
  |=== Opus frames (bidirectional) ===|
  |                                   |
  |--- call_end {CallID} ------------>| (either side)
  |                                   |
  | [Both close audio stream, release devices]
```

### Audio Stream Wire Format

Over the `/slackers/audio/1.0.0` stream, audio frames are sent as:

```
[2 bytes: frame length (uint16 big-endian)][N bytes: Opus frame]
[2 bytes: frame length][N bytes: Opus frame]
...
```

No JSON. Pure binary. Each Opus frame is 20ms at 48kHz mono = ~60-80 bytes at the default bitrate (32kbps). This yields ~4KB/s per direction.

### Audio Engine

```go
type Engine struct {
    ctx       context.Context
    cancel    context.CancelFunc
    device    *malgo.Device       // capture + playback (full-duplex)
    encoder   *opus.Encoder
    decoder   *opus.Decoder
    outStream io.Writer           // P2P stream for sending
    inBuffer  *JitterBuffer       // smooths incoming frames
    effects   *EffectChain        // DSP chain (EQ + compressor)
    muted     bool
    stats     CallStats           // packet counts, latency estimates
}
```

**Capture loop** (goroutine):
1. `malgo` delivers PCM samples from mic via callback
2. Apply outgoing effects chain (if enabled)
3. Encode to Opus frame
4. If not muted and VAD detects voice: write frame to outStream
5. If muted or silence: write silence frame (keeps timing consistent)

**Playback loop** (goroutine):
1. Read Opus frames from P2P stream
2. Push into jitter buffer
3. Jitter buffer delivers frames in order with gap filling
4. Decode Opus to PCM
5. Apply incoming effects chain (if enabled)
6. Write PCM to `malgo` playback device

### Jitter Buffer

Simple fixed-delay buffer (50ms = ~2.5 frames at 20ms/frame):
- Incoming frames are tagged with sequence number
- Buffer reorders out-of-order frames
- Missing frames trigger Opus PLC (Packet Loss Concealment) — the decoder generates comfort noise
- Buffer depth is configurable (default 50ms, range 20-200ms)

### Codec Settings

- Sample rate: 48000 Hz
- Channels: 1 (mono)
- Frame size: 960 samples (20ms)
- Bitrate: 32000 bps (configurable 16000-96000)
- Application: `opus.AppVoIP` (optimized for speech)

## Phase B: Call UI + Management

### Overlay: `overlayAudioCall`

The call overlay has three states:

**Ringing (outgoing):**
```
╭─ Calling... ─────────────────╮
│                               │
│     📞 Calling Ryan Weiss     │
│                               │
│         Ringing...            │
│                               │
│  Enter: cancel                │
╰───────────────────────────────╯
```

**Ringing (incoming):**
```
╭─ Incoming Call ──────────────╮
│                               │
│  📞 Ryan Weiss is calling     │
│                               │
│  Enter: accept · Esc: decline │
╰───────────────────────────────╯
```

**Active call:**
```
╭─ Call with Ryan Weiss ────────╮
│                               │
│  Duration: 2:34               │
│  Status: Connected            │
│                               │
│  🎤 Mic: on                   │
│  🔊 Speaker: on               │
│  📊 Effects: EQ + Compressor  │
│                               │
│  m: mute/unmute               │
│  e: effects settings          │
│  Enter: go to chat            │
│  Esc: close (call continues)  │
│  q: end call                  │
╰───────────────────────────────╯
```

### Effects Settings (sub-screen within call overlay)

The effects screen is wide — uses most of the terminal width to give room for the EQ visualization and meters. It has two tabs: **Outgoing** (your mic) and **Incoming** (their audio), switched with Tab.

```
╭─ Audio Effects ── Outgoing (your mic) ──────────────────────────────────────────────────────╮
│                                                                                              │
│  ── 7-Band Equalizer ── [on]                                                                 │
│                                                                                              │
│       100Hz   250Hz   500Hz    1kHz    3kHz    7kHz   12kHz                                   │
│  +12 ┤                                                                                       │
│      │                                                                                       │
│   +6 ┤         ██                                                                            │
│      │  ██     ██                               ██                                           │
│    0 ┤──██─────██──────────────────────────────────────────                                   │
│      │                  ██     ██     ██                                                      │
│   -6 ┤                                                    ██                                  │
│      │                                                                                       │
│  -12 ┤                                                                                       │
│                                                                                              │
│    > Low:     +3.0 dB       Mid:      0.0 dB       High:    -6.0 dB                         │
│      Lo-Mid:  +6.0 dB       Hi-Mid:  -3.0 dB       12kHz:   -2.0 dB                         │
│      250Hz:   +4.5 dB                                                                        │
│                                                                                              │
│  ── Compressor ── [on]                                                                       │
│                                                                                              │
│    Threshold:  -20.0 dB      Ratio:    4.0:1                                                 │
│    Attack:      10.0 ms      Release: 100.0 ms                                               │
│    Makeup:       4.0 dB                                                                      │
│                                                                                              │
│    Input Level   ████████████████████░░░░░░░░░░░░░░░░░░░░  -12.3 dB                          │
│    Gain Reduc.   ░░░░░░░░░░░░░░░░████████░░░░░░░░░░░░░░░   -6.2 dB                          │
│    Output Level  ██████████████████████████░░░░░░░░░░░░░░   -8.1 dB                          │
│                                                                                              │
│  Tab: switch chain · ↑/↓: navigate · ←/→: adjust · p: save profile · l: load · Esc: back    │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

**Live meters update every frame (20ms) via a tick message:**
- **Input Level** — RMS energy of audio entering the compressor (green bars)
- **Gain Reduction** — how much the compressor is attenuating (yellow bars, grows from right-to-left to show compression amount)
- **Output Level** — RMS energy after compression + makeup gain (blue bars)

The meter bars are 40 characters wide. Each character = 1.5dB of range (total -60dB to 0dB). The bars use block characters (`█` for filled, `░` for empty) with color coding:
- Green: signal in normal range
- Yellow: approaching threshold
- Red: clipping (> -1dB)

The EQ visualization shows a vertical bar chart of the 7 band gains, using `██` block characters scaled to the ±12dB range. The selected band is highlighted with the selection color. Left/right arrows adjust the selected band by 0.5dB steps.

**Monitor mode:** When enabled (toggle with `v` key), the meters are live and updating. When disabled, meters freeze showing the last values. Monitor mode is on by default during a call. The meter tick uses a `tea.Tick` at 50ms intervals (20fps) for smooth visualization.

### Taskbar Badge

When a call is active and the overlay is closed (user went back to chat), a badge appears in the top bar:

```
📞 Call 2:34
```

Positioned right of the downloads badge. Clicking it reopens the call overlay.

### Call State on Model

```go
// On the Model struct:
audioEngine    *audio.Engine       // nil when no call active
activeCall     *ActiveCall         // nil when no call
audioCallModel AudioCallModel      // overlay state

type ActiveCall struct {
    CallID     string
    PeerID     string              // friend's slacker ID
    PeerName   string              // display name
    StartTime  time.Time
    Muted      bool
    PeerMuted  bool
    State      CallState           // Ringing, Active, Ending
}
```

### Shortcut

```json
"audio_call": ["alt+p"]
```

Opens the call overlay if a call is active. If no call is active, shows "No active call" in the status bar.

### Sidebar Integration

In `buildSidebarOptionsItems`, for friend channels:

```go
items = append(items, sidebarOptionsItem{
    label: "Start Audio Call", action: SidebarActionStartAudioCall,
})
```

Handling in `SidebarOptionsSelectMsg`:
- Create a `CallID` (UUID)
- Send `MsgTypeCallRequest` to the friend
- Open the call overlay in "ringing outgoing" state
- Start a ring timeout (30s — auto-cancel if no response)

## Phase C: Audio Effects

### Effect Chain

```go
type EffectChain struct {
    eq         *Equalizer
    compressor *Compressor
    eqEnabled  bool
    compEnabled bool
}

// Process applies the enabled effects to a buffer of float32 PCM samples.
func (c *EffectChain) Process(samples []float32) {
    if c.eqEnabled {
        c.eq.Process(samples)
    }
    if c.compEnabled {
        c.compressor.Process(samples)
    }
}
```

### Equalizer

7-band parametric EQ with shelving filters on the ends and peaking filters in the middle:

```go
type Equalizer struct {
    bands [7]EQBand
}

type EQBand struct {
    Label    string        // "100Hz", "250Hz", etc.
    Freq     float64       // center/corner frequency in Hz
    Gain     float32       // -12.0 to +12.0 dB, 0.5dB steps
    Type     BiquadType    // LowShelf, Peaking, or HighShelf
    filter   BiquadFilter  // computed coefficients
}

// Default band layout:
// Band 0: 100 Hz   — Low Shelf
// Band 1: 250 Hz   — Peaking
// Band 2: 500 Hz   — Peaking
// Band 3: 1 kHz    — Peaking
// Band 4: 3 kHz    — Peaking
// Band 5: 7 kHz    — Peaking
// Band 6: 12 kHz   — High Shelf
```

Each band: -12dB to +12dB gain, 0.5dB steps. Q factor fixed at 1.0 for peaking bands (moderate width). Biquad filter coefficients are computed from the Audio EQ Cookbook (Robert Bristow-Johnson). Coefficients are recalculated only when the gain changes, not per-sample.

### Compressor

Feed-forward compressor with live metering:

```go
type Compressor struct {
    Threshold  float32  // dB (-60 to 0)
    Ratio      float32  // 1:1 to 20:1
    Attack     float32  // ms (0.1 to 100)
    Release    float32  // ms (10 to 1000)
    MakeupGain float32  // dB (0 to 24) — auto or manual
    envelope   float32  // current envelope follower state

    // Live metering (updated per-frame, read by UI tick).
    // These are smoothed peak values for display.
    InputLevel     float32  // dBFS, pre-compression
    GainReduction  float32  // dB of gain reduction being applied (always <= 0)
    OutputLevel    float32  // dBFS, post-compression + makeup
}
```

Per-sample processing: envelope follower → gain computation → gain application. ~15 lines. The metering values are updated once per frame (960 samples) as the RMS of the frame, smoothed with a simple peak-hold + decay (decay rate = 20dB/s for responsive yet readable meters).

### Dual Chains

Two independent `EffectChain` instances:
- **Outgoing** — applied to mic audio before Opus encoding. All peers hear the processed audio.
- **Incoming** — applied to decoded audio before playback. Only local user hears the processing.

### Effect Profiles

Stored as JSON at `~/.config/slackers/audio_profiles.json`:

```json
[
  {
    "name": "Default",
    "outgoing": {
      "eq_enabled": true,
      "eq_bands": [3.0, 4.5, 0.0, 0.0, -3.0, 0.0, -2.0],
      "comp_enabled": true,
      "comp_threshold": -20.0,
      "comp_ratio": 4.0,
      "comp_attack": 10.0,
      "comp_release": 100.0,
      "comp_makeup": 4.0
    },
    "incoming": {
      "eq_enabled": false,
      "eq_bands": [0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0],
      "comp_enabled": false,
      "comp_threshold": -20.0,
      "comp_ratio": 2.0,
      "comp_attack": 10.0,
      "comp_release": 100.0,
      "comp_makeup": 0.0
    }
  }
]
```

The `eq_bands` array maps to [100Hz, 250Hz, 500Hz, 1kHz, 3kHz, 7kHz, 12kHz]. Each value is dB gain (-12.0 to +12.0).

Profiles are loaded on app start and saved on change. The active profile name is stored in the global config.

## Phase D: Polish

### Voice Activity Detection (VAD)

Simple energy-based VAD:
- Compute RMS energy of each frame
- If energy < threshold for >300ms, mark as silence
- During silence: send silence indicator frames (1 byte) instead of full Opus frames
- Saves ~90% bandwidth during pauses in conversation

### Echo Cancellation

Not implemented in Phase D — it requires a full AEC algorithm (Speex or WebRTC). Instead, recommend users wear headphones. The compressor's noise gate behavior (high threshold) provides basic feedback suppression.

### Call Quality Metrics

Track and display in the call overlay:
- Packet loss % (rolling 10s window)
- Round-trip latency estimate (derived from ping/pong timestamps)
- Jitter (variance in frame arrival times)
- Bitrate (actual encoded bitrate)

### Call History

Store last 50 calls in `~/.config/slackers/call_history.json`:

```json
{
  "calls": [
    {
      "call_id": "...",
      "peer_id": "...",
      "peer_name": "Ryan Weiss",
      "started": "2026-04-13T15:00:00Z",
      "duration_seconds": 154,
      "direction": "outgoing"
    }
  ]
}
```

Viewable from a `/calls` command or from the friend details overlay.

## Implementation Phases

### Phase A: Basic Audio Streaming (MVP)
1. Add `malgo` and `hraban/opus` dependencies
2. Create `internal/audio/engine.go` — capture, encode, decode, playback
3. Create `internal/audio/codec.go` — Opus wrapper
4. Create `internal/audio/jitter.go` — jitter buffer
5. Add call signaling message types to p2p.go
6. Add `P2PAudioProtocol` stream handler
7. Wire into Model: call request/accept/reject/end flow
8. Basic console logging of call state (no UI yet)

### Phase B: Call UI
9. Create `internal/tui/audiocall.go` — overlay with ringing/active/effects states
10. Create `internal/tui/audiocall_taskbar.go` — active call badge
11. Add `overlayAudioCall` + `overlayIncomingCall` to overlay enum
12. Add `alt+p` shortcut for opening call overlay
13. Add "Start Audio Call" to sidebar options for friends
14. Handle incoming call notification (ring prompt)
15. Wire call timer tick for duration display

### Phase C: Audio Effects
16. Create `internal/audio/effects.go` — EffectChain with 7-band Equalizer + Compressor
17. Create `internal/audio/biquad.go` — Biquad filter math (low shelf, peaking, high shelf)
18. Create `internal/audio/metering.go` — RMS level computation, peak hold, gain reduction tracking
19. Create `internal/audio/profile.go` — profile load/save/list
20. Add effects settings sub-screen to call overlay with EQ bar chart + live meters
21. Wire dual chains (outgoing + incoming) into engine
22. Add monitor mode tick (50ms/20fps) for live meter updates
23. Add profile management (save/load/select) UI

### Phase D: Polish
21. Create `internal/audio/vad.go` — voice activity detection
22. Add call quality metrics to overlay
23. Add call history persistence + `/calls` command
24. Add mute notification to peer (MsgTypeCallMute)
25. Handle edge cases: peer disconnect, network interruption, device changes

## Error Handling

- **No mic access:** Show error in call overlay, offer to continue as listen-only
- **Peer offline:** Cancel call request after 5s, show "User offline"
- **Network drop during call:** Attempt reconnect for 10s, then end call
- **Audio device changes:** Detect via malgo device change callback, restart capture/playback
- **Opus encode failure:** Skip frame, log error, increment stats counter

## Config Additions

```json
{
  "audio_bitrate": 32000,
  "audio_jitter_ms": 50,
  "audio_profile": "Default",
  "audio_input_device": "",
  "audio_output_device": ""
}
```

Empty device strings = system default.

## Shortcut Additions

```json
{
  "audio_call": ["alt+p"],
  "audio_mute": ["alt+m"]
}
```

`alt+m` toggles mute globally (works even when call overlay is closed).
