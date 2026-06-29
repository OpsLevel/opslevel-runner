package pkg

import (
	"bytes"
	"sync"
)

// SafeBuffer is a goroutine safe bytes.Buffer with an optional size cap.
//
// The cap exists to protect the runner from OOM: pod stdout/stderr is written
// into this buffer by client-go's exec stream, while the LogStreamer drains it
// on a ticker. If the drain side stalls (e.g. a slow log-shipping API call) or a
// job emits data faster than it can be drained, an unbounded buffer would grow
// until the process is killed. When maxSize is exceeded, writes beyond the cap
// are dropped and the dropped byte count is recorded so the streamer can emit a
// visible marker into the log stream.
type SafeBuffer struct {
	buffer  bytes.Buffer
	mutex   sync.Mutex
	maxSize int
	dropped int
}

// NewSafeBuffer returns a SafeBuffer that drops writes once it holds maxSize
// bytes of unread data. A maxSize <= 0 means unbounded.
func NewSafeBuffer(maxSize int) *SafeBuffer {
	return &SafeBuffer{maxSize: maxSize}
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
//
// To the caller (client-go's exec stream copier) the write always "succeeds" —
// returning a short write or error would tear down the exec stream. When the cap
// is reached we accept as much as fits, drop the rest, and track how much was
// dropped.
func (s *SafeBuffer) Write(p []byte) (n int, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.maxSize > 0 {
		room := s.maxSize - s.buffer.Len()
		if room <= 0 {
			s.dropped += len(p)
			return len(p), nil
		}
		if len(p) > room {
			s.buffer.Write(p[:room])
			s.dropped += len(p) - room
			return len(p), nil
		}
	}
	return s.buffer.Write(p)
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the Buffer is a nil pointer, it returns "<nil>".
func (s *SafeBuffer) String() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.String()
}

// Len returns the number of bytes of the unread portion of the buffer.
func (s *SafeBuffer) Len() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.Len()
}

// DroppedBytes returns the number of bytes dropped since the last call and
// resets the counter.
func (s *SafeBuffer) DroppedBytes() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	n := s.dropped
	s.dropped = 0
	return n
}

// ReadString reads until the first occurrence of delim in the input,
// returning a string containing the data up to and including the delimiter.
// If ReadString encounters an error before finding a delimiter,
// it returns the data read before the error and the error itself (often io.EOF).
// ReadString returns err != nil if and only if the returned data does not end
// in delim.
func (s *SafeBuffer) ReadString(delim byte) (line string, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.ReadString(delim)
}
