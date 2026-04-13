package audio

import "testing"

func TestCodecRoundTrip(t *testing.T) {
	codec, err := NewCodec(32000)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
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
	// Prime the decoder with a few non-silent frames so it has state for PLC.
	buf := make([]byte, 512)
	out := make([]int16, FrameSize)
	for i := 0; i < 3; i++ {
		pcm := make([]int16, FrameSize)
		for j := range pcm {
			pcm[j] = int16((j + i*100) % 1000)
		}
		n, encErr := codec.Encode(pcm, buf)
		if encErr != nil {
			t.Fatalf("Encode frame %d: %v", i, encErr)
		}
		if _, decErr := codec.Decode(buf[:n], out); decErr != nil {
			t.Fatalf("Decode frame %d: %v", i, decErr)
		}
	}
	plcN, err := codec.DecodePLC(out)
	if err != nil {
		t.Fatalf("PLC: %v", err)
	}
	if plcN != FrameSize {
		t.Fatalf("PLC returned %d samples, want %d", plcN, FrameSize)
	}
}

func TestJitterBufferOrdering(t *testing.T) {
	jb := NewJitterBuffer(2)
	jb.Push(0, []byte{0x00})
	jb.Push(2, []byte{0x02})
	jb.Push(1, []byte{0x01})
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
	jb.Push(2, []byte{0x02})
	f0 := jb.Pop()
	if f0 == nil {
		t.Fatal("seq 0 should be present")
	}
	f1 := jb.Pop()
	if f1 != nil {
		t.Fatal("seq 1 should be nil (gap)")
	}
	f2 := jb.Pop()
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
