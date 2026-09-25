# eip-toolkit

A small toolkit for EIP-1559 base fee math, Merkle allowlist NFTs, and a production-grade Ethereum log filter engine.

## Layout

```
eip-toolkit/
├── go/
│   ├── eip1559/        # BaseFee calculator + unit tests
│   ├── merkle/         # OpenZeppelin-compatible Merkle tree/proof + tests
│   └── logfilter/      # Batch log filter engine (RPC, checkpoint, reorg protection, live mode, metrics)
├── solidity/
│   ├── contracts/MerkleNFTAllowlist.sol
│   ├── test/MerkleNFTAllowlist.test.js
│   ├── hardhat.config.js
│   └── package.json
├── scripts/merkle-gen.js
├── .github/workflows/ci.yml
└── README.md
```

## Go

```bash
go mod tidy

# Run demos
go run ./go/eip1559
go run ./go/merkle
go run ./go/logfilter

# Run all unit tests
go test ./go/...

# Vet
go vet ./...
```

### logfilter features

- Batched `eth_getLogs` with automatic batch shrink on range/rate-limit errors.
- Checkpoint persistence: memory, file, or Redis (`CheckpointStore`).
- **Reorg protection**: fetched logs sit in a `PendingPool` and are only handed to the handler after `ConfirmBlockNum` confirmations. Choose the in-memory `BlockPendingPool` (tests, short-lived) or `RedisPendingPool` (survives restarts; keys under `{prefix}:index` and `{prefix}:block:{height}`).
- Live mode: after catching up to chain head, polls for new blocks on an interval.
- Dual concurrency control: a global `RPCSemaphore` plus a per-task semaphore.
- Task watchdog: tasks that crash are restarted with exponential backoff; other tasks keep running.
- Prometheus metrics on `/metrics`:
  - `logfilter_rpc_retries_total`
  - `logfilter_rpc_request_total{task_name,status}`
  - `logfilter_logs_total`
  - `logfilter_batch_duration_seconds`
  - `logfilter_task_running`
  - `logfilter_task_healthy{task_name}`
  - `logfilter_task_unhealthy_seconds{task_name}`
  - `logfilter_task_restart_total{task_name}`
  - `logfilter_task_live_mode{task_name}`
  - `logfilter_rpc_concurrent_inflight`
  - `logfilter_pending_block_queue{task_name}`
- Batch handlers: `NoopBatchHandler`, `SQLBatchHandler` (Postgres-style upsert), `KafkaBatchHandler`.
- Graceful shutdown on `SIGINT` / `SIGTERM`.

### Suggested Prometheus alerts

```yaml
groups:
  - name: logfilter
    rules:
      - alert: TaskLongUnhealthy
        expr: logfilter_task_unhealthy_seconds > 30
        for: 10s
        labels: { severity: critical }
      - alert: RPCHighErrorRate
        expr: |
          sum(rate(logfilter_rpc_request_total{status="fail"}[5m]))
            / sum(rate(logfilter_rpc_request_total[5m])) > 0.1
        for: 1m
        labels: { severity: warning }
      - alert: PendingBlockQueueBacklog
        expr: logfilter_pending_block_queue > 100
        for: 2m
        labels: { severity: warning }
```

## Solidity / Hardhat

```bash
cd solidity
npm install
npx hardhat test
npx solhint "contracts/**/*.sol"
```

The contract (`MerkleNFTAllowlist.sol`) supports:

- Merkle-root allowlist mint (`wlMint(quantity, proof)`) with per-wallet limit.
- Public mint (`publicMint(quantity)`) with a separate per-wallet limit.
- Global `MAX_SUPPLY` cap.
- `Ownable`, `ReentrancyGuard`, events.

Generate a root + proof locally:

```bash
node scripts/merkle-gen.js
```

## CI

Every push / PR to `main` runs four parallel jobs:

1. `go-vet` — `go vet ./...`
2. `go-test` — `go test ./go/...`
3. `solhint` — Solidity lint
4. `hardhat-test` — contract unit tests

## Pushing to GitHub

```bash
# 1. Create an empty repo on GitHub named eip-toolkit (do NOT init README/LICENSE).
# 2. Inside this folder:
git init
git add .
git commit -m "feat: eip-1559 basefee, merkle allowlist NFT, logfilter engine, CI"
git branch -M main
git remote add origin https://github.com/<your-username>/eip-toolkit.git
git push -u origin main
```

Remember to update the module path in `go.mod` (`github.com/<your-username>/eip-toolkit`) after creating the repo.
