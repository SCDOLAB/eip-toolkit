package bridge

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"
)

// mockClient implements ChainClient for testing.
type mockClient struct {
	deposited []DepositedEvent
	burned    []BurnedEvent

	mu           sync.Mutex
	mintCalls    []mintCall
	withdrawCalls []withdrawCall
	mintErr      error
	withdrawErr  error
}

type mintCall struct {
	to        string
	amount    *big.Int
	sourceNonce uint64
}

type withdrawCall struct {
	to    string
	amount *big.Int
	nonce uint64
}

func (m *mockClient) WatchDeposited(ctx context.Context, fromBlock uint64) (<-chan DepositedEvent, error) {
	ch := make(chan DepositedEvent, len(m.deposited))
	for _, d := range m.deposited {
		ch <- d
	}
	return ch, nil
}

func (m *mockClient) WatchBurned(ctx context.Context, fromBlock uint64) (<-chan BurnedEvent, error) {
	ch := make(chan BurnedEvent, len(m.burned))
	for _, b := range m.burned {
		ch <- b
	}
	return ch, nil
}

func (m *mockClient) Mint(ctx context.Context, to string, amount *big.Int, sourceNonce uint64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mintErr != nil {
		return "", m.mintErr
	}
	m.mintCalls = append(m.mintCalls, mintCall{to, amount, sourceNonce})
	return "0xminttx", nil
}

func (m *mockClient) Withdraw(ctx context.Context, to string, amount *big.Int, nonce uint64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.withdrawErr != nil {
		return "", m.withdrawErr
	}
	m.withdrawCalls = append(m.withdrawCalls, withdrawCall{to, amount, nonce})
	return "0xwithdrawtx", nil
}

func TestRelayer_ForwardsDepositToMint(t *testing.T) {
	eth := &mockClient{
		deposited: []DepositedEvent{
			{Token: "0xtoken", User: "0xuser1", Amount: big.NewInt(100), Nonce: 1, SourceBlock: 100},
		},
	}
	scdo := &mockClient{}

	r := New(eth, scdo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go r.Run(ctx)
	<-ctx.Done()

	if len(scdo.mintCalls) != 1 {
		t.Fatalf("expected 1 mint call, got %d", len(scdo.mintCalls))
	}
	if scdo.mintCalls[0].to != "0xuser1" || scdo.mintCalls[0].sourceNonce != 1 {
		t.Fatalf("unexpected mint call: %+v", scdo.mintCalls[0])
	}
}

func TestRelayer_ForwardsBurnToWithdraw(t *testing.T) {
	eth := &mockClient{}
	scdo := &mockClient{
		burned: []BurnedEvent{
			{From: "0xuser2", Amount: big.NewInt(50), WithdrawNonce: 7},
		},
	}

	r := New(eth, scdo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go r.Run(ctx)
	<-ctx.Done()

	if len(eth.withdrawCalls) != 1 {
		t.Fatalf("expected 1 withdraw call, got %d", len(eth.withdrawCalls))
	}
	if eth.withdrawCalls[0].nonce != 7 {
		t.Fatalf("unexpected withdraw call: %+v", eth.withdrawCalls[0])
	}
}

func TestRelayer_Dedup(t *testing.T) {
	eth := &mockClient{
		deposited: []DepositedEvent{
			{Token: "0xt", User: "0xu", Amount: big.NewInt(1), Nonce: 1},
			{Token: "0xt", User: "0xu", Amount: big.NewInt(1), Nonce: 1}, // duplicate
		},
	}
	scdo := &mockClient{}

	r := New(eth, scdo)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go r.Run(ctx)
	<-ctx.Done()

	if len(scdo.mintCalls) != 1 {
		t.Fatalf("expected dedup to 1 mint call, got %d", len(scdo.mintCalls))
	}
}
