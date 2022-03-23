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


### Testing Commands

```
go run main.go run "while :; do echo 'Heartbeat'; sleep 1; done"

go run main.go run "for i in 1 2 3 4 5 6 7 8 9 10; do echo \"Hearthbeat \$i\"; sleep 1; done"
```