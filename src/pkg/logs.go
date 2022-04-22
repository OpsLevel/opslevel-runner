package pkg

import (
	"strings"
	"time"
)

type LogProcessor interface {
	Process(line string) string
}

type LogStreamer struct {
	Stdout     *SafeBuffer
	Stderr     *SafeBuffer
	processors []LogProcessor
}

func NewLogStreamer(processors... LogProcessor) LogStreamer {
	return LogStreamer{
		Stdout:     &SafeBuffer{},
		Stderr:     &SafeBuffer{},
		processors: processors,
	}
}

func (s *LogStreamer) Run() {
	for {
		for len(s.Stderr.String()) > 0 {
			line, err := s.Stderr.ReadString('\n')
			if err == nil {
				line = strings.TrimSuffix(line, "\n")
				for _, processor := range s.processors {
					line = processor.Process(line)
				}
			}
		}
		for len(s.Stdout.String()) > 0 {
			line, err := s.Stdout.ReadString('\n')
			if err == nil {
				line = strings.TrimSuffix(line, "\n")
				for _, processor := range s.processors {
					line = processor.Process(line)
				}
			}
		}
	}
}

func (s *LogStreamer) Flush() {
	for len(s.Stderr.String()) > 0 {
		time.Sleep(time.Millisecond * 200)
	}
	for len(s.Stdout.String()) > 0 {
		time.Sleep(time.Millisecond * 200)
	}
}

type OpsLevelLogWriter struct {
	id                string
	maxTime           time.Duration
	maxSize           int
	cache             []byte
	timeSinceLastEmit time.Time
}

func NewOpsLevelLogWriter(id string, maxTime time.Duration, maxSize int) OpsLevelLogWriter {
	return OpsLevelLogWriter{
		id:                id,
		maxTime:           maxTime,
		maxSize:           maxSize,
		cache:             []byte{},
		timeSinceLastEmit: time.Now(),
	}
}

func (s *OpsLevelLogWriter) Write(p []byte) (n int, err error) {
	s.cache = append(s.cache, p...)
	if time.Since(s.timeSinceLastEmit) > s.maxTime {
		s.Emit()
	}
	if len(s.cache) > s.maxSize {
		s.Emit()
	}
	return len(p), nil
}

func (s *OpsLevelLogWriter) Emit() {
	// TODO: Send API request back to OpsLevel with all s.cache log lines
	// fmt.Printf("Emitting '%d' bytes to OpsLevel\n", len(s.cache))
	// fmt.Printf("##########\n")
	// fmt.Printf("%s", string(s.cache))
	// fmt.Printf("##########\n")
	s.cache = []byte{}
	s.timeSinceLastEmit = time.Now()
}
