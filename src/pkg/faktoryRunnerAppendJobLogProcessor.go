package pkg

import (
	"encoding/base64"
	"time"

	faktory "github.com/contribsys/faktory/client"
	faktoryWorker "github.com/contribsys/faktory_worker_go"
	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rs/zerolog"
)

type FaktoryAppendJobLogProcessor struct {
	helper            faktoryWorker.Helper
	logger            zerolog.Logger
	jobId             opslevel.ID
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

func NewFaktoryAppendJobLogProcessor(helper faktoryWorker.Helper, logger zerolog.Logger, jobId opslevel.ID, maxBytes int, maxTime time.Duration) *FaktoryAppendJobLogProcessor {
	s := &FaktoryAppendJobLogProcessor{
		helper:            helper,
		logger:            logger,
		jobId:             jobId,
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

func (s *FaktoryAppendJobLogProcessor) Process(line string) string {
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

func (s *FaktoryAppendJobLogProcessor) ProcessStdout(line string) string {
	return s.Process(line)
}

func (s *FaktoryAppendJobLogProcessor) ProcessStderr(line string) string {
	return s.Process(line)
}

func (s *FaktoryAppendJobLogProcessor) Flush(outcome JobOutcome) {
	// The pod is done producing, so the final batch must not be dropped: enqueue
	// it with a blocking send (the shipper is still draining) before closing.
	if batch := s.takeBatch(); batch != nil {
		s.batches <- batch
	}
	close(s.batches)
	<-s.done // wait for in-flight batches to finish enqueuing
	if s.droppedBatches > 0 {
		s.logger.Warn().Msgf("dropped %d log batch(es) for job '%s' due to enqueue backpressure", s.droppedBatches, s.jobId)
	}
}

// takeBatch detaches the accumulated lines into a standalone batch and starts a
// fresh buffer; see OpsLevelAppendLogProcessor.takeBatch. Returns nil when there
// is nothing buffered.
func (s *FaktoryAppendJobLogProcessor) takeBatch() []string {
	if len(s.logLines) == 0 {
		return nil
	}
	batch := s.logLines
	s.logLines = make([]string, 0, len(batch))
	s.logLinesBytesSize = 0
	return batch
}

// submit hands the current batch off to the background shipper. It never blocks;
// see OpsLevelAppendLogProcessor.submit for the rationale.
func (s *FaktoryAppendJobLogProcessor) submit() {
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

// ship runs on its own goroutine, enqueuing batches to Faktory so the
// LogStreamer drain loop never blocks on the (network) enqueue call.
func (s *FaktoryAppendJobLogProcessor) ship() {
	defer close(s.done)
	for logLines := range s.batches {
		if len(logLines) == 0 {
			continue
		}
		job := faktory.NewJob("Runners::Faktory::AppendJobLog", opslevel.RunnerAppendJobLogInput{
			RunnerId:    "faktory",
			RunnerJobId: s.jobId,
			SentAt:      opslevel.NewISO8601DateNow(),
			Logs:        logLines,
		})
		job.Queue = "app"
		batch := s.helper.Bid()
		if batch != "" {
			err := s.helper.Batch(func(b *faktory.Batch) error {
				return b.Push(job)
			})
			if err != nil {
				MetricEnqueueBatchFailed.Inc()
				s.logger.Error().Err(err).Msgf("error while enqueuing append logs for '%d' log line(s) for job '%s'", len(logLines), s.jobId)
			}
		} else {
			err := s.helper.With(func(cl *faktory.Client) error {
				return cl.Push(job)
			})
			if err != nil {
				MetricEnqueueFailed.Inc()
				s.logger.Error().Err(err).Msgf("error while enqueuing append logs for '%d' log line(s) for job '%s'", len(logLines), s.jobId)
			}
		}
	}
}
