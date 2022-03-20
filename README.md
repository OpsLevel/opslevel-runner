# opslevel-runner
OpsLevel Runner is the Kubernetes based job processor for OpsLevel


### Testing Commands

```
go run main.go run "while :; do echo 'Heartbeat'; sleep 1; done"

go run main.go run "for i in 1 2 3 4 5 6 7 8 9 10; do echo \"Hearthbeat \$i\"; sleep 1; done"
```