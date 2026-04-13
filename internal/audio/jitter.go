// internal/audio/jitter.go
package audio

import "sync"

// JitterBuffer reorders and smooths incoming audio frames.
type JitterBuffer struct {
	mu      sync.Mutex
	slots   map[uint16][]byte
	nextSeq uint16
	depth   int
	seeded  bool
}

func NewJitterBuffer(depth int) *JitterBuffer {
	if depth < 1 {
		depth = 3
	}
	return &JitterBuffer{
		slots: make(map[uint16][]byte, depth*2),
		depth: depth,
	}
}

func (j *JitterBuffer) Push(seq uint16, data []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.seeded {
		j.nextSeq = seq
		j.seeded = true
	}
	frame := make([]byte, len(data))
	copy(frame, data)
	j.slots[seq] = frame
	limit := j.depth * 2
	for s := range j.slots {
		if seqDist(s, j.nextSeq) > uint16(limit) {
			delete(j.slots, s)
		}
	}
}

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
	return nil
}

func (j *JitterBuffer) Ready() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.slots) >= j.depth
}

func (j *JitterBuffer) Reset() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.slots = make(map[uint16][]byte, j.depth*2)
	j.seeded = false
}

func seqDist(a, b uint16) uint16 {
	return a - b
}
