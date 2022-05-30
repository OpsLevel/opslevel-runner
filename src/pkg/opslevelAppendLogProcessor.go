package pkg

import (
	"encoding/base64"
	"github.com/opslevel/opslevel-go"
	"github.com/rs/zerolog"
	"time"
)

type OpsLevelAppendLogProcessor struct {
	client            *opslevel.Client
	logger            zerolog.Logger
	runnerId          string
	jobId             string
	maxBytes          int
	maxTime           time.Duration
	logLines          []string
	logLinesBytesSize int
	firstLine         bool
	lastTime          time.Time
	elapsed           time.Duration
}

func NewOpsLevelAppendLogProcessor(client *opslevel.Client, logger zerolog.Logger, runnerId string, jobId string, maxBytes int, maxTime time.Duration) *OpsLevelAppendLogProcessor {
	return &OpsLevelAppendLogProcessor{
		client:            client,
		logger:            logger,
		runnerId:          runnerId,
		jobId:             jobId,
		maxBytes:          maxBytes,
		maxTime:           maxTime,
		logLines:          []string{},
		logLinesBytesSize: 0,
		firstLine:         false,
		lastTime:          time.Now(),
	}
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
	if s.firstLine == false {
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

func (s *OpsLevelAppendLogProcessor) Flush(outcome JobOutcome) {
	if len(s.logLines) > 0 {
		s.logger.Trace().Msg("Sleeping before append job logs ...")
		time.Sleep(1 * time.Second)
		s.submit()
		s.logger.Trace().Msg("Finished append job logs ...")
	}
}

func (s *OpsLevelAppendLogProcessor) submit() {
	if s.client != nil {
		err := s.client.RunnerAppendJobLog(opslevel.RunnerAppendJobLogInput{
			RunnerId:    s.runnerId,
			RunnerJobId: s.jobId,
			SentAt:      opslevel.NewISO8601DateNow(),
			Logs:        s.logLines,
		})
		// TODO: is there any reason we should retry for?
		if err != nil {
			s.logger.Error().Err(err).Msgf("error while appending '%d' log line(s) for job '%s'", len(s.logLines), s.jobId)
		}
	}
	s.logLinesBytesSize = 0
	s.logLines = nil
	s.logLines = []string{}
}
