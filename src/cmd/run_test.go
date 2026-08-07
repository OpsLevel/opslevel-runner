package cmd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/opslevel/opslevel-go/v2026"
	"github.com/rs/zerolog"
)

type pendingJobResponse struct {
	job   *opslevel.RunnerJob
	token opslevel.ID
	err   error
}

type fakePendingJobClient struct {
	mu        sync.Mutex
	responses []pendingJobResponse
	tokens    []opslevel.ID
	called    chan struct{}
}

func (f *fakePendingJobClient) RunnerGetPendingJob(_ opslevel.ID, token opslevel.ID) (*opslevel.RunnerJob, opslevel.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tokens = append(f.tokens, token)
	if f.called != nil {
		select {
		case f.called <- struct{}{}:
		default:
		}
	}

	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.job, response.token, response.err
}

type concurrentPendingJobClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *concurrentPendingJobClient) RunnerGetPendingJob(_ opslevel.ID, _ opslevel.ID) (*opslevel.RunnerJob, opslevel.ID, error) {
	c.started <- struct{}{}
	<-c.release
	return &opslevel.RunnerJob{Id: "job-1"}, "", nil
}

func TestWaitForJobPollsUntilJobAvailable(t *testing.T) {
	client := &fakePendingJobClient{
		responses: []pendingJobResponse{
			{job: &opslevel.RunnerJob{}, token: "token-1"},
			{job: &opslevel.RunnerJob{Id: "job-1"}, token: "token-2"},
		},
	}

	job, token, ok := waitForJob(
		context.Background(),
		zerolog.Nop(),
		client,
		"runner-1",
		"",
		0,
	)

	if !ok {
		t.Fatal("expected a pending job")
	}
	if job.Id != "job-1" {
		t.Fatalf("expected job-1, got %q", job.Id)
	}
	if token != "token-2" {
		t.Fatalf("expected token-2, got %q", token)
	}
	if len(client.tokens) != 2 || client.tokens[0] != "" || client.tokens[1] != "token-1" {
		t.Fatalf("expected tokens [\"\" \"token-1\"], got %v", client.tokens)
	}
}

func TestWaitForJobCallsCanRunConcurrently(t *testing.T) {
	const workerCount = 3

	client := &concurrentPendingJobClient{
		started: make(chan struct{}, workerCount),
		release: make(chan struct{}),
	}
	results := make(chan bool, workerCount)

	for range workerCount {
		go func() {
			_, _, ok := waitForJob(context.Background(), zerolog.Nop(), client, "runner-1", "", 0)
			results <- ok
		}()
	}

	for range workerCount {
		select {
		case <-client.started:
		case <-time.After(time.Second):
			t.Fatal("expected all workers to pull jobs concurrently")
		}
	}
	close(client.release)

	for range workerCount {
		if !<-results {
			t.Fatal("expected each worker to receive a job")
		}
	}
}

func TestWaitForJobStopsWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakePendingJobClient{
		responses: []pendingJobResponse{
			{job: &opslevel.RunnerJob{}, token: "token-1"},
		},
		called: make(chan struct{}, 1),
	}
	result := make(chan struct {
		token opslevel.ID
		ok    bool
	}, 1)

	go func() {
		_, token, ok := waitForJob(ctx, zerolog.Nop(), client, "runner-1", "", time.Hour)
		result <- struct {
			token opslevel.ID
			ok    bool
		}{token: token, ok: ok}
	}()

	<-client.called
	cancel()

	select {
	case got := <-result:
		if got.ok {
			t.Fatal("expected polling to stop")
		}
		if got.token != "token-1" {
			t.Fatalf("expected token-1, got %q", got.token)
		}
	case <-time.After(time.Second):
		t.Fatal("polling did not stop after cancellation")
	}
}
