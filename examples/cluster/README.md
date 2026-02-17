# Cluster Examples

Runnable examples demonstrating Percy's multi-agent clustering capabilities.
Each example is a standalone `main.go` that uses the `cluster` package directly.

## Prerequisites

- Go 1.22+
- No external dependencies (NATS is embedded)
- No API keys needed

## Examples

| # | Directory | What it demonstrates | Time |
|---|-----------|---------------------|------|
| 1 | `01_single_node/` | Node bootstrap, agent registration, self-discovery | <1s |
| 2 | `02_two_nodes/` | Multi-node clustering, distributed agent registry | <1s |
| 3 | `03_task_lifecycle/` | Submit → claim → working → complete, CAS double-claim prevention | <1s |
| 4 | `04_worker/` | Worker auto-execution, capability matching | ~2s |
| 5 | `05_dependencies/` | Orchestrator DAG scheduling, dependency resolution | <1s |
| 6 | `06_locks/` | Distributed file locking, contention, bulk release | <1s |
| 7 | `07_heartbeat/` | Stale agent detection, task requeue, lock cleanup | ~2s |
| 8 | `08_merge/` | Git merge worktree, clean merge, LLM conflict resolution | <1s |
| 9 | `09_full_pipeline/` | End-to-end: orchestrator + worker + monitor + git branches + dependency DAG | ~5s |

## Running

From the repo root:

```bash
# Run a single example
go run ./examples/cluster/01_single_node/

# Run all examples
for d in examples/cluster/*/; do
  echo "\n=== $d ==="
  go run "./$d"
done
```

## Progression

The examples build on each other conceptually:

```
01 Single Node          "Hello, cluster"
    │
02 Two Nodes            Node discovery & distributed state
    │
    ├── 03 Task Lifecycle    Task state machine & CAS concurrency
    │       │
    │   04 Worker             Auto-claim & capability routing
    │       │
    │   05 Dependencies       DAG scheduling & orchestration
    │
    ├── 06 Locks             Distributed file locking
    │
    ├── 07 Heartbeat         Failure detection & recovery
    │
    └── 08 Merge             Git worktree merge + conflict resolution
            │
        09 Full Pipeline     Everything together
```

## Architecture Recap

The cluster system coordinates multiple Percy instances via NATS JetStream:

- **Agent Registry** — agents register with capabilities, heartbeat for liveness
- **Task Queue** — CAS-based claiming prevents double-assignment
- **Orchestrator** — submits task plans, resolves dependency DAGs
- **Worker** — polls for matching tasks, executes via callback
- **Monitor** — event-driven dependency resolution + stale agent cleanup
- **Merge Pipeline** — git worktree merging with LLM conflict resolution
- **Lock Manager** — distributed file locking via JetStream KV

All distributed state lives in NATS JetStream KV buckets (`agents`, `tasks`, `locks`, `cluster`).
