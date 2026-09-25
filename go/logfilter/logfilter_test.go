package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---- mocks ----

type mockRPC struct {
	latestBlock uint64
	logs        map[uint64][]LogResult // by fromBlock
	blockErr    error
	logsErr     error
}

func (m *mockRPC) BlockNumber(context.Context) (uint64, error) {
	return m.latestBlock, m.blockErr
}

func (m *mockRPC) EthGetLogs(_ context.Context, from, to uint64, _ []string, _ [][]string) ([]LogResult, error) {
	if m.logsErr != nil {
		return nil, m.logsErr
	}
	// collect logs for blocks in [from,to]
	var out []LogResult
	for h := from; h <= to; h++ {
		out = append(out, m.logs[h]...)
	}
	return out, nil
}

type mockStore struct {
	saved  uint64
	saveFn func(block uint64) error
}

func (m *mockStore) Save(_ context.Context, block uint64) error {
	if m.saveFn != nil {
		if err := m.saveFn(block); err != nil {
			return err
		}
	}
	m.saved = block
	return nil
}
func (m *mockStore) Load(context.Context) (uint64, error) { return m.saved, nil }

type mockHandler struct {
	logs []LogResult
	err  error
}

func (h *mockHandler) HandleBatch(_ context.Context, logs []LogResult) error {
	if h.err != nil {
		return h.err
	}
	h.logs = append(h.logs, logs...)
	return nil
}

func newTestTask(t *testing.T, rpc RPCQuerier, store CheckpointStore, h LogBatchHandler) *LogTask {
	t.Helper()
	return &LogTask{
		TaskName:        "test",
		Checkpoint:      100,
		BatchSize:       10,
		RPC:             rpc,
		Store:           store,
		BatchHandler:   h,
		RPCSem:          NewRPCSemaphore(4),
		TaskLocalSem:    NewRPCSemaphore(2),
		ConfirmBlockNum: 6,
		PendingPool:     NewBlockPendingPool(),
	}
}

// ---- tests ----

func TestIsRateLimitOrRangeErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"range too large", errors.New("block range too large"), true},
		{"rate limit", errors.New("rate limit exceeded"), true},
		{"other", errors.New("connection reset"), false},
	}
	for _, c := range cases {
		if got := IsRateLimitOrRangeErr(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestRunOnce_PendingBlocksNotProcessedUntilConfirmDepth(t *testing.T) {
	// chain head = 105, checkpoint = 100, confirmNum = 6
	// block 100 needs head >= 106 to be emitted.
	rpc := &mockRPC{latestBlock: 105, logs: map[uint64][]LogResult{
		100: {{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xabc"}},
	}}
	store := &mockStore{}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)

	caughtUp, err := task.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !caughtUp {
		t.Error("expected caughtUp after reaching head")
	}
	if len(h.logs) != 0 {
		t.Errorf("expected no confirmed logs yet, got %d", len(h.logs))
	}
	pending, _ := task.PendingPool.Count(context.Background())
	if pending != 1 {
		t.Errorf("expected 1 pending block, got %d", pending)
	}
	if store.saved != 0 {
		t.Errorf("checkpoint must not advance while logs are pending, got %d", store.saved)
	}
}

func TestRunOnce_EmitsConfirmedLogs(t *testing.T) {
	// head = 110, confirmNum = 6 -> block 100 is confirmed.
	rpc := &mockRPC{latestBlock: 110, logs: map[uint64][]LogResult{
		100: {{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xabc"}},
	}}
	store := &mockStore{}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)

	if _, err := task.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(h.logs) != 1 {
		t.Fatalf("expected 1 emitted log, got %d", len(h.logs))
	}
	if store.saved != 109 { // end of first batch (100..109)
		t.Errorf("checkpoint = %d, want 109", store.saved)
	}
}

func TestRunOnce_DedupWithinBatch(t *testing.T) {
	dup := LogResult{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xdup", Address: "0xa"}
	rpc := &mockRPC{latestBlock: 200, logs: map[uint64][]LogResult{
		100: {dup, dup, dup},
	}}
	store := &mockStore{}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)
	task.ConfirmBlockNum = 0 // emit immediately

	if _, err := task.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.logs) != 1 {
		t.Errorf("dedup failed: got %d logs, want 1", len(h.logs))
	}
}

func TestRunOnce_BatchHandlerError(t *testing.T) {
	rpc := &mockRPC{latestBlock: 200, logs: map[uint64][]LogResult{
		100: {{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xabc"}},
	}}
	store := &mockStore{}
	h := &mockHandler{err: errors.New("handler boom")}
	task := newTestTask(t, rpc, store, h)
	task.ConfirmBlockNum = 0

	if _, err := task.RunOnce(context.Background()); err == nil {
		t.Error("expected handler error")
	}
}

func TestRunOnce_StoreSaveError(t *testing.T) {
	rpc := &mockRPC{latestBlock: 200, logs: map[uint64][]LogResult{
		100: {{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xabc"}},
	}}
	store := &mockStore{saveFn: func(uint64) error { return errors.New("disk full") }}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)
	task.ConfirmBlockNum = 0

	if _, err := task.RunOnce(context.Background()); err == nil {
		t.Error("expected store error")
	}
}

func TestRunOnce_AutoShrinkOnRangeError(t *testing.T) {
	rpc := &mockRPC{latestBlock: 200, logsErr: errors.New("block range too large")}
	store := &mockStore{}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)

	_, err := task.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected error after batch shrinks to 1")
	}
	if task.BatchSize >= 10 {
		t.Errorf("batch size should have shrunk, got %d", task.BatchSize)
	}
}

func TestTaskLoop_ContextCancel(t *testing.T) {
	rpc := &mockRPC{latestBlock: 200}
	store := &mockStore{}
	h := &mockHandler{}
	task := newTestTask(t, rpc, store, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := task.TaskLoop(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := NewRPCSemaphore(1)
	ctx := context.Background()
	if err := sem.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = sem.Acquire(ctx)
		close(done)
	}()
	select {
	case <-done:
		t.Error("second acquire should block until release")
	case <-time.After(50 * time.Millisecond):
	}
	sem.Release()
	<-done
}
