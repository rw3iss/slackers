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

type Codec struct {
	Encoder *opus.Encoder
	Decoder *opus.Decoder
	Bitrate int
}

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

func (c *Codec) Encode(pcm []int16, buf []byte) (int, error) {
	n, err := c.Encoder.Encode(pcm, buf)
	if err != nil {
		return 0, fmt.Errorf("opus encode: %w", err)
	}
	return n, nil
}

func (c *Codec) Decode(data []byte, pcm []int16) (int, error) {
	n, err := c.Decoder.Decode(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}
	return n, nil
}

func (c *Codec) DecodePLC(pcm []int16) (int, error) {
	n, err := c.Decoder.Decode(nil, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus plc: %w", err)
	}
	return n, nil
}
