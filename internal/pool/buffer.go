package pool

import (
	"bytes"
	"sync"
)

// Do not retain unusually large pages after their requests finish.
const maxBufferCapacity = 1024 * 1024

var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func GetBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

// Callers must finish using Bytes before returning the buffer. String makes an
// independent copy and is safe to keep after PutBuffer.
func PutBuffer(buf *bytes.Buffer) {
	if buf.Cap() > maxBufferCapacity {
		return
	}
	buf.Reset()
	bufferPool.Put(buf)
}
