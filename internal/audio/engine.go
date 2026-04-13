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

// CallStats tracks packet-level statistics for a call session.
type CallStats struct {
	PacketsSent     uint64
	PacketsReceived uint64
	PacketsLost     uint64
}

// Engine manages audio capture, encoding, decoding, and playback for a call.
type Engine struct {
	ctx    context.Context
	cancel context.CancelFunc
	codec  *Codec
	jitter *JitterBuffer

	outgoing *EffectChain
	incoming *EffectChain

	muted  atomic.Bool
	seqOut uint16 // outgoing frame sequence counter

	outStream io.Writer
	inStream  io.Reader

	// Metering values readable by the UI.
	OutMeter Meter
	InMeter  Meter

	// Stats tracks packet-level call statistics.
	Stats CallStats

	// vad performs voice activity detection on outgoing audio.
	vad *VAD

	mu             sync.Mutex
	running        bool
	malgoCtx       *malgo.AllocatedContext
	captureDevice  *malgo.Device
	playbackDevice *malgo.Device

	// Capture buffer accumulator (malgo delivers variable-size chunks).
	capBuf []int16
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
	e := &Engine{
		ctx:      ctx,
		cancel:   cancel,
		codec:    codec,
		jitter:   NewJitterBuffer(jitterFrames),
		outgoing: NewEffectChain(),
		incoming: NewEffectChain(),
	}
	e.vad = NewVAD(-40, 300) // -40dBFS threshold, 300ms hold
	return e, nil
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

// IsMuted returns whether the mic is muted.
func (e *Engine) IsMuted() bool { return e.muted.Load() }

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

	// Write to stream: [2 byte seq][2 byte len][opus data]
	e.mu.Lock()
	out := e.outStream
	e.mu.Unlock()
	if out == nil || e.muted.Load() {
		return
	}

	// VAD: skip sending silent frames (but don't mute — just suppress).
	if e.vad != nil && !e.vad.IsVoice(floats) {
		return
	}

	header := make([]byte, 4) // 2 bytes seq + 2 bytes len
	binary.BigEndian.PutUint16(header[0:2], e.seqOut)
	binary.BigEndian.PutUint16(header[2:4], uint16(n))
	e.seqOut++
	out.Write(header)
	out.Write(buf[:n])
	atomic.AddUint64(&e.Stats.PacketsSent, 1)
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
		atomic.AddUint64(&e.Stats.PacketsReceived, 1)
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
			atomic.AddUint64(&e.Stats.PacketsLost, 1)
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
