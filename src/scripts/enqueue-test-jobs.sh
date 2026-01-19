#!/bin/bash
#
# Enqueue N test jobs to Faktory for end-to-end testing
#
# Usage: ./scripts/enqueue-test-jobs.sh [NUM_JOBS]
#        NUM_JOBS defaults to 10
#

set -e

NUM_JOBS=${1:-10}
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$(dirname "$SCRIPT_DIR")"

echo "Enqueuing $NUM_JOBS test jobs to Faktory..."

for i in $(seq 1 $NUM_JOBS); do
    JOB_ID="test-job-${i}-$(date +%s)"

    # Create a temporary job file
    # Note: commands must be an array of strings, files must have name/contents
    JOB_FILE=$(mktemp)
    cat > "$JOB_FILE" <<ENDJOB
type: legacy
queue: runner
reserve_for: 300
retries: 0
args:
  - image: "alpine:latest"
    commands:
      - "echo Hello from job ${i}"
      - "echo Job ID: ${JOB_ID}"
      - "sleep 2"
      - "echo Job ${i} complete"
    variables: []
    files:
      - name: "test.sh"
        contents: "#!/bin/sh\necho Running test script"
custom:
  opslevel-runner-job-id: "${JOB_ID}"
ENDJOB

    # Enqueue the job
    cd "$SRC_DIR" && go run main.go enqueue -f "$JOB_FILE"

    # Clean up temp file
    rm -f "$JOB_FILE"

    echo "  Enqueued job $i/$NUM_JOBS (ID: $JOB_ID)"
done

echo ""
echo "Done! Enqueued $NUM_JOBS jobs to the 'runner' queue."
echo ""
echo "Monitor at: http://localhost:7420"
