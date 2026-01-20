# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

## Project Overview

OpsLevel Runner is a Kubernetes-based job processor for OpsLevel. It polls for jobs from the OpsLevel API (or Faktory queue), spins up Kubernetes pods to execute commands, and streams logs back to OpsLevel.

## Build and Development Commands

All commands use [Task](https://taskfile.dev/) and should be run from the repository root:

```bash
# Initial setup (installs tools, sets up go workspace)
task setup

# Build the binary (from src/ directory)
cd src && go build

# Run tests with coverage
task test

# Run a single test
cd src && go test -v ./pkg -run TestSanitizeLogProcessor

# Lint and format check
task lint

# Auto-fix formatting and linting issues
task fix

# Start local Faktory development environment
task start-faktory
```

## Testing Jobs Locally

Test a job against a local Kubernetes cluster:
```bash
cd src
OPSLEVEL_API_TOKEN=XXX go run main.go test -f job.yaml
# Or from stdin:
cat job.yaml | OPSLEVEL_API_TOKEN=XXX go run main.go test -f -
```

## End-to-End Testing with Faktory

Run an end-to-end test with Faktory and a local Kubernetes cluster:
```bash
# Terminal 1: Start Faktory server and worker
task start-faktory

# Terminal 2: Enqueue test jobs (requires Faktory running)
cd src && go run scripts/enqueue-test-jobs.go 50

# Monitor jobs at http://localhost:7420
```

## Architecture

### Entry Points (src/cmd/)
- `root.go` - CLI configuration with Cobra/Viper, initializes logging, Sentry, and K8s client
- `run.go` - Main execution mode with two backends:
  - **API mode** (default): Polls OpsLevel GraphQL API for pending jobs, uses worker pool
  - **Faktory mode**: Consumes jobs from Faktory queue
- `test.go` - Local job testing against a K8s cluster

### Core Components (src/pkg/)
- `k8s.go` - `JobRunner` creates K8s pods, ConfigMaps, and PDBs to execute jobs. Each job gets an ephemeral pod with:
  - Init container that copies the runner binary
  - Job container running the specified image with commands
  - ConfigMap mounting custom scripts from job definition
- `api.go` - GraphQL client wrapper for OpsLevel API
- `logs.go` - `LogStreamer` with pluggable `LogProcessor` chain for stdout/stderr processing
- `leaderElection.go` - K8s lease-based leader election for pod scaling

### Log Processors (src/pkg/)
Log processors form a pipeline that transforms job output:
- `sanitizeLogProcessor.go` - Redacts sensitive variables
- `prefixLogProcessor.go` - Adds timestamps
- `setOutcomeVarLogProcessor.go` - Parses `::set-outcome-var key=value` directives
- `opslevelAppendLogProcessor.go` - Batches and sends logs to OpsLevel API
- `faktoryAppendJobLogProcessor.go` / `faktorySetOutcomeProcessor.go` - Faktory-specific processors

### Signal Handling (src/signal/)
- Graceful shutdown via context cancellation on SIGINT/SIGTERM

## Key Configuration

Environment variables (or CLI flags):
- `OPSLEVEL_API_TOKEN` - Required for API authentication
- `OPSLEVEL_API_URL` - API endpoint (default: https://api.opslevel.com)
- `OPSLEVEL_JOB_POD_NAMESPACE` - K8s namespace for job pods
- `OPSLEVEL_JOB_POD_MAX_WAIT` - Pod startup timeout
- `OPSLEVEL_JOB_POD_MAX_LIFETIME` - Max job duration

## Dependencies

- Uses `opslevel-go` client library (available as git submodule in `src/submodules/opslevel-go`)
- K8s client-go for cluster interaction
- Zerolog for structured logging
- Prometheus client for metrics
