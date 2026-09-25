// logfilter daemon entrypoint.
//
// Required env:
//   RPC_URL          e.g. https://eth-sepolia.g.alchemy.com/v2/KEY
//   CONTRACT_ADDRESS 0x... (repeatable, comma separated)
//
// Optional env:
//   START_BLOCK          (default: 0 = start from chain head)
//   CONFIRM_BLOCKS       (default: 6)
//   BATCH_SIZE           (default: 1000)
//   LIVE_POLL_INTERVAL   (default: 12s)
//   REDIS_ADDR           (e.g. redis:6379; if empty, uses in-memory pending pool + file checkpoint)
//   CHECKPOINT_FILE      (default: ./checkpoint.json)
//   METRICS_ADDR         (default: :8080)
//   KAFKA_BROKERS        (comma separated; if set, logs are produced to KAFKA_TOPIC)
//   KAFKA_TOPIC
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envUint64(key string, def uint64) uint64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// fileCheckpoint is a tiny CheckpointStore backed by a JSON file.
type fileCheckpoint struct {
	path string
}

func (f *fileCheckpoint) Load(_ context.Context) (uint64, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var m map[string]uint64
	if err := json.Unmarshal(b, &m); err != nil {
		return 0, err
	}
	return m["lastBlock"], nil
}

func (f *fileCheckpoint) Save(_ context.Context, block uint64) error {
	m := map[string]uint64{"lastBlock": block}
	b, _ := json.Marshal(m)
	return os.WriteFile(f.path, b, 0o644)
}

func main() {
	flag.Parse()

	rpcURL := env("RPC_URL", "")
	if rpcURL == "" {
		log.Fatal("RPC_URL is required")
	}
	contracts := strings.Split(env("CONTRACT_ADDRESS", ""), ",")
	for i := range contracts {
		contracts[i] = strings.TrimSpace(contracts[i])
	}
	if len(contracts) == 0 || contracts[0] == "" {
		log.Fatal("CONTRACT_ADDRESS is required")
	}

	confirmBlocks := envUint64("CONFIRM_BLOCKS", 6)
	batchSize := envUint64("BATCH_SIZE", 1000)
	startBlock := envUint64("START_BLOCK", 0)
	liveInterval, _ := time.ParseDuration(env("LIVE_POLL_INTERVAL", "12s"))
	metricsAddr := env("METRICS_ADDR", ":8080")
	redisAddr := env("REDIS_ADDR", "")
	checkpointFile := env("CHECKPOINT_FILE", "./checkpoint.json")

	// Metrics endpoint.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: metricsAddr, Handler: mux}
	go func() {
		log.Printf("metrics on http://%s/metrics", metricsAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("metrics server: %v", err)
		}
	}()

	// RPC client.
	rpc, err := NewRealRPC(rpcURL, WithMaxRetries(3))
	if err != nil {
		log.Fatalf("dial rpc: %v", err)
	}
	defer rpc.Close()

	// Checkpoint store.
	store := &fileCheckpoint{path: checkpointFile}
	if startBlock == 0 {
		if saved, err := store.Load(context.Background()); err == nil && saved > 0 {
			startBlock = saved
			log.Printf("resuming from checkpoint block %d", startBlock)
		} else {
			head, err := rpc.BlockNumber(context.Background())
			if err != nil {
				log.Fatalf("get chain head: %v", err)
			}
			startBlock = head
			log.Printf("starting from chain head %d", startBlock)
		}
	}

	// Pending pool: redis if configured, else in-memory.
	var pending PendingPool
	if redisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
		pending = NewRedisPendingPool(rdb, "logfilter")
		log.Printf("using redis pending pool at %s", redisAddr)
	} else {
		pending = NewBlockPendingPool()
		log.Printf("using in-memory pending pool")
	}

	// Batch handler.
	var handler LogBatchHandler = NoopBatchHandler{}
	if kb := env("KAFKA_BROKERS", ""); kb != "" {
		topic := env("KAFKA_TOPIC", "evm_logs")
		brokers := strings.Split(kb, ",")
		handler = NewKafkaBatchHandler(brokers, topic)
		log.Printf("kafka handler -> %s/%s", brokers, topic)
	}

	task := &LogTask{
		TaskName:        "primary",
		Address:         contracts,
		Topics:          nil,
		Checkpoint:      startBlock,
		BatchSize:       batchSize,
		RPC:             rpc,
		Store:           store,
		BatchHandler:    handler,
		LiveMode:        true,
		LivePollInterval: liveInterval,
		RestartBackoff:  5 * time.Second,
		MaxBackoff:      2 * time.Minute,
		RPCSem:          NewRPCSemaphore(10),
		TaskLocalSem:    NewRPCSemaphore(3),
		ConfirmBlockNum: confirmBlocks,
		PendingPool:     pending,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigCh
		log.Printf("received %s, shutting down", s)
		cancel()
		shutdownCtx, sh := context.WithTimeout(context.Background(), 5*time.Second)
		defer sh()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("logfilter running: addresses=%v startBlock=%d confirm=%d batch=%d",
		contracts, startBlock, confirmBlocks, batchSize)
	if err := task.TaskLoop(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "task loop exited: %v\n", err)
		os.Exit(1)
	}
	log.Println("stopped")
}
