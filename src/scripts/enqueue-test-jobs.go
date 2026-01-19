//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	faktory "github.com/contribsys/faktory/client"
)

func main() {
	numJobs := 10
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			numJobs = n
		}
	}

	client, err := faktory.Open()
	if err != nil {
		fmt.Printf("Failed to connect to Faktory: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	fmt.Printf("Enqueuing %d test jobs to Faktory...\n", numJobs)

	for i := 1; i <= numJobs; i++ {
		jobID := fmt.Sprintf("test-job-%d-%d", i, time.Now().UnixNano())

		// Create job args matching opslevel.RunnerJob structure
		jobArgs := map[string]interface{}{
			"image": "alpine:latest",
			"commands": []string{
				fmt.Sprintf("echo Hello from job %d", i),
				fmt.Sprintf("echo Job ID: %s", jobID),
				"sleep 2",
				fmt.Sprintf("echo Job %d complete", i),
			},
			"variables": []map[string]interface{}{},
			"files": []map[string]interface{}{
				{
					"name":     "test.sh",
					"contents": "#!/bin/sh\necho Running test script",
				},
			},
		}

		job := faktory.NewJob("legacy", jobArgs)
		job.Queue = "runner"
		job.ReserveFor = 300
		job.SetCustom("opslevel-runner-job-id", jobID)

		if err := client.Push(job); err != nil {
			fmt.Printf("  Failed to enqueue job %d: %v\n", i, err)
			continue
		}

		fmt.Printf("  Enqueued job %d/%d (ID: %s)\n", i, numJobs, jobID)
	}

	fmt.Println()
	fmt.Printf("Done! Enqueued %d jobs to the 'runner' queue.\n", numJobs)
	fmt.Println()
	fmt.Println("Monitor at: http://localhost:7420")
}
