package cmd

import (
	"bytes"
	"sync"
)

// SafeBuffer is a goroutine safe bytes.Buffer
type SafeBuffer struct {
	buffer bytes.Buffer
	mutex  sync.Mutex
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (s *SafeBuffer) Write(p []byte) (n int, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.Write(p)
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the Buffer is a nil pointer, it returns "<nil>".
func (s *SafeBuffer) String() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.buffer.String()
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
