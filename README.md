<p align="center">
    <a href="https://github.com/OpsLevel/opslevel-runner/blob/main/LICENSE" alt="License">
        <img src="https://img.shields.io/github/license/OpsLevel/opslevel-runner.svg" /></a>
    <a href="https://goreportcard.com/report/github.com/OpsLevel/opslevel-runner" alt="Go Report Card">
        <img src="https://goreportcard.com/badge/github.com/OpsLevel/opslevel-runner" /></a>
    <a href="https://GitHub.com/OpsLevel/opslevel-runner/releases/" alt="Release">
        <img src="https://img.shields.io/github/v/release/OpsLevel/opslevel-runner" /></a>  
    <a href="https://masterminds.github.io/stability/experimental.html" alt="Stability: Experimental">
        <img src="https://masterminds.github.io/stability/experimental.svg" /></a>  
    <a href="https://github.com/OpsLevel/opslevel-runner/graphs/contributors" alt="Contributors">
        <img src="https://img.shields.io/github/contributors/OpsLevel/opslevel-runner" /></a>
    <a href="https://github.com/OpsLevel/opslevel-runner/pulse" alt="Activity">
        <img src="https://img.shields.io/github/commit-activity/m/OpsLevel/opslevel-runner" /></a>
    <a href="https://github.com/OpsLevel/opslevel-runner/releases" alt="Downloads">
        <img src="https://img.shields.io/github/downloads/OpsLevel/opslevel-runner/total" /></a>
</p>

# opslevel-runner
OpsLevel Runner is the Kubernetes based job processor for [OpsLevel](https://www.opslevel.com/)

### Metrics

| Name | Type | Description |
| --- | --- | --- |
| opslevel_runner_jobs_duration | `histogram` | The duration of jobs in seconds. |
| opslevel_runner_jobs_finished | `counter` | The count of jobs that finished processing by outcome status. |
| opslevel_runner_jobs_processing | `gauge` | The current number of active jobs being processed. |
| opslevel_runner_jobs_started | `counter` | The count of jobs that started processing. |


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
variables:
  - key: Secret
    value: "World!"
    sensitive: true
  - key: NotSecret
    value: "World!"
    sensitive: false
EOF
```

Running

```sh
# Production
OPSLEVEL_API_TOKEN=XXXXX go run main.go run ZZZZZ 
# Staging
OPSLEVEL_API_TOKEN=XXXXX go run main.go run ZZZZZ --api-url=https://api.opslevel-staging.com/graphql --app-url=https://app.opslevel-staging.com  
```