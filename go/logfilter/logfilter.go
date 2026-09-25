// Package main implements a reusable Ethereum log filter engine with:
//   - batched eth_getLogs queries with auto batch shrink on range errors
//   - checkpoint persistence (memory / file / redis)
//   - block confirmation (reorg protection) via an in-memory pending pool
//   - batch callback handlers (noop / SQL / Kafka)
//   - live polling mode after catching up to the chain head
//   - dual concurrency limiting: global RPC semaphore + per-task semaphore
//   - task watchdog with exponential backoff restart
//   - Prometheus metrics exposed on /metrics
//   - SIGINT/SIGTERM graceful shutdown
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

// LogResult is the business-level log record.
type LogResult struct {
	BlockNumber     string
	BlockNumUint    uint64
	TransactionHash string
	Address         string
	Topics          []string
	Data            string
}

// uniqueKey identifies a log across retries (block + tx hash).
func (l LogResult) uniqueKey() string {
	h := sha256.Sum256([]byte(l.BlockNumber + "|" + l.TransactionHash))
	return hex.EncodeToString(h[:])
}

// ===================== Batch handler =====================

// LogBatchHandler consumes a batch of confirmed logs.
type LogBatchHandler interface {
	HandleBatch(ctx context.Context, logs []LogResult) error
}

// NoopBatchHandler does nothing.
type NoopBatchHandler struct{}

func (NoopBatchHandler) HandleBatch(context.Context, []LogResult) error { return nil }

// SQLBatchHandler writes confirmed logs to a relational database.
type SQLBatchHandler struct {
	db *sql.DB
}

// NewSQLBatchHandler wraps a *sql.DB.
func NewSQLBatchHandler(db *sql.DB) *SQLBatchHandler { return &SQLBatchHandler{db: db} }

// HandleBatch inserts logs in a transaction, ignoring duplicates via ON CONFLICT.
func (s *SQLBatchHandler) HandleBatch(ctx context.Context, logs []LogResult) error {
	if len(logs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO event_logs (block_number, tx_hash, contract_addr, topics, log_data)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (block_number, tx_hash) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, l := range logs {
		topicsStr := ""
		for i, t := range l.Topics {
			if i > 0 {
				topicsStr += ","
			}
			topicsStr += t
		}
		if _, err := stmt.ExecContext(ctx, l.BlockNumber, l.TransactionHash, l.Address, topicsStr, l.Data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// KafkaBatchHandler publishes confirmed logs to Kafka.
type KafkaBatchHandler struct {
	w *kafka.Writer
}

// NewKafkaBatchHandler creates a writer to topic.
func NewKafkaBatchHandler(brokers []string, topic string) *KafkaBatchHandler {
	return &KafkaBatchHandler{w: &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}}
}

// HandleBatch writes one message per log.
func (k *KafkaBatchHandler) HandleBatch(ctx context.Context, logs []LogResult) error {
	msgs := make([]kafka.Message, 0, len(logs))
	for _, l := range logs {
		payload := fmt.Sprintf(`{"block":%q,"tx":%q,"addr":%q}`, l.BlockNumber, l.TransactionHash, l.Address)
		msgs = append(msgs, kafka.Message{Key: []byte(l.TransactionHash), Value: []byte(payload)})
	}
	return k.w.WriteMessages(ctx, msgs...)
}

// Close releases the Kafka writer.
func (k *KafkaBatchHandler) Close() error { return k.w.Close() }

// ===================== Prometheus metrics =====================

var (
	metricRPCRetriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "logfilter_rpc_retries_total",
		Help: "Total number of RPC retries",
	})
	metricLogsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "logfilter_logs_total",
		Help: "Total fetched logs after dedup",
	})
	metricBatchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "logfilter_batch_duration_seconds",
		Help:    "Duration of each log fetch batch",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
	})
	metricTaskRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "logfilter_task_running",
		Help: "Number of running logfilter tasks",
	})
	metricTaskHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "logfilter_task_healthy",
		Help: "Task health status, 1=healthy, 0=unhealthy",
	}, []string{"task_name"})
	metricTaskRestartTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "logfilter_task_restart_total",
		Help: "Total task restart count",
	}, []string{"task_name"})
	metricRPCConcurrent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "logfilter_rpc_concurrent_inflight",
		Help: "Current inflight RPC requests controlled by semaphore",
	})
	metricTaskLiveMode = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "logfilter_task_live_mode",
		Help: "1=live listening mode, 0=catching up historical blocks",
	}, []string{"task_name"})
	metricPendingBlockCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "logfilter_pending_block_queue",
		Help: "Count of blocks waiting for N block confirmation",
	}, []string{"task_name"})
	metricRPCRequestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "logfilter_rpc_request_total",
		Help: "Total rpc requests, label status=ok/fail",
	}, []string{"task_name", "status"})
	metricTaskUnhealthySec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "logfilter_task_unhealthy_seconds",
		Help: "How long task has been unhealthy (seconds), for alerting",
	}, []string{"task_name"})
)

// ===================== Concurrency =====================

// RPCSemaphore bounds concurrent RPC calls.
type RPCSemaphore struct {
	sem chan struct{}
}

// NewRPCSemaphore creates a semaphore allowing maxConcurrent in-flight calls.
func NewRPCSemaphore(maxConcurrent int) *RPCSemaphore {
	return &RPCSemaphore{sem: make(chan struct{}, maxConcurrent)}
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s *RPCSemaphore) Acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.sem <- struct{}{}:
		metricRPCConcurrent.Inc()
		return nil
	}
}

// Release frees a slot.
func (s *RPCSemaphore) Release() {
	<-s.sem
	metricRPCConcurrent.Dec()
}

// ===================== Interfaces =====================

// RPCQuerier abstracts the Ethereum node.
type RPCQuerier interface {
	BlockNumber(ctx context.Context) (uint64, error)
	EthGetLogs(ctx context.Context, fromBlock, toBlock uint64, addresses []string, topics [][]string) ([]LogResult, error)
}

// CheckpointStore persists the last processed block.
type CheckpointStore interface {
	Save(ctx context.Context, block uint64) error
	Load(ctx context.Context) (uint64, error)
}

// PendingPool holds blocks that have not yet reached confirmation depth and
// hands them out once they are N blocks deep. Implementations may be
// in-memory (for tests) or Redis-backed (so pending logs survive restarts).
type PendingPool interface {
	Add(ctx context.Context, blockHeight uint64, logs []LogResult) error
	PopConfirmed(ctx context.Context, latestBlock, confirmNum uint64) ([]LogResult, error)
	Count(ctx context.Context) (int, error)
}

// BlockPendingPool is the in-memory PendingPool, used by tests and for
// short-lived processes.
type BlockPendingPool struct {
	mu   sync.Mutex
	data map[uint64][]LogResult
}

// NewBlockPendingPool creates an empty in-memory pool.
func NewBlockPendingPool() *BlockPendingPool {
	return &BlockPendingPool{data: make(map[uint64][]LogResult)}
}

// Add stores logs for a block height.
func (p *BlockPendingPool) Add(_ context.Context, blockHeight uint64, logs []LogResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data[blockHeight] = logs
	return nil
}

// PopConfirmed returns and removes logs for blocks at least confirmNum deep
// relative to latestBlock.
func (p *BlockPendingPool) PopConfirmed(_ context.Context, latestBlock, confirmNum uint64) ([]LogResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []LogResult
	for h, logs := range p.data {
		if h+confirmNum <= latestBlock {
			out = append(out, logs...)
			delete(p.data, h)
		}
	}
	return out, nil
}

// Count returns the number of pending blocks.
func (p *BlockPendingPool) Count(_ context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.data), nil
}

// RedisPendingPool persists pending blocks in Redis so that a process
// restart does not lose unconfirmed logs. Keys:
//
//	{prefix}:index            – SET of pending block heights
//	{prefix}:block:{height}   – JSON []LogResult for that block
type RedisPendingPool struct {
	rdb    *redis.Client
	prefix string
}

// NewRedisPendingPool creates a Redis-backed pool with the given key prefix
// (typically the task name, e.g. "logfilter:pending:contract_usdc").
func NewRedisPendingPool(rdb *redis.Client, keyPrefix string) *RedisPendingPool {
	return &RedisPendingPool{rdb: rdb, prefix: keyPrefix}
}

func (p *RedisPendingPool) indexKey() string        { return p.prefix + ":index" }
func (p *RedisPendingPool) blockKey(h uint64) string { return fmt.Sprintf("%s:block:%d", p.prefix, h) }

// Add serializes logs as JSON and stores them under a per-height key, then
// adds the height to the index set.
func (p *RedisPendingPool) Add(ctx context.Context, blockHeight uint64, logs []LogResult) error {
	payload, err := json.Marshal(logs)
	if err != nil {
		return fmt.Errorf("marshal pending logs: %w", err)
	}
	pipe := p.rdb.TxPipeline()
	pipe.Set(ctx, p.blockKey(blockHeight), payload, 0)
	pipe.SAdd(ctx, p.indexKey(), blockHeight)
	_, err = pipe.Exec(ctx)
	return err
}

// PopConfirmed scans the index set, pops heights that have reached
// confirmation depth, reads and deletes their JSON payloads.
func (p *RedisPendingPool) PopConfirmed(ctx context.Context, latestBlock, confirmNum uint64) ([]LogResult, error) {
	heights, err := p.rdb.SMembers(ctx, p.indexKey()).Result()
	if err != nil {
		return nil, err
	}
	var out []LogResult
	pipe := p.rdb.TxPipeline()
	for _, hs := range heights {
		h, err := strconv.ParseUint(hs, 10, 64)
		if err != nil {
			continue
		}
		if h+confirmNum > latestBlock {
			continue
		}
		payload, err := p.rdb.Get(ctx, p.blockKey(h)).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				pipe.SRem(ctx, p.indexKey(), h)
				continue
			}
			return nil, err
		}
		var logs []LogResult
		if err := json.Unmarshal(payload, &logs); err != nil {
			return nil, fmt.Errorf("unmarshal pending block %d: %w", h, err)
		}
		out = append(out, logs...)
		pipe.Del(ctx, p.blockKey(h))
		pipe.SRem(ctx, p.indexKey(), h)
	}
	if len(out) > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Count returns the size of the pending index set.
func (p *RedisPendingPool) Count(ctx context.Context) (int, error) {
	n, err := p.rdb.SCard(ctx, p.indexKey()).Result()
	return int(n), err
}

// ===================== Task =====================

// LogTask is one contract log-filter job.
type LogTask struct {
	TaskName     string
	Address      []string
	Topics       [][]string
	Checkpoint   uint64
	BatchSize    uint64
	RPC          RPCQuerier
	Store        CheckpointStore
	BatchHandler LogBatchHandler

	LiveMode         bool
	LivePollInterval time.Duration
	RestartBackoff   time.Duration
	MaxBackoff       time.Duration

	RPCSem       *RPCSemaphore
	TaskLocalSem *RPCSemaphore

	ConfirmBlockNum uint64
	PendingPool     PendingPool

	unhealthyStart time.Time
}

// IsRateLimitOrRangeErr reports whether an error suggests shrinking the batch.
func IsRateLimitOrRangeErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "block range too large" || msg == "query timeout" || msg == "rate limit exceeded"
}

// RunOnce performs one sync round: pull blocks up to chain head, buffer
// unconfirmed ones, and emit confirmed logs to the handler.
func (t *LogTask) RunOnce(ctx context.Context) (caughtUp bool, err error) {
	latestBlock, err := t.RPC.BlockNumber(ctx)
	if err != nil {
		metricRPCRequestTotal.WithLabelValues(t.TaskName, "fail").Inc()
		return false, fmt.Errorf("get latest block: %w", err)
	}
	metricRPCRequestTotal.WithLabelValues(t.TaskName, "ok").Inc()

	if t.Checkpoint > latestBlock {
		return true, nil
	}

	currentStart := t.Checkpoint
	for currentStart <= latestBlock {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		if err := t.TaskLocalSem.Acquire(ctx); err != nil {
			return false, err
		}
		if err := t.RPCSem.Acquire(ctx); err != nil {
			t.TaskLocalSem.Release()
			return false, err
		}
		batchStart := time.Now()

		end := currentStart + t.BatchSize - 1
		if end > latestBlock {
			end = latestBlock
		}

		logs, err := t.RPC.EthGetLogs(ctx, currentStart, end, t.Address, t.Topics)
		t.RPCSem.Release()
		t.TaskLocalSem.Release()

		if err != nil {
			metricRPCRequestTotal.WithLabelValues(t.TaskName, "fail").Inc()
			if IsRateLimitOrRangeErr(err) {
				t.BatchSize /= 2
				if t.BatchSize < 1 {
					return false, fmt.Errorf("batch reduced to 1, still failing: %w", err)
				}
				continue
			}
			return false, fmt.Errorf("rpc query: %w", err)
		}
		metricRPCRequestTotal.WithLabelValues(t.TaskName, "ok").Inc()

		// Dedup within batch.
		seen := make(map[string]struct{}, len(logs))
		var batchDedup []LogResult
		for _, l := range logs {
			k := l.uniqueKey()
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				batchDedup = append(batchDedup, l)
			}
		}
		metricLogsTotal.Add(float64(len(batchDedup)))

		if err := t.PendingPool.Add(ctx, currentStart, batchDedup); err != nil {
			return false, fmt.Errorf("pending add: %w", err)
		}

		ready, err := t.PendingPool.PopConfirmed(ctx, latestBlock, t.ConfirmBlockNum)
		if err != nil {
			return false, fmt.Errorf("pending pop: %w", err)
		}
		if len(ready) > 0 {
			if err := t.BatchHandler.HandleBatch(ctx, ready); err != nil {
				return false, fmt.Errorf("batch handler: %w", err)
			}
			if err := t.Store.Save(ctx, end); err != nil {
				return false, fmt.Errorf("save checkpoint: %w", err)
			}
			t.Checkpoint = end
		}

		metricBatchDuration.Observe(time.Since(batchStart).Seconds())
		if pending, err := t.PendingPool.Count(ctx); err == nil {
			metricPendingBlockCount.WithLabelValues(t.TaskName).Set(float64(pending))
		}
		currentStart = end + 1
	}
	return true, nil
}

// TaskLoop is the main task loop: catch up history, then optionally poll live.
func (t *LogTask) TaskLoop(ctx context.Context) error {
	if t.PendingPool == nil {
		t.PendingPool = NewBlockPendingPool()
	}
	metricTaskHealth.WithLabelValues(t.TaskName).Set(1)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	defer func() {
		metricTaskHealth.WithLabelValues(t.TaskName).Set(0)
		metricTaskUnhealthySec.WithLabelValues(t.TaskName).Set(0)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		caughtUp, err := t.RunOnce(ctx)
		if err != nil {
			if t.unhealthyStart.IsZero() {
				t.unhealthyStart = time.Now()
			}
			metricTaskUnhealthySec.WithLabelValues(t.TaskName).Set(time.Since(t.unhealthyStart).Seconds())
			return err
		}
		t.unhealthyStart = time.Time{}
		metricTaskUnhealthySec.WithLabelValues(t.TaskName).Set(0)

		if !t.LiveMode {
			return nil
		}
		if caughtUp {
			metricTaskLiveMode.WithLabelValues(t.TaskName).Set(1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(t.LivePollInterval):
			}
		} else {
			metricTaskLiveMode.WithLabelValues(t.TaskName).Set(0)
		}
	}
}

// ===================== RealRPC =====================

// RealRPC is the production RPCQuerier backed by go-ethereum.
type RealRPC struct {
	client        *ethclient.Client
	maxRetries    int
	retryBaseWait time.Duration
}

// RealRPCOption configures RealRPC.
type RealRPCOption func(*RealRPC)

// WithMaxRetries sets the retry budget.
func WithMaxRetries(n int) RealRPCOption {
	return func(r *RealRPC) { r.maxRetries = n }
}

// WithRetryBaseWait sets the initial exponential backoff.
func WithRetryBaseWait(d time.Duration) RealRPCOption {
	return func(r *RealRPC) { r.retryBaseWait = d }
}

// NewRealRPC dials an Ethereum RPC endpoint.
func NewRealRPC(rpcEndpoint string, opts ...RealRPCOption) (*RealRPC, error) {
	c, err := ethclient.Dial(rpcEndpoint)
	if err != nil {
		return nil, err
	}
	r := &RealRPC{client: c, maxRetries: 3, retryBaseWait: 500 * time.Millisecond}
	for _, o := range opts {
		o(r)
	}
	return r, nil
}

// Close releases the underlying client.
func (r *RealRPC) Close() { r.client.Close() }

// BlockNumber returns the chain head with retries.
func (r *RealRPC) BlockNumber(ctx context.Context) (uint64, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		bn, err := r.client.BlockNumber(ctx)
		if err == nil {
			return bn, nil
		}
		lastErr = err
		metricRPCRetriesTotal.Inc()
		if attempt == r.maxRetries {
			break
		}
		wait := r.retryBaseWait * (1 << attempt)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return 0, ctx.Err()
		case <-t.C:
		}
	}
	return 0, fmt.Errorf("block number retries exhausted: %w", lastErr)
}

// EthGetLogs calls FilterLogs with retries.
func (r *RealRPC) EthGetLogs(ctx context.Context, fromBlock, toBlock uint64, addresses []string, topics [][]string) ([]LogResult, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		res, err := r.fetchRawLogs(ctx, fromBlock, toBlock, addresses, topics)
		if err == nil {
			return res, nil
		}
		lastErr = err
		metricRPCRetriesTotal.Inc()
		if attempt == r.maxRetries {
			break
		}
		wait := r.retryBaseWait * (1 << attempt)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	return nil, fmt.Errorf("get logs retries exhausted: %w", lastErr)
}

func (r *RealRPC) fetchRawLogs(ctx context.Context, fromBlock, toBlock uint64, addresses []string, topics [][]string) ([]LogResult, error) {
	filter := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
	}
	if len(addresses) > 0 {
		addrs := make([]common.Address, 0, len(addresses))
		for _, a := range addresses {
			addrs = append(addrs, common.HexToAddress(a))
		}
		filter.Addresses = addrs
	}
	if len(topics) > 0 {
		filter.Topics = make([][]common.Hash, 0, len(topics))
		for _, grp := range topics {
			hg := make([]common.Hash, 0, len(grp))
			for _, t := range grp {
				hg = append(hg, common.HexToHash(t))
			}
			filter.Topics = append(filter.Topics, hg)
		}
	}
	raw, err := r.client.FilterLogs(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]LogResult, 0, len(raw))
	for _, l := range raw {
		topicsStr := make([]string, 0, len(l.Topics))
		for _, t := range l.Topics {
			topicsStr = append(topicsStr, t.Hex())
		}
		out = append(out, LogResult{
			BlockNumber:     strconv.FormatUint(l.BlockNumber, 10),
			BlockNumUint:    l.BlockNumber,
			TransactionHash: l.TxHash.Hex(),
			Address:         l.Address.Hex(),
			Topics:          topicsStr,
			Data:            common.Bytes2Hex(l.Data),
		})
	}
	return out, nil
}

// ===================== Checkpoint stores =====================

// MemoryCheckpointStore keeps the checkpoint in memory.
type MemoryCheckpointStore struct {
	mu  sync.Mutex
	val uint64
}

// NewMemoryCheckpointStore starts at initVal.
func NewMemoryCheckpointStore(initVal uint64) *MemoryCheckpointStore {
	return &MemoryCheckpointStore{val: initVal}
}

// Save implements CheckpointStore.
func (m *MemoryCheckpointStore) Save(_ context.Context, block uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.val = block
	return nil
}

// Load implements CheckpointStore.
func (m *MemoryCheckpointStore) Load(_ context.Context) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.val, nil
}

// FileCheckpointStore persists the checkpoint as a decimal text file.
type FileCheckpointStore struct {
	path string
	mu   sync.Mutex
}

// NewFileCheckpointStore stores to path.
func NewFileCheckpointStore(path string) *FileCheckpointStore {
	return &FileCheckpointStore{path: path}
}

// Save implements CheckpointStore.
func (f *FileCheckpointStore) Save(ctx context.Context, block uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return os.WriteFile(f.path, []byte(strconv.FormatUint(block, 10)), 0o644)
}

// Load implements CheckpointStore.
func (f *FileCheckpointStore) Load(ctx context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return strconv.ParseUint(string(data), 10, 64)
}

// RedisCheckpointStore stores the checkpoint in Redis.
type RedisCheckpointStore struct {
	rdb *redis.Client
	key string
}

// NewRedisCheckpointStore stores under key.
func NewRedisCheckpointStore(rdb *redis.Client, key string) *RedisCheckpointStore {
	return &RedisCheckpointStore{rdb: rdb, key: key}
}

// Save implements CheckpointStore.
func (r *RedisCheckpointStore) Save(ctx context.Context, block uint64) error {
	return r.rdb.Set(ctx, r.key, block, 0).Err()
}

// Load implements CheckpointStore.
func (r *RedisCheckpointStore) Load(ctx context.Context) (uint64, error) {
	v, err := r.rdb.Get(ctx, r.key).Uint64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}

// ===================== Task runner =====================

// TaskRunner runs multiple LogTasks concurrently with automatic restart.
type TaskRunner struct {
	tasks []*LogTask
	wg    sync.WaitGroup
}

// NewTaskRunner creates an empty runner.
func NewTaskRunner() *TaskRunner { return &TaskRunner{} }

// AddTask registers a task.
func (r *TaskRunner) AddTask(t *LogTask) { r.tasks = append(r.tasks, t) }

// RunAll starts all tasks and blocks until ctx is cancelled.
func (r *TaskRunner) RunAll(ctx context.Context) {
	for _, t := range r.tasks {
		task := t
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			backoff := task.RestartBackoff
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				metricTaskRunning.Inc()
				err := task.TaskLoop(ctx)
				metricTaskRunning.Dec()
				if errors.Is(err, ctx.Err()) {
					fmt.Printf("task %s shutdown\n", task.TaskName)
					return
				}
				metricTaskRestartTotal.WithLabelValues(task.TaskName).Inc()
				fmt.Printf("task %s failed: %v; restarting in %v\n", task.TaskName, err, backoff)
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				backoff *= 2
				if backoff > task.MaxBackoff {
					backoff = task.MaxBackoff
				}
			}
		}()
	}
	r.wg.Wait()
}

// Wait blocks until all tasks have stopped.
func (r *TaskRunner) Wait() { r.wg.Wait() }

// ===================== HTTP / shutdown =====================

// StartMetricsServer exposes Prometheus metrics on addr/metrics.
func StartMetricsServer(addr string, stopCh <-chan struct{}) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-stopCh
		_ = srv.Shutdown(context.Background())
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("metrics server error: %v\n", err)
		}
	}()
	fmt.Printf("metrics server on http://%s/metrics\n", addr)
	return srv
}

// NewShutdownContext returns a ctx cancelled on SIGINT/SIGTERM and a stop
// channel closed once the signal has been received.
func NewShutdownContext(parent context.Context) (context.Context, <-chan struct{}) {
	ctx, cancel := context.WithCancel(parent)
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Println("\nreceived shutdown signal, stopping...")
			cancel()
			close(stopCh)
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()
	return ctx, stopCh
}

func main() {
	ctx, stopCh := NewShutdownContext(context.Background())
	StartMetricsServer(":8080", stopCh)

	globalRPCSem := NewRPCSemaphore(3)
	_ = globalRPCSem // in production, build RealRPC and tasks; see README.

	fmt.Println("logfilter skeleton running. configure RPC endpoint and tasks in main()")
	<-ctx.Done()
	fmt.Println("bye")
}
