package pkg

import (
	"encoding/base64"
	"time"

	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rs/zerolog"
)

// shipQueueDepth bounds how many log batches may wait to ship at once. A slow
// API call backs up into this queue instead of stalling the LogStreamer drain
// loop (which would let the pod stdout/stderr buffers grow unbounded). Memory
// held by the shipper is therefore bounded by shipQueueDepth * maxBytes (times
// ~1.34 for base64), plus the batch currently being assembled. Kept small and
// fixed so per-job memory tracks the single 'job-pod-log-max-size' lever.
const shipQueueDepth = 2

type OpsLevelAppendLogProcessor struct {
	client            *opslevel.Client
	logger            zerolog.Logger
	runnerId          opslevel.ID
	jobId             opslevel.ID
	jobNumber         string
	maxBytes          int
	maxTime           time.Duration
	logLines          []string
	logLinesBytesSize int
	firstLine         bool
	lastTime          time.Time
	elapsed           time.Duration
	batches           chan []string
	done              chan struct{}
	droppedBatches    int
}

func NewOpsLevelAppendLogProcessor(client *opslevel.Client, logger zerolog.Logger, runnerId opslevel.ID, jobId opslevel.ID, jobNumber string, maxBytes int, maxTime time.Duration) *OpsLevelAppendLogProcessor {
	s := &OpsLevelAppendLogProcessor{
		client:            client,
		logger:            logger,
		runnerId:          runnerId,
		jobId:             jobId,
		jobNumber:         jobNumber,
		maxBytes:          maxBytes,
		maxTime:           maxTime,
		logLines:          []string{},
		logLinesBytesSize: 0,
		firstLine:         false,
		lastTime:          time.Now(),
		batches:           make(chan []string, shipQueueDepth),
		done:              make(chan struct{}),
	}
	go s.ship()
	return s
}

func (s *OpsLevelAppendLogProcessor) Process(line string) string {
	lineInBytes := []byte(line)
	lineBytesSize := len(lineInBytes)

	if s.logLinesBytesSize+lineBytesSize > s.maxBytes {
		s.logger.Trace().Msg("Shipping logs because of maxBytes ...")
		s.submit()
	}

	s.logLinesBytesSize += lineBytesSize
	s.logLines = append(s.logLines, base64.StdEncoding.EncodeToString(lineInBytes))
	if !s.firstLine {
		s.logger.Trace().Msg("Shipping logs because its the first line ...")
		s.firstLine = true
		s.submit()
	}

	s.elapsed += time.Since(s.lastTime)
	if s.elapsed > s.maxTime {
		s.logger.Trace().Msg("Shipping logs because of maxTime ...")
		s.elapsed = 0
		s.submit()
	}
	s.lastTime = time.Now()

	return line
}

func (s *OpsLevelAppendLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *OpsLevelAppendLogProcessor) ProcessStderr(line string) string {
	return s.Process(line)
}

func (s *OpsLevelAppendLogProcessor) Flush(outcome JobOutcome) {
	// The pod is done producing, so the final batch must not be dropped: send it
	// with a blocking enqueue (the shipper is still draining) before closing.
	if batch := s.takeBatch(); batch != nil {
		s.batches <- batch
	}
	close(s.batches)
	<-s.done // wait for in-flight batches to finish shipping
	if s.droppedBatches > 0 {
		s.logger.Warn().Msgf("dropped %d log batch(es) for job '%s' due to shipping backpressure", s.droppedBatches, s.jobNumber)
	}
}

// takeBatch detaches the accumulated lines into a standalone batch and starts a
// fresh buffer. A new backing slice is allocated because the returned slice is
// handed to the shipper goroutine. Returns nil when there is nothing buffered.
func (s *OpsLevelAppendLogProcessor) takeBatch() []string {
	if len(s.logLines) == 0 {
		return nil
	}
	batch := s.logLines
	s.logLines = make([]string, 0, len(batch))
	s.logLinesBytesSize = 0
	return batch
}

// submit hands the current batch off to the background shipper. It never blocks:
// if the shipper is behind and the queue is full, the batch is dropped (and
// counted) rather than stalling the drain loop, which would let the pod
// stdout/stderr buffers grow unbounded.
func (s *OpsLevelAppendLogProcessor) submit() {
	batch := s.takeBatch()
	if batch == nil {
		return
	}
	select {
	case s.batches <- batch:
	default:
		s.droppedBatches++
	}
}

// ship runs on its own goroutine for the lifetime of the processor, performing
// the (blocking) network calls so the LogStreamer drain loop never waits on the
// API.
func (s *OpsLevelAppendLogProcessor) ship() {
	defer close(s.done)
	for batch := range s.batches {
		if s.client == nil || len(batch) == 0 {
			continue
		}
		err := s.client.RunnerAppendJobLog(opslevel.RunnerAppendJobLogInput{
			RunnerId:    s.runnerId,
			RunnerJobId: s.jobId,
			SentAt:      opslevel.NewISO8601DateNow(),
			Logs:        batch,
		})
		if err != nil {
			s.logger.Error().Err(err).Msgf("error while appending '%d' log line(s) for job '%s'", len(batch), s.jobNumber)
		}
	}
}
