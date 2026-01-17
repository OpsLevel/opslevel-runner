package pkg

import (
	"encoding/base64"
	"time"

	faktory "github.com/contribsys/faktory/client"
	faktoryWorker "github.com/contribsys/faktory_worker_go"
	"github.com/opslevel/opslevel-go/v2024"
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
}

func NewFaktoryAppendJobLogProcessor(helper faktoryWorker.Helper, logger zerolog.Logger, jobId opslevel.ID, maxBytes int, maxTime time.Duration) *FaktoryAppendJobLogProcessor {
	return &FaktoryAppendJobLogProcessor{
		helper:            helper,
		logger:            logger,
		jobId:             jobId,
		maxBytes:          maxBytes,
		maxTime:           maxTime,
		logLines:          []string{},
		logLinesBytesSize: 0,
		firstLine:         false,
		lastTime:          time.Now(),
	}
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
		s.elapsed = time.Since(time.Now())
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
	if len(s.logLines) > 0 {
		s.logger.Trace().Msg("Sleeping before append job logs ...")
		time.Sleep(1 * time.Second)
		s.submit()
		s.logger.Trace().Msg("Finished append job logs ...")
	}
}

func (s *FaktoryAppendJobLogProcessor) submit() {
	if len(s.logLines) > 0 {
		job := faktory.NewJob("Runners::Faktory::AppendJobLog", opslevel.RunnerAppendJobLogInput{
			RunnerId:    "faktory",
			RunnerJobId: s.jobId,
			SentAt:      opslevel.NewISO8601DateNow(),
			Logs:        s.logLines,
		})
		job.Queue = "app"
		batch := s.helper.Bid()
		if batch != "" {
			err := s.helper.Batch(func(b *faktory.Batch) error {
				return b.Push(job)
			})
			if err != nil {
				MetricEnqueueBatchFailed.Inc()
				s.logger.Error().Err(err).Msgf("error while enqueuing append logs for '%d' log line(s) for job '%s'", len(s.logLines), s.jobId)
			}
		} else {
			err := s.helper.With(func(cl *faktory.Client) error {
				return cl.Push(job)
			})
			if err != nil {
				MetricEnqueueFailed.Inc()
				s.logger.Error().Err(err).Msgf("error while enqueuing append logs for '%d' log line(s) for job '%s'", len(s.logLines), s.jobId)
			}
		}
	}
	s.logLinesBytesSize = 0
	s.logLines = nil
	s.logLines = []string{}
}
