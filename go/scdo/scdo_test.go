package scdo

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	client, err := Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client, srv
}

func TestBlockHeight(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","result":9237584,"id":1}`))
	})
	defer srv.Close()

	height, err := client.BlockHeight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if height != 9237584 {
		t.Errorf("expected 9237584, got %d", height)
	}
}

func TestBalance(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// balance as hex string
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x2540be400","id":1}`))
	})
	defer srv.Close()

	bal, err := client.Balance(context.Background(), "0x1234", -1)
	if err != nil {
		t.Fatal(err)
	}
	expected := big.NewInt(10000000000) // 0x2540be400 = 10,000,000,000
	if bal.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected, bal)
	}
}

func TestNonce(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","result":42,"id":1}`))
	})
	defer srv.Close()

	nonce, err := client.Nonce(context.Background(), "0x1234", -1)
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 42 {
		t.Errorf("expected 42, got %d", nonce)
	}
}

func TestGetReceiptNotFound(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// SCDO returns error "leveldb: not found" when tx not mined yet
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"leveldb: not found"},"id":1}`))
	})
	defer srv.Close()

	r, err := client.GetReceipt(context.Background(), "0xdeadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Errorf("expected nil receipt for unmined tx, got %+v", r)
	}
}

func TestGetReceiptSuccess(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"jsonrpc":"2.0",
			"result":{
				"txhash":"0xabc",
				"contract":"1S016aa1b6f0457d0e7363cc06da29ef6ad8f40032",
				"failed":true,
				"usedGas":26152,
				"poststate":"0x60380fd",
				"result":"evm: execution reverted",
				"totalFee":"0x6648"
			},
			"id":1
		}`))
	})
	defer srv.Close()

	r, err := client.GetReceipt(context.Background(), "0xabc")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected receipt, got nil")
	}
	if !r.Failed {
		t.Error("expected failed=true")
	}
	if r.UsedGas != 26152 {
		t.Errorf("expected usedGas 26152, got %d", r.UsedGas)
	}
	if r.ContractAddress == "" {
		t.Error("expected contract address")
	}
}

func TestRPCError(t *testing.T) {
	client, srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"method not found"},"id":1}`))
	})
	defer srv.Close()

	_, err := client.BlockHeight(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
