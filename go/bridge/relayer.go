// Package bridge implements a minimal cross-chain relayer between an EVM chain
// (Ethereum) and another EVM-compatible chain (SCDO).
//
// Flow:
//   1. User deposits ERC20 on Ethereum BridgeLock -> Deposited event.
//   2. Relayer sees event -> calls BridgeMint.mint on SCDO.
//   3. User burns pegged tokens on SCDO -> Burned event.
//   4. Relayer sees Burned -> signs withdrawal and calls BridgeLock.withdraw on Ethereum.
package bridge

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// DepositedEvent is parsed from BridgeLock.Deposited.
type DepositedEvent struct {
	Token        string
	User         string
	Amount       *big.Int
	Nonce        uint64
	DestChainID  uint64
	SourceBlock  uint64
	TxHash       string
}

// BurnedEvent is parsed from BridgeMint.Burned.
type BurnedEvent struct {
	From          string
	Amount        *big.Int
	WithdrawNonce uint64
	SourceBlock   uint64
	TxHash        string
}

// ChainClient abstracts one side of the bridge (Ethereum or SCDO).
type ChainClient interface {
	// WatchDeposited streams Deposited events from BridgeLock since `fromBlock`.
	WatchDeposited(ctx context.Context, fromBlock uint64) (<-chan DepositedEvent, error)
	// WatchBurned streams Burned events from BridgeMint since `fromBlock`.
	WatchBurned(ctx context.Context, fromBlock uint64) (<-chan BurnedEvent, error)
	// Mint calls BridgeMint.mint on the destination chain.
	Mint(ctx context.Context, to string, amount *big.Int, sourceNonce uint64) (string, error)
	// Withdraw signs and calls BridgeLock.withdraw on the source chain.
	Withdraw(ctx context.Context, to string, amount *big.Int, nonce uint64) (string, error)
}

// Relayer coordinates the two chain clients.
type Relayer struct {
	eth    ChainClient // Ethereum side (BridgeLock)
	scdo   ChainClient // SCDO side (BridgeMint)

	ethFromBlock  uint64
	scdoFromBlock uint64

	pollInterval time.Duration
	logger      *log.Logger

	// dedup
	seenMinted   map[uint64]bool
	seenWithdrawn map[uint64]bool
	mu           sync.Mutex
}

// Option configures Relayer.
type Option func(*Relayer)

func WithStartBlock(ethFrom, scdoFrom uint64) Option {
	return func(r *Relayer) {
		r.ethFromBlock = ethFrom
		r.scdoFromBlock = scdoFrom
	}
}

func WithPollInterval(d time.Duration) Option {
	return func(r *Relayer) { r.pollInterval = d }
}

func New(eth, scdo ChainClient, opts ...Option) *Relayer {
	r := &Relayer{
		eth:          eth,
		scdo:         scdo,
		pollInterval:  12 * time.Second,
		logger:       log.Default(),
		seenMinted:   make(map[uint64]bool),
		seenWithdrawn: make(map[uint64]bool),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Run starts the relayer loop until ctx is cancelled.
func (r *Relayer) Run(ctx context.Context) error {
	r.logger.Printf("bridge relayer started: ethFromBlock=%d scdoFromBlock=%d", r.ethFromBlock, r.scdoFromBlock)

	// Watch Ethereum deposits -> mint on SCDO.
	depChan, err := r.eth.WatchDeposited(ctx, r.ethFromBlock)
	if err != nil {
		return fmt.Errorf("watch deposits: %w", err)
	}

	// Watch SCDO burns -> withdraw on Ethereum.
	burnChan, err := r.scdo.WatchBurned(ctx, r.scdoFromBlock)
	if err != nil {
		return fmt.Errorf("watch burns: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			r.logger.Println("relayer stopped")
			return ctx.Err()

		case dep, ok := <-depChan:
			if !ok {
				r.logger.Println("deposit channel closed, restarting watch")
				depChan, err = r.eth.WatchDeposited(ctx, r.ethFromBlock)
				if err != nil {
					return err
				}
				continue
			}
			r.handleDeposit(ctx, dep)

		case burn, ok := <-burnChan:
			if !ok {
				r.logger.Println("burn channel closed, restarting watch")
				burnChan, err = r.scdo.WatchBurned(ctx, r.scdoFromBlock)
				if err != nil {
					return err
				}
				continue
			}
			r.handleBurn(ctx, burn)
		}
	}
}

func (r *Relayer) handleDeposit(ctx context.Context, d DepositedEvent) {
	r.mu.Lock()
	if r.seenMinted[d.Nonce] {
		r.mu.Unlock()
		return
	}
	r.seenMinted[d.Nonce] = true
	r.mu.Unlock()

	r.logger.Printf("deposit seen: nonce=%d user=%s amount=%s -> minting on SCDO",
		d.Nonce, d.User, d.Amount.String())

	tx, err := r.scdo.Mint(ctx, d.User, d.Amount, d.Nonce)
	if err != nil {
		r.logger.Printf("Mint failed for nonce=%d: %v", d.Nonce, err)
		// Remove from seen so we retry on next poll.
		r.mu.Lock()
		delete(r.seenMinted, d.Nonce)
		r.mu.Unlock()
		return
	}
	r.logger.Printf("minted on SCDO: nonce=%d tx=%s", d.Nonce, tx)
}

func (r *Relayer) handleBurn(ctx context.Context, b BurnedEvent) {
	r.mu.Lock()
	if r.seenWithdrawn[b.WithdrawNonce] {
		r.mu.Unlock()
		return
	}
	r.seenWithdrawn[b.WithdrawNonce] = true
	r.mu.Unlock()

	r.logger.Printf("burn seen: nonce=%d from=%s amount=%s -> withdrawing on Ethereum",
		b.WithdrawNonce, b.From, b.Amount.String())

	tx, err := r.eth.Withdraw(ctx, b.From, b.Amount, b.WithdrawNonce)
	if err != nil {
		r.logger.Printf("Withdraw failed for nonce=%d: %v", b.WithdrawNonce, err)
		r.mu.Lock()
		delete(r.seenWithdrawn, b.WithdrawNonce)
		r.mu.Unlock()
		return
	}
	r.logger.Printf("withdrew on Ethereum: nonce=%d tx=%s", b.WithdrawNonce, tx)
}
