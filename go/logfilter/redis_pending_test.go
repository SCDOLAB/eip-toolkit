package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { cli.Close() })
	return cli, mr
}

func TestRedisPendingPool_AddPopCount(t *testing.T) {
	ctx := context.Background()
	cli, _ := newTestRedis(t)
	pool := NewRedisPendingPool(cli, "test")

	// Initially empty.
	if n, _ := pool.Count(ctx); n != 0 {
		t.Fatalf("initial count = %d, want 0", n)
	}

	logs := []LogResult{
		{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xabc", Address: "0xa"},
		{BlockNumber: "100", BlockNumUint: 100, TransactionHash: "0xdef", Address: "0xa"},
	}
	if err := pool.Add(ctx, 100, logs); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if n, _ := pool.Count(ctx); n != 1 {
		t.Fatalf("count after Add = %d, want 1", n)
	}

	// Not confirmed yet (latest=103, confirm=6 -> 100+6=106 > 103).
	got, err := pool.PopConfirmed(ctx, 103, 6)
	if err != nil {
		t.Fatalf("PopConfirmed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 confirmed logs, got %d", len(got))
	}
	if n, _ := pool.Count(ctx); n != 1 {
		t.Fatalf("pending should still hold block 100, count=%d", n)
	}

	// Now confirmed (latest=110, confirm=6 -> 100+6=106 <= 110).
	got, err = pool.PopConfirmed(ctx, 110, 6)
	if err != nil {
		t.Fatalf("PopConfirmed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 confirmed logs, got %d", len(got))
	}
	if n, _ := pool.Count(ctx); n != 0 {
		t.Fatalf("pending should be drained, count=%d", n)
	}
}

func TestRedisPendingPool_RestartRecovery(t *testing.T) {
	ctx := context.Background()
	cli, mr := newTestRedis(t)
	pool := NewRedisPendingPool(cli, "restart")

	logs := []LogResult{{BlockNumber: "200", BlockNumUint: 200, TransactionHash: "0xtx"}}
	if err := pool.Add(ctx, 200, logs); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Simulate restart: new pool instance on the same redis data.
	_ = mr
	pool2 := NewRedisPendingPool(cli, "restart")
	if n, _ := pool2.Count(ctx); n != 1 {
		t.Fatalf("after restart, pending count = %d, want 1", n)
	}
	got, err := pool2.PopConfirmed(ctx, 210, 6)
	if err != nil {
		t.Fatalf("PopConfirmed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected logs to survive restart, got %d", len(got))
	}
}
