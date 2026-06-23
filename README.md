<p align="center">
    <a href="https://github.com/OpsLevel/opslevel-runner/blob/main/LICENSE">
        <img src="https://img.shields.io/github/license/OpsLevel/opslevel-runner.svg" alt="License" /></a>
    <a href="https://goreportcard.com/report/github.com/OpsLevel/opslevel-runner">
        <img src="https://goreportcard.com/badge/github.com/OpsLevel/opslevel-runner" alt="Go Report Card" /></a>
    <a href="https://GitHub.com/OpsLevel/opslevel-runner/releases/">
        <img src="https://img.shields.io/github/v/release/OpsLevel/opslevel-runner" alt="Release" /></a>
    <a href="https://masterminds.github.io/stability/experimental.html">
        <img src="https://masterminds.github.io/stability/experimental.svg" alt="Stability: Experimental" /></a>
    <a href="https://github.com/OpsLevel/opslevel-runner/graphs/contributors">
        <img src="https://img.shields.io/github/contributors/OpsLevel/opslevel-runner" alt="Contributors" /></a>
    <a href="https://github.com/OpsLevel/opslevel-runner/pulse">
        <img src="https://img.shields.io/github/commit-activity/m/OpsLevel/opslevel-runner" alt="Activity" /></a>
    <a href="https://github.com/OpsLevel/opslevel-runner/releases">
        <img src="https://img.shields.io/github/downloads/OpsLevel/opslevel-runner/total" alt="Downloads" /></a>
</p>

[![Overall](https://img.shields.io/endpoint?style=flat&url=https%3A%2F%2Fapp.opslevel.com%2Fapi%2Fservice_level%2FjcZ9Qt0e3fce3G6Xbo767Z2tXbKKKZ6qsRGzHZWwRME)](https://app.opslevel.com/services/opslevel_runner/maturity-report)

# opslevel-runner
OpsLevel Runner is the Kubernetes based job processor for [OpsLevel](https://www.opslevel.com/)

### Metrics

| Name                            | Type        | Description                                                   |
|---------------------------------|-------------|---------------------------------------------------------------|
| opslevel_runner_jobs_duration   | `histogram` | The duration of jobs in seconds.                              |
| opslevel_runner_jobs_finished   | `counter`   | The count of jobs that finished processing by outcome status. |
| opslevel_runner_jobs_processing | `gauge`     | The current number of active jobs being processed.            |
| opslevel_runner_jobs_started    | `counter`   | The count of jobs that started processing.                    |

### Commands

Testing a job

```sh
OPSLEVEL_API_TOKEN=XXXXX go run main.go test -f job.yaml

cat << EOF | OPSLEVEL_API_TOKEN=XXXXX go run main.go test -f -
id: "1"
image: alpine/curl
commands:
  - export TEST=100
  - echo "::set-outcome-var hello-world=42"
  - sleep 2
  - echo $TEST
  - echo $Secret
  - echo $NotSecret
  - /opslevel/check.sh
variables:
  - key: Secret
    value: "World!"
    sensitive: true
  - key: NotSecret
    value: "World!"
    sensitive: false
files:
  - name: check.sh
    contents: |
      #! /bin/bash
      echo "Hello from inside the script!"
      echo "Secrets are still ${Secret}"
      sleep 2
EOF
```

Running

```sh
# Production
OPSLEVEL_API_TOKEN=XXXXX go run main.go run
# Staging
OPSLEVEL_API_TOKEN=XXXXX go run main.go run --api-url=https://api.opslevel-staging.com/graphql --app-url=https://app.opslevel-staging.com
```

## Running

Download the latest release from the [Releases](https://github.com/OpsLevel/opslevel-runner/releases/) page for your architecture.

Extract it to a directory of your choice, mark it as executable and move it to something on your path

```
chmod +x ./opslevel-runner
mv ./opslevel-runner /usr/local/bin/opslevel-runner
```

For OSX you'll probably need to remove the quarantine bit:

```
sudo xattr -r -d com.apple.quarantine /usr/local/bin/opslevel-runner
```

At this point you can run the OpsLevel Runner with OpsLevel by following the [instructions](https://gitlab.com/jklabsinc/OpsLevel/-/blob/master/CONTRIBUTING.md#OpsLevel-Runner) in that repository.

## Developing

To build, ensure you have go installed

```
brew install go
```

Then init and update the submodules (if you clone with `--recurse-submodules` you can skip this step)

```
git submodule init && git submodule update
```

Then run `go build` in `src` to build in the local directory, you can also use `-o <PATH>` to put the file in a target location of your choice.

```
cd src
go build
```

## Local Development

The dev environment uses [kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker/Podman), [Faktory](https://github.com/contribsys/faktory) as job queue, and [Task](https://taskfile.dev/) as task runner.

### Prerequisites

- Go (`brew install go`)
- [Task](https://taskfile.dev/) (`brew install go-task`)
- Docker or Podman

### Quick Start

```sh
task setup   # install Faktory + workspace deps
task run     # start Faktory + workers (creates kind cluster automatically)
```

### What `task run` Does

The `run` task instantiates the kind cluster if it doesn't exist then starts
[goreman](https://github.com/mattn/goreman) which supervises 4 concurrent
processes defined in `src/Procfile`:

| Process | Description |
|---------|-------------|
| `faktory` | Starts the Faktory work server (job queue) |
| `runner` | hot-reloads `opslevel-runner run --mode=faktory --queues=runner` through `watchexec` |
| `image-builder` | Watches Go sources and `Dockerfile` with `watchexec`; rebuilds the helper container image and reloads it into kind on change |

> Note: `--mode faktory` does have `opslevel-runner` poll Faktory for runner
> jobs and launches them as pods in the kind cluster

### Kubernetes Configuration

Scripts source `.env.local` (gitignored) to set local environment overrides
before creating or connecting to the kind cluster. e.g.: to reuse a k8s cluster
from a specific KUBECONFIG file

```sh
# .env.local
# Point at a dedicated kubeconfig to keep localdev contexts isolated.
export KUBECONFIG=${HOME}/.kube/opslevel.localdev.yaml
```

- `bin/kind-env.sh` loads this file, falling back to `~/.kube/config` when `KUBECONFIG` is unset.
- The kind cluster name defaults to `opslevel-runner`.

### Container Runtime

Podman is preferred; Docker is used as fallback. Handled in `bin/kind-env.sh`.

### Other Noteworthy Tasks

#### Helper Image

Build and load the runner helper image into kind:

```sh
task build-helper-image
```

This cross-compiles the Go binary for linux, builds the container image from
`Dockerfile`, and loads it into the kind cluster. The image is only rebuilt
when source checksums change.

#### Stopping Kind Cluster

```sh
task stop-kind   # clean orphaned job pods and stop the cluster
```
