# Audio Calling System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable P2P voice calls between friends in slackers with 7-band EQ, compressor with live metering, call management UI, and persistent effect profiles.

**Architecture:** A new `internal/audio/` package handles audio capture/playback (malgo), Opus encoding/decoding, jitter buffering, and DSP effects. Call signaling uses existing P2P message types. Audio data streams over a dedicated `/slackers/audio/1.0.0` libp2p protocol with binary Opus frames. The TUI gets an `AudioCallModel` overlay for call management and effects settings, plus a taskbar badge for active calls.

**Tech Stack:** Go 1.25.7, malgo (miniaudio CGo), hraban/opus (CGo, requires libopus-dev), libp2p v0.48.0, Bubbletea

**Spec:** `docs/superpowers/specs/2026-04-13-audio-calls-design.md`

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `internal/audio/engine.go` | Audio engine: device init, capture/playback loops, stream I/O coordination |
| `internal/audio/codec.go` | Opus encoder/decoder wrapper with configurable bitrate/frame size |
| `internal/audio/jitter.go` | Jitter buffer: reorder, gap-fill, deliver frames at steady rate |
| `internal/audio/effects.go` | EffectChain, 7-band Equalizer, Compressor with live metering |
| `internal/audio/biquad.go` | Biquad filter math (low shelf, peaking, high shelf coefficients) |
| `internal/audio/metering.go` | RMS level computation, peak hold with decay |
| `internal/audio/profile.go` | Effect profile persistence (load/save/list JSON) |
| `internal/audio/vad.go` | Voice Activity Detection (energy-based silence suppression) |
| `internal/audio/engine_test.go` | Tests for codec, jitter buffer, biquad, effects, metering |
| `internal/tui/audiocall.go` | AudioCallModel overlay (ringing, active, effects sub-screens) |
| `internal/tui/audiocall_taskbar.go` | Active call taskbar badge rendering + click area |

### Modified files

| File | Changes |
|------|---------|
| `go.mod` | Add `github.com/gen2brain/malgo`, `github.com/hraban/opus` |
| `internal/secure/p2p.go` | Add call signaling message types, `P2PAudioProtocol`, audio stream handler |
| `internal/tui/model.go` | Add `overlayAudioCall`, `overlayIncomingCall`, audio fields, shortcut dispatch, message routing |
| `internal/tui/keymap.go` | Add `AudioCall`, `AudioMute` key bindings |
| `internal/shortcuts/defaults.json` | Add `"audio_call": ["alt+p"]`, `"audio_mute": ["alt+m"]` |
| `internal/tui/sidebaroptions.go` | Add `SidebarActionStartAudioCall` |
| `internal/tui/handlers_ui.go` | Handle `SidebarActionStartAudioCall` in sidebar options dispatch |
| `internal/config/config.go` | Add `AudioBitrate`, `AudioJitterMs`, `AudioProfile`, `AudioInputDevice`, `AudioOutputDevice` |

---

## Phase A: Call Signaling + Basic Audio Streaming

### Task 1: Add audio dependencies

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Install system dependency**

```bash
sudo dnf install opus-devel   # Fedora
# or: sudo apt install libopus-dev  # Debian/Ubuntu
```

- [ ] **Step 2: Add Go modules**

```bash
cd /home/rw3iss/Sites/others/tools/slackers
go get github.com/gen2brain/malgo
go get github.com/hraban/opus
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no errors (CGo compiles malgo + opus bindings)

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add malgo (audio I/O) and hraban/opus (codec)"
```

---

### Task 2: Create Opus codec wrapper

**Files:**
- Create: `internal/audio/codec.go`

- [ ] **Step 1: Create the codec package**

```go
// internal/audio/codec.go
package audio

import (
	"fmt"

	"github.com/hraban/opus"
)

const (
	SampleRate = 48000
	Channels   = 1
	FrameSize  = 960 // 20ms at 48kHz
)

// Codec wraps Opus encoder and decoder.
type Codec struct {
	Encoder *opus.Encoder
	Decoder *opus.Decoder
	Bitrate int
}

// NewCodec creates an Opus encoder/decoder pair.
func NewCodec(bitrate int) (*Codec, error) {
	if bitrate <= 0 {
		bitrate = 32000
	}
	enc, err := opus.NewEncoder(SampleRate, Channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("opus encoder: %w", err)
	}
	if err := enc.SetBitrate(bitrate); err != nil {
		return nil, fmt.Errorf("opus set bitrate: %w", err)
	}
	dec, err := opus.NewDecoder(SampleRate, Channels)
	if err != nil {
		return nil, fmt.Errorf("opus decoder: %w", err)
	}
	return &Codec{Encoder: enc, Decoder: dec, Bitrate: bitrate}, nil
}

// Encode encodes a frame of PCM int16 samples to Opus.
// Returns the encoded bytes. buf is a reusable output buffer.
func (c *Codec) Encode(pcm []int16, buf []byte) (int, error) {
	n, err := c.Encoder.Encode(pcm, buf)
	if err != nil {
		return 0, fmt.Errorf("opus encode: %w", err)
	}
	return n, nil
}

// Decode decodes an Opus frame to PCM int16 samples.
// If data is nil, performs PLC (packet loss concealment).
func (c *Codec) Decode(data []byte, pcm []int16) (int, error) {
	n, err := c.Decoder.Decode(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}
	return n, nil
}

// DecodePLC generates a concealment frame for a lost packet.
func (c *Codec) DecodePLC(pcm []int16) (int, error) {
	n, err := c.Decoder.Decode(nil, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus plc: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/audio/...`

- [ ] **Step 3: Commit**

```bash
git add internal/audio/codec.go
git commit -m "feat(audio): Opus codec wrapper with encode/decode/PLC"
```

---

### Task 3: Create jitter buffer

**Files:**
- Create: `internal/audio/jitter.go`

- [ ] **Step 1: Implement the jitter buffer**

```go
// internal/audio/jitter.go
package audio

import "sync"

// JitterBuffer reorders and smooths incoming audio frames.
// It holds a fixed number of slots indexed by sequence number.
// The playback side pops frames in order, using PLC for gaps.
type JitterBuffer struct {
	mu       sync.Mutex
	slots    map[uint16][]byte // seq → opus frame
	nextSeq  uint16            // next sequence to deliver
	depth    int               // target buffer depth in frames
	seeded   bool              // first frame received
}

// NewJitterBuffer creates a buffer with the given depth (in frames).
// depth=3 means ~60ms at 20ms/frame.
func NewJitterBuffer(depth int) *JitterBuffer {
	if depth < 1 {
		depth = 3
	}
	return &JitterBuffer{
		slots: make(map[uint16][]byte, depth*2),
		depth: depth,
	}
}

// Push adds an incoming frame. seq is the sender's frame counter.
func (j *JitterBuffer) Push(seq uint16, data []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.seeded {
		j.nextSeq = seq
		j.seeded = true
	}
	// Copy data to avoid aliasing the network buffer.
	frame := make([]byte, len(data))
	copy(frame, data)
	j.slots[seq] = frame
	// Discard stale frames (more than 2*depth behind nextSeq).
	limit := j.depth * 2
	for s := range j.slots {
		if seqDist(s, j.nextSeq) > uint16(limit) {
			delete(j.slots, s)
		}
	}
}

// Pop returns the next frame in sequence, or nil if missing (gap).
// The caller should use PLC when nil is returned.
func (j *JitterBuffer) Pop() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	data, ok := j.slots[j.nextSeq]
	if ok {
		delete(j.slots, j.nextSeq)
	}
	j.nextSeq++
	if ok {
		return data
	}
	return nil // gap — caller should PLC
}

// Ready returns true when the buffer has accumulated enough frames
// to start playback (initial fill).
func (j *JitterBuffer) Ready() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.slots) >= j.depth
}

// Reset clears all buffered frames.
func (j *JitterBuffer) Reset() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.slots = make(map[uint16][]byte, j.depth*2)
	j.seeded = false
}

// seqDist computes the forward distance between two uint16 sequence
// numbers, handling wraparound.
func seqDist(a, b uint16) uint16 {
	return a - b // unsigned subtraction wraps correctly
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/audio/...`

- [ ] **Step 3: Commit**

```bash
git add internal/audio/jitter.go
git commit -m "feat(audio): jitter buffer with sequence reordering and gap detection"
```

---

### Task 4: Add call signaling to P2P

**Files:**
- Modify: `internal/secure/p2p.go`

- [ ] **Step 1: Add message type constants**

After the existing `MsgTypePlugin` constant, add:

```go
MsgTypeCallRequest = "call_request"
MsgTypeCallAccept  = "call_accept"
MsgTypeCallReject  = "call_reject"
MsgTypeCallEnd     = "call_end"
MsgTypeCallMute    = "call_mute"
```

- [ ] **Step 2: Add P2PAudioProtocol**

After the existing `P2PFileProtocol` constant:

```go
P2PAudioProtocol = protocol.ID("/slackers/audio/1.0.0")
```

- [ ] **Step 3: Add CallID and CallerName fields to P2PMessage**

Add to the `P2PMessage` struct:

```go
// Audio call fields
CallID     string `json:"call_id,omitempty"`
CallerName string `json:"caller_name,omitempty"`
CallMuted  bool   `json:"call_muted,omitempty"`
```

- [ ] **Step 4: Add audio stream handler registration**

In `NewP2PNode`, after the file protocol handler:

```go
h.SetStreamHandler(P2PAudioProtocol, node.handleAudioStream)
```

Add the handler method:

```go
// handleAudioStream is a placeholder for the audio stream receiver.
// The actual audio processing is wired by the TUI via SetAudioStreamHandler.
func (n *P2PNode) handleAudioStream(s network.Stream) {
	n.mu.RLock()
	handler := n.onAudioStream
	n.mu.RUnlock()
	if handler != nil {
		handler(s)
	} else {
		s.Close()
	}
}
```

Add to P2PNode struct:

```go
onAudioStream func(network.Stream)
```

Add setter:

```go
func (n *P2PNode) SetAudioStreamHandler(h func(network.Stream)) {
	n.mu.Lock()
	n.onAudioStream = h
	n.mu.Unlock()
}
```

- [ ] **Step 5: Add method to open an audio stream to a peer**

```go
// OpenAudioStream opens a bidirectional audio stream to the given peer.
func (n *P2PNode) OpenAudioStream(slackUserID string) (network.Stream, error) {
	n.mu.RLock()
	peerID, ok := n.peerMap[slackUserID]
	n.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("peer not found: %s", slackUserID)
	}
	return n.host.NewStream(n.ctx, peerID, P2PAudioProtocol)
}
```

- [ ] **Step 6: Add SendCallSignal helper**

```go
// SendCallSignal sends a call control message (request/accept/reject/end/mute).
func (n *P2PNode) SendCallSignal(slackUserID string, msgType, callID, callerName string, muted bool) error {
	msg := P2PMessage{
		Type:       msgType,
		CallID:     callID,
		CallerName: callerName,
		CallMuted:  muted,
		Timestamp:  time.Now().Unix(),
	}
	return n.SendMessage(slackUserID, msg)
}
```

- [ ] **Step 7: Verify build**

Run: `go build ./...`

- [ ] **Step 8: Commit**

```bash
git add internal/secure/p2p.go
git commit -m "feat(p2p): call signaling messages and audio stream protocol"
```

---

### Task 5: Create audio engine

**Files:**
- Create: `internal/audio/engine.go`

- [ ] **Step 1: Create the engine**

```go
// internal/audio/engine.go
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

// Engine manages audio capture, encoding, decoding, and playback for a call.
type Engine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	codec     *Codec
	jitter    *JitterBuffer
	outgoing  *EffectChain
	incoming  *EffectChain
	muted     atomic.Bool
	seqOut    uint16 // outgoing frame sequence counter
	outStream io.Writer
	inStream  io.Reader

	// Metering values readable by the UI.
	OutMeter  Meter
	InMeter   Meter

	mu        sync.Mutex
	running   bool
	malgoCtx  *malgo.AllocatedContext
	captureDevice  *malgo.Device
	playbackDevice *malgo.Device

	// Capture buffer accumulator (malgo delivers variable-size chunks).
	capBuf    []int16
}

// Meter holds the latest metering snapshot for UI display.
type Meter struct {
	InputLevel    float32
	GainReduction float32
	OutputLevel   float32
}

// NewEngine creates an audio engine with the given codec bitrate and jitter depth.
func NewEngine(bitrate, jitterDepthMs int) (*Engine, error) {
	codec, err := NewCodec(bitrate)
	if err != nil {
		return nil, err
	}
	jitterFrames := jitterDepthMs / 20
	if jitterFrames < 1 {
		jitterFrames = 3
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		ctx:      ctx,
		cancel:   cancel,
		codec:    codec,
		jitter:   NewJitterBuffer(jitterFrames),
		outgoing: NewEffectChain(),
		incoming: NewEffectChain(),
	}, nil
}

// SetStreams sets the outgoing writer and incoming reader for audio data.
func (e *Engine) SetStreams(out io.Writer, in io.Reader) {
	e.mu.Lock()
	e.outStream = out
	e.inStream = in
	e.mu.Unlock()
}

// SetMuted toggles mic muting.
func (e *Engine) SetMuted(m bool) { e.muted.Store(m) }
func (e *Engine) IsMuted() bool   { return e.muted.Load() }

// OutgoingEffects returns the outgoing effect chain for configuration.
func (e *Engine) OutgoingEffects() *EffectChain { return e.outgoing }
// IncomingEffects returns the incoming effect chain for configuration.
func (e *Engine) IncomingEffects() *EffectChain { return e.incoming }

// Start begins audio capture and playback.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return nil
	}

	mCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("malgo init context: %w", err)
	}
	e.malgoCtx = mCtx

	// Start capture device.
	if err := e.startCapture(); err != nil {
		mCtx.Uninit()
		mCtx.Free()
		return err
	}

	// Start playback device.
	if err := e.startPlayback(); err != nil {
		e.captureDevice.Stop()
		e.captureDevice.Uninit()
		mCtx.Uninit()
		mCtx.Free()
		return err
	}

	// Start the incoming frame reader goroutine.
	go e.readIncomingFrames()

	e.running = true
	return nil
}

// Stop tears down audio devices and cancels the engine context.
func (e *Engine) Stop() {
	e.cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	if e.captureDevice != nil {
		e.captureDevice.Stop()
		e.captureDevice.Uninit()
	}
	if e.playbackDevice != nil {
		e.playbackDevice.Stop()
		e.playbackDevice.Uninit()
	}
	if e.malgoCtx != nil {
		e.malgoCtx.Uninit()
		e.malgoCtx.Free()
	}
	e.running = false
}

func (e *Engine) startCapture() error {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = Channels
	deviceConfig.SampleRate = SampleRate

	capCallbacks := malgo.DeviceCallbacks{
		Data: e.onCaptureData,
	}

	dev, err := malgo.InitDevice(e.malgoCtx.Context, deviceConfig, capCallbacks)
	if err != nil {
		return fmt.Errorf("malgo capture init: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return fmt.Errorf("malgo capture start: %w", err)
	}
	e.captureDevice = dev
	return nil
}

func (e *Engine) startPlayback() error {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = Channels
	deviceConfig.SampleRate = SampleRate

	playCallbacks := malgo.DeviceCallbacks{
		Data: e.onPlaybackData,
	}

	dev, err := malgo.InitDevice(e.malgoCtx.Context, deviceConfig, playCallbacks)
	if err != nil {
		return fmt.Errorf("malgo playback init: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return fmt.Errorf("malgo playback start: %w", err)
	}
	e.playbackDevice = dev
	return nil
}

// onCaptureData is called by malgo when mic samples are available.
func (e *Engine) onCaptureData(outputSamples, inputSamples []byte, frameCount uint32) {
	// Convert bytes to int16 samples.
	sampleCount := int(frameCount) * Channels
	samples := make([]int16, sampleCount)
	for i := 0; i < sampleCount && i*2+1 < len(inputSamples); i++ {
		samples[i] = int16(inputSamples[i*2]) | int16(inputSamples[i*2+1])<<8
	}

	// Accumulate until we have a full frame.
	e.capBuf = append(e.capBuf, samples...)
	for len(e.capBuf) >= FrameSize {
		frame := e.capBuf[:FrameSize]
		e.capBuf = e.capBuf[FrameSize:]
		e.processAndSendFrame(frame)
	}
}

func (e *Engine) processAndSendFrame(pcm []int16) {
	// Apply outgoing effects.
	floats := int16ToFloat32(pcm)
	e.outgoing.Process(floats)
	float32ToInt16(floats, pcm)

	// Encode to Opus.
	buf := make([]byte, 256)
	n, err := e.codec.Encode(pcm, buf)
	if err != nil {
		return
	}

	// Write to stream: [2 byte len][opus data]
	e.mu.Lock()
	out := e.outStream
	e.mu.Unlock()
	if out == nil || e.muted.Load() {
		return
	}

	header := make([]byte, 4) // 2 bytes seq + 2 bytes len
	binary.BigEndian.PutUint16(header[0:2], e.seqOut)
	binary.BigEndian.PutUint16(header[2:4], uint16(n))
	e.seqOut++
	out.Write(header)
	out.Write(buf[:n])
}

// readIncomingFrames reads Opus frames from the P2P stream and pushes
// them into the jitter buffer.
func (e *Engine) readIncomingFrames() {
	header := make([]byte, 4)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		e.mu.Lock()
		in := e.inStream
		e.mu.Unlock()
		if in == nil {
			return
		}
		if _, err := io.ReadFull(in, header); err != nil {
			return
		}
		seq := binary.BigEndian.Uint16(header[0:2])
		frameLen := binary.BigEndian.Uint16(header[2:4])
		if frameLen > 1000 { // sanity check
			return
		}
		data := make([]byte, frameLen)
		if _, err := io.ReadFull(in, data); err != nil {
			return
		}
		e.jitter.Push(seq, data)
	}
}

// onPlaybackData is called by malgo when it needs samples for the speaker.
func (e *Engine) onPlaybackData(outputSamples, inputSamples []byte, frameCount uint32) {
	sampleCount := int(frameCount) * Channels
	pcm := make([]int16, sampleCount)

	// Fill from jitter buffer, one Opus frame at a time.
	filled := 0
	for filled+FrameSize <= sampleCount {
		frame := e.jitter.Pop()
		var n int
		if frame != nil {
			n, _ = e.codec.Decode(frame, pcm[filled:filled+FrameSize])
		} else {
			n, _ = e.codec.DecodePLC(pcm[filled:filled+FrameSize])
		}
		if n <= 0 {
			n = FrameSize
		}
		filled += n
	}

	// Apply incoming effects.
	floats := int16ToFloat32(pcm[:filled])
	e.incoming.Process(floats)
	float32ToInt16(floats, pcm[:filled])

	// Write to output buffer.
	for i := 0; i < filled && i*2+1 < len(outputSamples); i++ {
		outputSamples[i*2] = byte(pcm[i])
		outputSamples[i*2+1] = byte(pcm[i] >> 8)
	}
}

// int16ToFloat32 converts PCM int16 samples to float32 in [-1.0, 1.0].
func int16ToFloat32(pcm []int16) []float32 {
	out := make([]float32, len(pcm))
	for i, s := range pcm {
		out[i] = float32(s) / 32768.0
	}
	return out
}

// float32ToInt16 converts float32 samples back to int16, clamping.
func float32ToInt16(f []float32, pcm []int16) {
	for i, s := range f {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		pcm[i] = int16(s * 32767.0)
	}
}
```

- [ ] **Step 2: Create stub EffectChain for compilation**

Create a minimal `internal/audio/effects.go` so the engine compiles:

```go
// internal/audio/effects.go
package audio

// EffectChain applies audio effects to PCM samples.
// Full implementation in Phase C.
type EffectChain struct{}

// NewEffectChain creates a pass-through effect chain.
func NewEffectChain() *EffectChain { return &EffectChain{} }

// Process applies effects in-place. Currently a no-op.
func (c *EffectChain) Process(samples []float32) {}
```

- [ ] **Step 3: Verify build**

Run: `go build ./internal/audio/...`

- [ ] **Step 4: Commit**

```bash
git add internal/audio/engine.go internal/audio/effects.go
git commit -m "feat(audio): audio engine with malgo capture/playback and Opus streaming"
```

---

### Task 6: Write audio tests

**Files:**
- Create: `internal/audio/engine_test.go`

- [ ] **Step 1: Write tests for codec and jitter buffer**

```go
// internal/audio/engine_test.go
package audio

import (
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	codec, err := NewCodec(32000)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	// Create a simple tone frame.
	pcm := make([]int16, FrameSize)
	for i := range pcm {
		pcm[i] = int16(i % 1000)
	}
	buf := make([]byte, 512)
	n, err := codec.Encode(pcm, buf)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if n <= 0 {
		t.Fatal("Encode returned 0 bytes")
	}
	out := make([]int16, FrameSize)
	dn, err := codec.Decode(buf[:n], out)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if dn != FrameSize {
		t.Fatalf("Decode returned %d samples, want %d", dn, FrameSize)
	}
}

func TestCodecPLC(t *testing.T) {
	codec, err := NewCodec(32000)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	// Encode a real frame first so the decoder has state.
	pcm := make([]int16, FrameSize)
	buf := make([]byte, 512)
	codec.Encode(pcm, buf)
	out := make([]int16, FrameSize)
	codec.Decode(buf[:10], out) // feed something
	// Now PLC.
	n, err := codec.DecodePLC(out)
	if err != nil {
		t.Fatalf("PLC: %v", err)
	}
	if n != FrameSize {
		t.Fatalf("PLC returned %d samples, want %d", n, FrameSize)
	}
}

func TestJitterBufferOrdering(t *testing.T) {
	jb := NewJitterBuffer(2)
	// Push out of order: 2, 0, 1
	jb.Push(2, []byte{0x02})
	jb.Push(0, []byte{0x00})
	jb.Push(1, []byte{0x01})
	// Pop should deliver 0, 1, 2
	f0 := jb.Pop()
	if f0 == nil || f0[0] != 0x00 {
		t.Fatalf("expected seq 0, got %v", f0)
	}
	f1 := jb.Pop()
	if f1 == nil || f1[0] != 0x01 {
		t.Fatalf("expected seq 1, got %v", f1)
	}
	f2 := jb.Pop()
	if f2 == nil || f2[0] != 0x02 {
		t.Fatalf("expected seq 2, got %v", f2)
	}
}

func TestJitterBufferGap(t *testing.T) {
	jb := NewJitterBuffer(2)
	jb.Push(0, []byte{0x00})
	// Skip seq 1
	jb.Push(2, []byte{0x02})
	f0 := jb.Pop() // seq 0
	if f0 == nil {
		t.Fatal("seq 0 should be present")
	}
	f1 := jb.Pop() // seq 1 — missing
	if f1 != nil {
		t.Fatal("seq 1 should be nil (gap)")
	}
	f2 := jb.Pop() // seq 2
	if f2 == nil {
		t.Fatal("seq 2 should be present")
	}
}

func TestInt16Float32RoundTrip(t *testing.T) {
	pcm := []int16{0, 16383, -16384, 32767, -32768}
	f := int16ToFloat32(pcm)
	out := make([]int16, len(pcm))
	float32ToInt16(f, out)
	for i, want := range pcm {
		diff := int(out[i]) - int(want)
		if diff > 1 || diff < -1 {
			t.Fatalf("sample %d: got %d, want %d (diff %d)", i, out[i], want, diff)
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/audio/ -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add internal/audio/engine_test.go
git commit -m "test(audio): codec round-trip, jitter buffer ordering/gaps, PCM conversion"
```

---

### Task 7: Add audio config fields

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/shortcuts/defaults.json`

- [ ] **Step 1: Add config fields**

Add to the `Config` struct in `internal/config/config.go`:

```go
// Audio call settings.
AudioBitrate      int    `json:"audio_bitrate,omitempty"`
AudioJitterMs     int    `json:"audio_jitter_ms,omitempty"`
AudioProfile      string `json:"audio_profile,omitempty"`
AudioInputDevice  string `json:"audio_input_device,omitempty"`
AudioOutputDevice string `json:"audio_output_device,omitempty"`
```

- [ ] **Step 2: Add shortcuts**

Add to `internal/shortcuts/defaults.json`:

```json
"audio_call": ["alt+p"],
"audio_mute": ["alt+m"]
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/shortcuts/defaults.json
git commit -m "feat(config): audio call settings and alt+p/alt+m shortcuts"
```

---

### Task 8: Wire call signaling into Model

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/sidebaroptions.go`
- Modify: `internal/tui/handlers_ui.go`

- [ ] **Step 1: Add overlay constants and Model fields**

In `model.go`, add to the overlay enum after `overlayWorkspaceEdit`:

```go
overlayAudioCall
overlayIncomingCall
```

Add fields to Model struct:

```go
// Audio call state.
audioEngine    *audio.Engine
activeCall     *ActiveCall
audioCallModel AudioCallModel
```

Add the `ActiveCall` type (can go near the top of model.go or in a new section):

```go
type CallState int
const (
	CallStateRinging CallState = iota
	CallStateActive
	CallStateEnding
)

type ActiveCall struct {
	CallID    string
	PeerID    string
	PeerName  string
	StartTime time.Time
	Muted     bool
	PeerMuted bool
	State     CallState
	Outgoing  bool // true = we initiated
}
```

- [ ] **Step 2: Add key bindings**

In `internal/tui/keymap.go`, add to the KeyMap struct:

```go
AudioCall key.Binding
AudioMute key.Binding
```

In `BuildKeyMap`, add:

```go
AudioCall: binding(sm, "audio_call", "alt+p", "open active call"),
AudioMute: binding(sm, "audio_mute", "alt+m", "toggle mute"),
```

- [ ] **Step 3: Add sidebar option**

In `internal/tui/sidebaroptions.go`, add to the enum:

```go
SidebarActionStartAudioCall
```

- [ ] **Step 4: Add "Start Audio Call" to friend context menu**

In `model.go`'s `buildSidebarOptionsItems`, in the friend channel section (after "Browse Shared Files"):

```go
items = append(items, sidebarOptionsItem{
	label: "Start Audio Call", action: SidebarActionStartAudioCall,
})
```

- [ ] **Step 5: Handle the sidebar action**

In `model.go`'s `SidebarOptionsSelectMsg` handler, add a case:

```go
case SidebarActionStartAudioCall:
	if m.activeCall != nil {
		m.warning = "Already in a call"
		return m, nil
	}
	// Initiate call to this friend.
	callID := generateCallID()
	friendName := m.resolveFriendName(msg.UserID)
	m.activeCall = &ActiveCall{
		CallID:   callID,
		PeerID:   msg.UserID,
		PeerName: friendName,
		State:    CallStateRinging,
		Outgoing: true,
	}
	m.audioCallModel = NewAudioCallModel(m.activeCall)
	m.audioCallModel.SetSize(m.width, m.height)
	m.overlay = overlayAudioCall
	// Send call request via P2P.
	if m.p2pNode != nil {
		m.p2pNode.SendCallSignal(msg.UserID, secure.MsgTypeCallRequest, callID, m.cfg.MyName, false)
	}
	return m, audioCallRingTimeoutCmd(callID)
```

Add helper:

```go
func generateCallID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 6: Handle incoming call signals in P2P message handler**

In the P2PReceivedMsg handler in model.go, add cases for call messages:

```go
case secure.MsgTypeCallRequest:
	if m.activeCall != nil {
		// Already in a call — auto-reject.
		m.p2pNode.SendCallSignal(msg.SenderID, secure.MsgTypeCallReject, msg.CallID, "", false)
		return m, nil
	}
	friendName := m.resolveFriendName(msg.SenderID)
	m.activeCall = &ActiveCall{
		CallID:   msg.CallID,
		PeerID:   msg.SenderID,
		PeerName: friendName,
		State:    CallStateRinging,
		Outgoing: false,
	}
	m.audioCallModel = NewAudioCallModel(m.activeCall)
	m.audioCallModel.SetSize(m.width, m.height)
	m.overlay = overlayIncomingCall
	return m, nil

case secure.MsgTypeCallAccept:
	if m.activeCall != nil && m.activeCall.CallID == msg.CallID {
		m.activeCall.State = CallStateActive
		m.activeCall.StartTime = time.Now()
		// Start audio engine and open streams.
		return m, startAudioEngineCmd(m)
	}

case secure.MsgTypeCallReject:
	if m.activeCall != nil && m.activeCall.CallID == msg.CallID {
		m.activeCall = nil
		m.overlay = overlayNone
		m.warning = msg.SenderName + " declined the call"
	}

case secure.MsgTypeCallEnd:
	if m.activeCall != nil && m.activeCall.CallID == msg.CallID {
		m.endCall()
	}

case secure.MsgTypeCallMute:
	if m.activeCall != nil && m.activeCall.CallID == msg.CallID {
		m.activeCall.PeerMuted = msg.CallMuted
	}
```

- [ ] **Step 7: Add endCall helper and startAudioEngineCmd**

```go
func (m *Model) endCall() {
	if m.audioEngine != nil {
		m.audioEngine.Stop()
		m.audioEngine = nil
	}
	m.activeCall = nil
	if m.overlay == overlayAudioCall || m.overlay == overlayIncomingCall {
		m.overlay = overlayNone
	}
}

func startAudioEngineCmd(m *Model) tea.Cmd {
	return func() tea.Msg {
		// Engine creation and stream setup will be implemented
		// as a tea.Msg when the streams are ready.
		return AudioEngineStartedMsg{}
	}
}

type AudioEngineStartedMsg struct{}
```

- [ ] **Step 8: Add Alt+P and Alt+M shortcut dispatch**

In the global shortcut section of model.go:

```go
case key.Matches(msg, m.keymap.AudioCall):
	if m.activeCall != nil {
		m.audioCallModel.SetSize(m.width, m.height)
		m.overlay = overlayAudioCall
	} else {
		m.warning = "No active call"
	}
	return m, nil

case key.Matches(msg, m.keymap.AudioMute):
	if m.activeCall != nil && m.audioEngine != nil {
		m.activeCall.Muted = !m.activeCall.Muted
		m.audioEngine.SetMuted(m.activeCall.Muted)
		if m.p2pNode != nil {
			m.p2pNode.SendCallSignal(m.activeCall.PeerID, secure.MsgTypeCallMute, m.activeCall.CallID, "", m.activeCall.Muted)
		}
	}
	return m, nil
```

- [ ] **Step 9: Verify build** (will have errors for AudioCallModel — Task 9 creates it)

- [ ] **Step 10: Commit**

```bash
git add internal/tui/model.go internal/tui/keymap.go internal/tui/sidebaroptions.go internal/tui/handlers_ui.go
git commit -m "feat(tui): call signaling, shortcuts, sidebar action, P2P message routing"
```

---

## Phase B: Call UI

### Task 9: Create AudioCallModel overlay

**Files:**
- Create: `internal/tui/audiocall.go`

- [ ] **Step 1: Create the call overlay**

Create `internal/tui/audiocall.go` with:

1. **Message types:** `AudioCallOpenMsg`, `AudioCallCloseMsg`, `AudioCallEndMsg`, `AudioCallAcceptMsg`, `AudioCallTimerTickMsg`

2. **AudioCallModel struct** with fields: `call *ActiveCall`, `width/height int`, `showEffects bool`, `effectsTab int` (0=outgoing, 1=incoming), `effectsSel int`, `timerStr string`

3. **NewAudioCallModel(call *ActiveCall)** constructor

4. **Update** handling:
   - Ringing outgoing: `Esc`/`Enter` → cancel call
   - Ringing incoming: `Enter` → accept, `Esc` → decline
   - Active: `m` → mute, `e` → toggle effects, `q` → end call, `Esc` → close overlay (call continues)
   - Effects sub-screen: `Tab` → switch chain, up/down navigate, left/right adjust values, `Esc` → back

5. **View** rendering for all three states (ringing outgoing, ringing incoming, active call with duration/status/controls)

6. Timer tick: `audioCallTimerTickCmd()` returns a `tea.Tick` at 1s intervals updating the duration display

Follow the patterns from `gameoverlay.go` for exclusive keyboard control and tick-driven updates.

- [ ] **Step 2: Verify build**

Run: `go build ./...`

- [ ] **Step 3: Commit**

```bash
git add internal/tui/audiocall.go
git commit -m "feat(tui): AudioCallModel overlay with ringing/active/effects states"
```

---

### Task 10: Create call taskbar badge

**Files:**
- Create: `internal/tui/audiocall_taskbar.go`

- [ ] **Step 1: Create the taskbar badge**

Follow the pattern from `downloads_taskbar.go`:

```go
// internal/tui/audiocall_taskbar.go
package tui

import "fmt"

func renderAudioCallButton(call *ActiveCall) string {
	if call == nil || call.State != CallStateActive {
		return ""
	}
	dur := time.Since(call.StartTime)
	min := int(dur.Minutes())
	sec := int(dur.Seconds()) % 60
	text := fmt.Sprintf(" 📞 %d:%02d ", min, sec)
	return lipgloss.NewStyle().
		Background(ColorAccent).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		Render(text)
}

func (m Model) audioCallButtonClickArea() (x0, x1, y int, visible bool) {
	if m.activeCall == nil || m.activeCall.State != CallStateActive {
		return 0, 0, 0, false
	}
	btn := renderAudioCallButton(m.activeCall)
	btnW := lipgloss.Width(btn)
	if btnW == 0 {
		return 0, 0, 0, false
	}
	// Position right of downloads button.
	paneEnd := m.width - 2
	if m.backgroundGame != nil {
		paneEnd -= lipgloss.Width(m.renderGameTaskbarButton()) + 1
	}
	if m.downloadMgr != nil && m.downloadMgr.ActiveCount() > 0 {
		paneEnd -= lipgloss.Width(renderDownloadsButton(m.downloadMgr)) + 1
	}
	x := paneEnd - btnW
	if x < 0 {
		x = 0
	}
	return x, x + btnW, 0, true
}
```

- [ ] **Step 2: Wire into View rendering**

In model.go's `renderBaseView` or wherever the notification/download badges are composited, add the audio call badge using the same `overlayOnRow` pattern.

- [ ] **Step 3: Wire click handler**

In the mouse handler, add a click area check for the audio call button — clicking it opens `overlayAudioCall`.

- [ ] **Step 4: Verify build**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/tui/audiocall_taskbar.go internal/tui/model.go
git commit -m "feat(tui): active call taskbar badge with click-to-open"
```

---

### Task 11: Add overlay dispatch and View routing

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add overlay key routing**

In the key handler dispatch section (where other overlays are routed):

```go
if m.overlay == overlayAudioCall || m.overlay == overlayIncomingCall {
	var cmd tea.Cmd
	m.audioCallModel, cmd = m.audioCallModel.Update(msg)
	return m, cmd
}
```

- [ ] **Step 2: Add View routing**

In the overlay View switch:

```go
case overlayAudioCall, overlayIncomingCall:
	return m.audioCallModel.View()
```

- [ ] **Step 3: Handle AudioCallModel messages**

Add cases for the model's outgoing messages:

```go
case AudioCallEndMsg:
	if m.activeCall != nil && m.p2pNode != nil {
		m.p2pNode.SendCallSignal(m.activeCall.PeerID, secure.MsgTypeCallEnd, m.activeCall.CallID, "", false)
	}
	m.endCall()
	return m, nil

case AudioCallAcceptMsg:
	if m.activeCall != nil && m.p2pNode != nil {
		m.p2pNode.SendCallSignal(m.activeCall.PeerID, secure.MsgTypeCallAccept, m.activeCall.CallID, "", false)
		m.activeCall.State = CallStateActive
		m.activeCall.StartTime = time.Now()
		m.overlay = overlayAudioCall
		return m, startAudioEngineCmd(m)
	}

case AudioCallCloseMsg:
	m.overlay = overlayNone
	return m, nil

case AudioCallTimerTickMsg:
	if m.activeCall != nil && m.activeCall.State == CallStateActive {
		return m, audioCallTimerTickCmd()
	}
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): wire audio call overlay dispatch, View routing, and message handlers"
```

---

## Phase C: Audio Effects

### Task 12: Create biquad filter math

**Files:**
- Create: `internal/audio/biquad.go`

- [ ] **Step 1: Implement biquad filters**

```go
// internal/audio/biquad.go
package audio

import "math"

type BiquadType int

const (
	BiquadLowShelf BiquadType = iota
	BiquadPeaking
	BiquadHighShelf
)

// BiquadFilter is a second-order IIR filter (biquad).
type BiquadFilter struct {
	b0, b1, b2 float64
	a1, a2     float64
	z1, z2     float64 // state (delay line)
}

// ComputeCoefficients sets the filter coefficients for the given type,
// frequency, sample rate, gain (dB), and Q factor.
// Based on the Audio EQ Cookbook by Robert Bristow-Johnson.
func (f *BiquadFilter) ComputeCoefficients(typ BiquadType, freq, sampleRate, gainDB, q float64) {
	A := math.Pow(10, gainDB/40.0)
	w0 := 2 * math.Pi * freq / sampleRate
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / (2 * q)

	var b0, b1, b2, a0, a1, a2 float64

	switch typ {
	case BiquadLowShelf:
		sqrtA := math.Sqrt(A)
		b0 = A * ((A + 1) - (A-1)*cosW0 + 2*sqrtA*alpha)
		b1 = 2 * A * ((A - 1) - (A+1)*cosW0)
		b2 = A * ((A + 1) - (A-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (A + 1) + (A-1)*cosW0 + 2*sqrtA*alpha
		a1 = -2 * ((A - 1) + (A+1)*cosW0)
		a2 = (A + 1) + (A-1)*cosW0 - 2*sqrtA*alpha

	case BiquadPeaking:
		b0 = 1 + alpha*A
		b1 = -2 * cosW0
		b2 = 1 - alpha*A
		a0 = 1 + alpha/A
		a1 = -2 * cosW0
		a2 = 1 - alpha/A

	case BiquadHighShelf:
		sqrtA := math.Sqrt(A)
		b0 = A * ((A + 1) + (A-1)*cosW0 + 2*sqrtA*alpha)
		b1 = -2 * A * ((A - 1) + (A+1)*cosW0)
		b2 = A * ((A + 1) + (A-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (A + 1) - (A-1)*cosW0 + 2*sqrtA*alpha
		a1 = 2 * ((A - 1) - (A+1)*cosW0)
		a2 = (A + 1) - (A-1)*cosW0 - 2*sqrtA*alpha
	}

	// Normalize by a0.
	f.b0 = b0 / a0
	f.b1 = b1 / a0
	f.b2 = b2 / a0
	f.a1 = a1 / a0
	f.a2 = a2 / a0
}

// Process applies the filter to a single sample (Direct Form II Transposed).
func (f *BiquadFilter) Process(in float64) float64 {
	out := f.b0*in + f.z1
	f.z1 = f.b1*in - f.a1*out + f.z2
	f.z2 = f.b2*in - f.a2*out
	return out
}

// Reset clears the filter state.
func (f *BiquadFilter) Reset() {
	f.z1 = 0
	f.z2 = 0
}
```

- [ ] **Step 2: Add biquad test**

Add to `internal/audio/engine_test.go`:

```go
func TestBiquadPassthrough(t *testing.T) {
	var f BiquadFilter
	// 0 dB gain = passthrough
	f.ComputeCoefficients(BiquadPeaking, 1000, 48000, 0, 1.0)
	// Feed a known signal.
	for i := 0; i < 100; i++ {
		in := float64(i) / 100.0
		out := f.Process(in)
		// With 0dB gain, output should approximately equal input
		// after the transient settles.
		if i > 10 {
			diff := out - in
			if diff > 0.01 || diff < -0.01 {
				t.Fatalf("sample %d: in=%f out=%f diff=%f", i, in, out, diff)
			}
		}
	}
}
```

- [ ] **Step 3: Verify and commit**

```bash
go test ./internal/audio/ -v
git add internal/audio/biquad.go internal/audio/engine_test.go
git commit -m "feat(audio): biquad filter math (low shelf, peaking, high shelf)"
```

---

### Task 13: Create full EffectChain with 7-band EQ and Compressor

**Files:**
- Modify: `internal/audio/effects.go` (replace stub)

- [ ] **Step 1: Replace the stub with the full implementation**

```go
// internal/audio/effects.go
package audio

import "math"

// EffectChain applies EQ and compression to PCM float32 samples.
type EffectChain struct {
	EQ         *Equalizer
	Comp       *Compressor
	EQEnabled  bool
	CompEnabled bool
}

func NewEffectChain() *EffectChain {
	return &EffectChain{
		EQ:   NewEqualizer(),
		Comp: NewCompressor(),
	}
}

func (c *EffectChain) Process(samples []float32) {
	if c.EQEnabled && c.EQ != nil {
		c.EQ.Process(samples)
	}
	if c.CompEnabled && c.Comp != nil {
		c.Comp.Process(samples)
	}
}

// ── Equalizer ──────────────────────────────────────────────────

type EQBand struct {
	Label  string
	Freq   float64
	Gain   float32 // dB, -12 to +12
	Type   BiquadType
	filter BiquadFilter
}

type Equalizer struct {
	Bands [7]EQBand
}

func NewEqualizer() *Equalizer {
	eq := &Equalizer{
		Bands: [7]EQBand{
			{Label: "100Hz", Freq: 100, Type: BiquadLowShelf},
			{Label: "250Hz", Freq: 250, Type: BiquadPeaking},
			{Label: "500Hz", Freq: 500, Type: BiquadPeaking},
			{Label: "1kHz", Freq: 1000, Type: BiquadPeaking},
			{Label: "3kHz", Freq: 3000, Type: BiquadPeaking},
			{Label: "7kHz", Freq: 7000, Type: BiquadPeaking},
			{Label: "12kHz", Freq: 12000, Type: BiquadHighShelf},
		},
	}
	for i := range eq.Bands {
		eq.Bands[i].Recalc()
	}
	return eq
}

func (b *EQBand) Recalc() {
	b.filter.ComputeCoefficients(b.Type, b.Freq, float64(SampleRate), float64(b.Gain), 1.0)
}

func (b *EQBand) SetGain(g float32) {
	if g < -12 {
		g = -12
	}
	if g > 12 {
		g = 12
	}
	b.Gain = g
	b.Recalc()
}

func (eq *Equalizer) Process(samples []float32) {
	for i, s := range samples {
		v := float64(s)
		for b := range eq.Bands {
			v = eq.Bands[b].filter.Process(v)
		}
		samples[i] = float32(v)
	}
}

// ── Compressor ─────────────────────────────────────────────────

type Compressor struct {
	Threshold  float32 // dB (-60 to 0)
	Ratio      float32 // 1.0 to 20.0
	AttackMs   float32 // 0.1 to 100
	ReleaseMs  float32 // 10 to 1000
	MakeupGain float32 // dB (0 to 24)
	envelope   float32 // current envelope in dB

	// Live metering (updated per-frame).
	InputLevel    float32 // dBFS
	GainReduction float32 // dB (<= 0)
	OutputLevel   float32 // dBFS
}

func NewCompressor() *Compressor {
	return &Compressor{
		Threshold:  -20,
		Ratio:      4,
		AttackMs:   10,
		ReleaseMs:  100,
		MakeupGain: 0,
	}
}

func (c *Compressor) Process(samples []float32) {
	attackCoeff := float32(math.Exp(-1.0 / (float64(c.AttackMs) * 0.001 * float64(SampleRate))))
	releaseCoeff := float32(math.Exp(-1.0 / (float64(c.ReleaseMs) * 0.001 * float64(SampleRate))))

	var inputSum, outputSum float32
	var maxGR float32

	for i, s := range samples {
		// Input level in dB.
		absS := s
		if absS < 0 {
			absS = -absS
		}
		inputDB := float32(-96.0)
		if absS > 1e-10 {
			inputDB = float32(20 * math.Log10(float64(absS)))
		}

		// Envelope follower.
		if inputDB > c.envelope {
			c.envelope = attackCoeff*c.envelope + (1-attackCoeff)*inputDB
		} else {
			c.envelope = releaseCoeff*c.envelope + (1-releaseCoeff)*inputDB
		}

		// Gain computation.
		gr := float32(0)
		if c.envelope > c.Threshold && c.Ratio > 1 {
			excess := c.envelope - c.Threshold
			gr = excess - excess/c.Ratio
		}

		// Apply gain reduction + makeup.
		gainDB := -gr + c.MakeupGain
		gain := float32(math.Pow(10, float64(gainDB)/20.0))
		samples[i] = s * gain

		inputSum += s * s
		outputSum += samples[i] * samples[i]
		if gr > maxGR {
			maxGR = gr
		}
	}

	// Update metering (per-frame RMS).
	n := float32(len(samples))
	if n > 0 {
		inputRMS := float32(math.Sqrt(float64(inputSum / n)))
		outputRMS := float32(math.Sqrt(float64(outputSum / n)))
		c.InputLevel = dbFS(inputRMS)
		c.OutputLevel = dbFS(outputRMS)
		c.GainReduction = -maxGR
	}
}

func dbFS(rms float32) float32 {
	if rms < 1e-10 {
		return -96
	}
	return float32(20 * math.Log10(float64(rms)))
}
```

- [ ] **Step 2: Add effects test**

```go
func TestCompressorReducesLoudSignal(t *testing.T) {
	c := NewCompressor()
	c.Threshold = -20
	c.Ratio = 4
	// Create a loud signal (0 dBFS = amplitude 1.0).
	samples := make([]float32, FrameSize)
	for i := range samples {
		samples[i] = 0.9
	}
	c.Process(samples)
	// After compression, samples should be quieter.
	if samples[FrameSize-1] >= 0.9 {
		t.Fatalf("compressor did not reduce: got %f", samples[FrameSize-1])
	}
	if c.GainReduction >= 0 {
		t.Fatalf("expected negative gain reduction, got %f", c.GainReduction)
	}
}

func TestEqualizerBoost(t *testing.T) {
	eq := NewEqualizer()
	eq.Bands[3].SetGain(12) // boost 1kHz by 12dB
	// Feed a DC signal — the filter should pass it through without explosion.
	samples := make([]float32, FrameSize)
	for i := range samples {
		samples[i] = 0.1
	}
	eq.Process(samples)
	// Should not be NaN or Inf.
	for _, s := range samples {
		if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
			t.Fatal("EQ produced NaN/Inf")
		}
	}
}
```

- [ ] **Step 3: Run tests and commit**

```bash
go test ./internal/audio/ -v
git add internal/audio/effects.go internal/audio/engine_test.go
git commit -m "feat(audio): 7-band EQ + compressor with live metering"
```

---

### Task 14: Create metering helpers

**Files:**
- Create: `internal/audio/metering.go`

- [ ] **Step 1: Implement metering**

```go
// internal/audio/metering.go
package audio

import "math"

// MeterSnapshot holds the display values for one meter bar.
type MeterSnapshot struct {
	Level float32 // dBFS (-96 to 0)
}

// MeterBar renders a meter as a string of filled/empty blocks.
// width is the number of characters. rangeDB is the total dB range
// (e.g. 60 for -60dB to 0dB).
func MeterBar(level float32, width int, rangeDB float32) string {
	if level < -rangeDB {
		level = -rangeDB
	}
	if level > 0 {
		level = 0
	}
	frac := (level + rangeDB) / rangeDB
	filled := int(frac * float32(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := make([]byte, width)
	for i := range bar {
		if i < filled {
			bar[i] = '\xe2' // will use styled rendering instead
		}
	}
	// Return as simple string — TUI will style with colors.
	result := ""
	for i := 0; i < width; i++ {
		if i < filled {
			result += "█"
		} else {
			result += "░"
		}
	}
	return result
}

// GainReductionBar renders gain reduction right-to-left (more reduction = more filled from right).
func GainReductionBar(gr float32, width int, rangeDB float32) string {
	// gr is negative (e.g. -6 dB). Map to fill from right.
	absGR := -gr
	if absGR < 0 {
		absGR = 0
	}
	if absGR > rangeDB {
		absGR = rangeDB
	}
	frac := absGR / rangeDB
	filled := int(frac * float32(width))
	result := ""
	for i := 0; i < width; i++ {
		if i >= width-filled {
			result += "█"
		} else {
			result += "░"
		}
	}
	return result
}

// EQBarChart renders the 7-band EQ as a vertical bar chart string.
// Returns multiple lines (top to bottom: +12 to -12).
func EQBarChart(bands [7]EQBand, selectedBand int, height int) []string {
	if height < 3 {
		height = 13 // ±12 + zero line
	}
	lines := make([]string, height)
	halfH := height / 2
	dbPerRow := 12.0 / float64(halfH)

	for row := 0; row < height; row++ {
		rowDB := float64(halfH-row) * dbPerRow
		line := ""
		for b, band := range bands {
			bandGain := float64(band.Gain)
			// Determine if this row should be filled.
			filled := false
			if bandGain >= 0 && rowDB >= 0 && rowDB <= bandGain {
				filled = true
			}
			if bandGain < 0 && rowDB < 0 && rowDB >= bandGain {
				filled = true
			}
			if row == halfH {
				// Zero line.
				if filled {
					line += "██"
				} else {
					line += "──"
				}
			} else if filled {
				line += "██"
			} else {
				line += "  "
			}
			if b < 6 {
				line += "  " // spacing between bands
			}
			_ = selectedBand // styling handled by caller
		}
		lines[row] = line
	}
	return lines
}

// RMSLevel computes the RMS level in dBFS for a buffer of float32 samples.
func RMSLevel(samples []float32) float32 {
	if len(samples) == 0 {
		return -96
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	if rms < 1e-10 {
		return -96
	}
	return float32(20 * math.Log10(rms))
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/audio/metering.go
git commit -m "feat(audio): metering helpers — bar charts, EQ visualization, RMS levels"
```

---

### Task 15: Create effect profiles

**Files:**
- Create: `internal/audio/profile.go`

- [ ] **Step 1: Implement profile persistence**

```go
// internal/audio/profile.go
package audio

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EffectProfile stores the configuration for both outgoing and incoming chains.
type EffectProfile struct {
	Name     string       `json:"name"`
	Outgoing ChainConfig  `json:"outgoing"`
	Incoming ChainConfig  `json:"incoming"`
}

// ChainConfig stores the serializable state of one EffectChain.
type ChainConfig struct {
	EQEnabled   bool       `json:"eq_enabled"`
	EQBands     [7]float32 `json:"eq_bands"`     // gain per band in dB
	CompEnabled bool       `json:"comp_enabled"`
	CompThreshold float32  `json:"comp_threshold"`
	CompRatio     float32  `json:"comp_ratio"`
	CompAttack    float32  `json:"comp_attack"`
	CompRelease   float32  `json:"comp_release"`
	CompMakeup    float32  `json:"comp_makeup"`
}

// DefaultProfile returns a sensible starting profile.
func DefaultProfile() EffectProfile {
	return EffectProfile{
		Name: "Default",
		Outgoing: ChainConfig{
			CompThreshold: -20,
			CompRatio:     4,
			CompAttack:    10,
			CompRelease:   100,
		},
		Incoming: ChainConfig{
			CompThreshold: -20,
			CompRatio:     2,
			CompAttack:    10,
			CompRelease:   100,
		},
	}
}

// ApplyToChain configures an EffectChain from a ChainConfig.
func ApplyToChain(chain *EffectChain, cfg ChainConfig) {
	chain.EQEnabled = cfg.EQEnabled
	chain.CompEnabled = cfg.CompEnabled
	for i, gain := range cfg.EQBands {
		chain.EQ.Bands[i].SetGain(gain)
	}
	chain.Comp.Threshold = cfg.CompThreshold
	chain.Comp.Ratio = cfg.CompRatio
	chain.Comp.AttackMs = cfg.CompAttack
	chain.Comp.ReleaseMs = cfg.CompRelease
	chain.Comp.MakeupGain = cfg.CompMakeup
}

// ChainToConfig reads the current state of an EffectChain into a ChainConfig.
func ChainToConfig(chain *EffectChain) ChainConfig {
	var bands [7]float32
	for i, b := range chain.EQ.Bands {
		bands[i] = b.Gain
	}
	return ChainConfig{
		EQEnabled:     chain.EQEnabled,
		EQBands:       bands,
		CompEnabled:   chain.CompEnabled,
		CompThreshold: chain.Comp.Threshold,
		CompRatio:     chain.Comp.Ratio,
		CompAttack:    chain.Comp.AttackMs,
		CompRelease:   chain.Comp.ReleaseMs,
		CompMakeup:    chain.Comp.MakeupGain,
	}
}

// LoadProfiles reads profiles from disk. Returns a default if the file doesn't exist.
func LoadProfiles(configDir string) []EffectProfile {
	path := filepath.Join(configDir, "audio_profiles.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []EffectProfile{DefaultProfile()}
	}
	var profiles []EffectProfile
	if err := json.Unmarshal(data, &profiles); err != nil || len(profiles) == 0 {
		return []EffectProfile{DefaultProfile()}
	}
	return profiles
}

// SaveProfiles writes profiles to disk.
func SaveProfiles(configDir string, profiles []EffectProfile) error {
	path := filepath.Join(configDir, "audio_profiles.json")
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/audio/profile.go
git commit -m "feat(audio): effect profiles with load/save/apply"
```

---

### Task 16: Add effects UI to call overlay

**Files:**
- Modify: `internal/tui/audiocall.go`

- [ ] **Step 1: Add effects sub-screen state**

Add fields to `AudioCallModel`:

```go
showEffects    bool
effectsTab     int     // 0=outgoing, 1=incoming
effectsSel     int     // selected parameter row
monitorMode    bool    // live meter updates
eqSelectedBand int     // which EQ band is selected in the chart
engine         *audio.Engine
profiles       []audio.EffectProfile
```

- [ ] **Step 2: Add effects key handling**

In the active call `Update`, when `showEffects` is true:
- `Tab` → toggle effectsTab between 0 and 1
- `Up/Down` → navigate effectsSel
- `Left/Right` → adjust the selected parameter (EQ gain by 0.5dB, compressor params by appropriate step)
- `v` → toggle monitorMode
- `p` → save current settings as profile
- `l` → load profile (cycle through saved profiles)
- `Esc` → back to call main screen

- [ ] **Step 3: Add effects View rendering**

Render the wide effects screen with:
- 7-band EQ bar chart using `audio.EQBarChart()`
- Parameter list with selected highlight
- Compressor meters using `audio.MeterBar()` and `audio.GainReductionBar()`
- Tab indicator (Outgoing/Incoming)
- Footer hints

- [ ] **Step 4: Add meter tick**

When `monitorMode` is true, dispatch a `tea.Tick` at 50ms that reads meter values from `engine.OutgoingEffects().Comp.InputLevel` etc. and triggers a re-render.

- [ ] **Step 5: Verify build and commit**

```bash
go build ./...
git add internal/tui/audiocall.go
git commit -m "feat(tui): effects sub-screen with 7-band EQ chart, compressor meters, profiles"
```

---

## Phase D: Polish

### Task 17: Voice Activity Detection

**Files:**
- Create: `internal/audio/vad.go`

- [ ] **Step 1: Implement VAD**

```go
// internal/audio/vad.go
package audio

import "math"

// VAD performs simple energy-based voice activity detection.
type VAD struct {
	Threshold   float32 // dBFS threshold for speech detection
	HoldMs      int     // ms to hold "active" after last voice frame
	sampleRate  int
	holdSamples int
	silentFor   int     // samples since last voice frame
}

func NewVAD(thresholdDB float32, holdMs int) *VAD {
	return &VAD{
		Threshold:   thresholdDB,
		HoldMs:      holdMs,
		sampleRate:  SampleRate,
		holdSamples: holdMs * SampleRate / 1000,
	}
}

// IsVoice returns true if the frame contains voice activity.
func (v *VAD) IsVoice(samples []float32) bool {
	level := RMSLevel(samples)
	if level > v.Threshold {
		v.silentFor = 0
		return true
	}
	v.silentFor += len(samples)
	return v.silentFor < v.holdSamples
}
```

- [ ] **Step 2: Wire into engine capture loop**

In `engine.go`'s `processAndSendFrame`, add VAD check:

```go
// After effects processing, before encoding:
if e.vad != nil && !e.vad.IsVoice(floats) {
    return // skip sending silence frames
}
```

Add `vad *VAD` field to Engine and initialize in NewEngine.

- [ ] **Step 3: Test and commit**

```bash
go test ./internal/audio/ -v
git add internal/audio/vad.go internal/audio/engine.go
git commit -m "feat(audio): voice activity detection to suppress silence frames"
```

---

### Task 18: Call quality metrics

**Files:**
- Modify: `internal/audio/engine.go`
- Modify: `internal/tui/audiocall.go`

- [ ] **Step 1: Add CallStats to engine**

```go
type CallStats struct {
	PacketsSent     uint64
	PacketsReceived uint64
	PacketsLost     uint64
	Bitrate         int    // actual encoded bitrate
	JitterMs        float32
}
```

Track in the send and receive paths. Display in the call overlay's active view.

- [ ] **Step 2: Commit**

```bash
git add internal/audio/engine.go internal/tui/audiocall.go
git commit -m "feat(audio): call quality metrics (packets, loss, jitter, bitrate)"
```

---

### Task 19: Call history

**Files:**
- Create: `internal/audio/history.go`
- Modify: `internal/tui/commands_basic.go`

- [ ] **Step 1: Implement call history persistence**

```go
// internal/audio/history.go
package audio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type CallRecord struct {
	CallID    string        `json:"call_id"`
	PeerID    string        `json:"peer_id"`
	PeerName  string        `json:"peer_name"`
	Started   time.Time     `json:"started"`
	Duration  time.Duration `json:"duration_ns"`
	Direction string        `json:"direction"` // "outgoing" or "incoming"
}

type CallHistory struct {
	Calls []CallRecord `json:"calls"`
}

func LoadCallHistory(configDir string) *CallHistory {
	path := filepath.Join(configDir, "call_history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &CallHistory{}
	}
	var h CallHistory
	json.Unmarshal(data, &h)
	return &h
}

func (h *CallHistory) Add(r CallRecord) {
	h.Calls = append(h.Calls, r)
	if len(h.Calls) > 50 {
		h.Calls = h.Calls[len(h.Calls)-50:]
	}
}

func (h *CallHistory) Save(configDir string) error {
	path := filepath.Join(configDir, "call_history.json")
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
```

- [ ] **Step 2: Add /calls command**

In `commands_basic.go`, register a `/calls` command that reads call history and displays it in the Output view as sections.

- [ ] **Step 3: Record calls on end**

In `model.go`'s `endCall`, save to call history.

- [ ] **Step 4: Commit**

```bash
git add internal/audio/history.go internal/tui/commands_basic.go internal/tui/model.go
git commit -m "feat(audio): call history persistence and /calls command"
```

---

### Task 20: Final build and smoke test

- [ ] **Step 1: Full build**

Run: `make build && make install`

- [ ] **Step 2: Run all tests**

Run: `go test ./...`

- [ ] **Step 3: Verify single-user flow**

Launch slackers, right-click a friend → "Start Audio Call", verify:
- Call request is sent
- Ringing overlay appears
- Cancel works

- [ ] **Step 4: Verify two-user flow**

With two slackers instances:
- Instance A calls Instance B
- B sees incoming call overlay, accepts
- Both hear audio
- Alt+M toggles mute
- `e` opens effects
- `q` ends call
- Taskbar badge appears when overlay is closed

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: P2P audio calling with 7-band EQ, compressor, live metering"
```

---

## Self-Review Checklist

### Spec Coverage
- [x] Opus codec (48kHz mono, 20ms frames) → Task 2
- [x] Jitter buffer → Task 3
- [x] Call signaling (request/accept/reject/end/mute) → Task 4
- [x] P2PAudioProtocol stream → Task 4
- [x] Audio engine (capture/playback/streams) → Task 5
- [x] Tests → Task 6
- [x] Config fields → Task 7
- [x] Shortcuts (alt+p, alt+m) → Tasks 7, 8
- [x] Sidebar "Start Audio Call" → Task 8
- [x] Call overlay (ringing/active states) → Task 9
- [x] Taskbar badge → Task 10
- [x] Overlay dispatch → Task 11
- [x] Biquad filter math → Task 12
- [x] 7-band EQ + Compressor + metering → Task 13
- [x] Metering visualization → Task 14
- [x] Effect profiles → Task 15
- [x] Effects UI with EQ chart + live meters → Task 16
- [x] VAD → Task 17
- [x] Call quality metrics → Task 18
- [x] Call history + /calls → Task 19
- [x] Dual effect chains (outgoing/incoming) → Task 5, 13, 16
- [x] Monitor mode → Task 16
- [x] Wire format (4-byte header: seq + len + opus) → Task 5

### Type Consistency
- `EffectChain` stub in Task 5 → replaced by full impl in Task 13 ✓
- `Codec.Encode/Decode` signatures consistent across Tasks 2, 5, 6 ✓
- `JitterBuffer.Push/Pop` consistent across Tasks 3, 5, 6 ✓
- `BiquadFilter.ComputeCoefficients` in Task 12 → used by `EQBand.Recalc` in Task 13 ✓
- `ActiveCall` struct in Task 8 → used by Tasks 9, 10, 11 ✓
