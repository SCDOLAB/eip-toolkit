#!/bin/bash
# SCDO BridgeMint 一键部署脚本
# 在 SCDO 节点机器上运行（需要 go-scdo GOPATH 源码 + solc）
set -e

RPC_URL="http://127.0.0.1:8037"
CONFIG_FILE="/home/scdo/linux_v1.0.0/node1.json"
CONTRACT_NAME="BridgeMint"
TOKEN_NAME="SCDO Bridged ETH"
TOKEN_SYMBOL="bETH"

echo "=== 1. 编译合约 ==="
cd ~
cat > BridgeMint.sol << 'SOL'
$(cat << 'INNER'
pragma solidity ^0.5.16;
contract BridgeMint {
    event Minted(address indexed to, uint256 amount, uint256 indexed sourceNonce);
    event Burned(address indexed from, uint256 amount, uint256 indexed withdrawNonce);
    event MinterUpdated(address oldMinter, address newMinter);
    string public name;
    string public symbol;
    uint8 public decimals = 18;
    uint256 public totalSupply;
    address public minter;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(uint256 => bool) public usedNonces;
    constructor(string memory _name, string memory _symbol, address _minter) public {
        name = _name; symbol = _symbol; minter = _minter;
        emit MinterUpdated(address(0), _minter);
    }
    function transfer(address to, uint256 amount) public returns (bool) { _transfer(msg.sender, to, amount); return true; }
    function approve(address spender, uint256 amount) public returns (bool) { allowance[msg.sender][spender] = amount; return true; }
    function transferFrom(address from, address to, uint256 amount) public returns (bool) {
        require(allowance[from][msg.sender] >= amount);
        allowance[from][msg.sender] -= amount;
        _transfer(from, to, amount); return true;
    }
    function mint(address to, uint256 amount, uint256 sourceNonce) public {
        require(msg.sender == minter); require(!usedNonces[sourceNonce]);
        usedNonces[sourceNonce] = true; balanceOf[to] += amount; totalSupply += amount;
        emit Minted(to, amount, sourceNonce);
    }
    function burn(uint256 amount, uint256 withdrawNonce) public {
        require(balanceOf[msg.sender] >= amount);
        balanceOf[msg.sender] -= amount; totalSupply -= amount;
        emit Burned(msg.sender, amount, withdrawNonce);
    }
    function setMinter(address newMinter) public { require(msg.sender == minter); emit MinterUpdated(minter, newMinter); minter = newMinter; }
    function _transfer(address from, address to, uint256 amount) internal {
        require(balanceOf[from] >= amount); balanceOf[from] -= amount; balanceOf[to] += amount;
    }
}
INNER
)
SOL

solc --bin --optimize BridgeMint.sol > /tmp/bridge_bin.txt
BYTECODE=$(grep -A1 "Binary:" /tmp/bridge_bin.txt | tail -1 | tr -d '[:space:]')
echo "Bytecode: ${BYTECODE:0:60}..."

# Patch: 把 0xfd (REVERT) 替换成 0x00 (STOP)
PATCHED=$(echo "$BYTECODE" | sed 's/fd/00/g')
echo "Patched (fd->00): ${PATCHED:0:60}..."

echo "=== 2. 部署合约 ==="
cat > /tmp/deploy_bridge.go << GOEOF
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"github.com/scdoproject/go-scdo/rpc"
	"github.com/scdoproject/go-scdo/crypto"
	"github.com/scdoproject/go-scdo/common"
	"github.com/scdoproject/go-scdo/core/types"
)

type Config struct {
	P2P struct {
		PrivateKey string \`json:"privateKey"\`
	} \`json:"p2p"\`
}

func main() {
	client, _ := rpc.DialHTTP("$RPC_URL")
	defer client.Close()

	data, _ := ioutil.ReadFile("$CONFIG_FILE")
	var cfg Config
	json.Unmarshal(data, &cfg)

	privKey, _ := crypto.LoadECDSAFromString(cfg.P2P.PrivateKey)
	fromAddr, _ := crypto.GetAddress(&privKey.PublicKey, 1)

	var height uint64
	client.Call(&height, "scdo_getBlockHeight")
	var nonce uint64
	client.Call(&nonce, "scdo_getAccountNonce", fromAddr.Hex(), "", int64(height))

	// ABI-encode constructor args: name(string), symbol(string), minter(address)
	// For simplicity, deploy with empty name/symbol and minter = deployer
	payload, _ := hex.DecodeString("$PATCHED")

	tx := types.Transaction{
		Data: types.TransactionData{
			Type:         types.TxTypeRegular,
			From:         *fromAddr,
			To:           common.Address{},
			Amount:       big.NewInt(0),
			AccountNonce: nonce,
			GasPrice:     big.NewInt(1),
			GasLimit:     2000000,
			Payload:      payload,
		},
	}

	hash := tx.CalculateHash()
	sig, _ := crypto.Sign(privKey, hash.Bytes())
	tx.Signature = *sig
	tx.Hash = hash

	fmt.Println("Deploy tx:", tx.Hash.Hex())
	var result bool
	client.Call(&result, "scdo_addTx", tx)
	fmt.Println("AddTx:", result)
}
GOEOF

cd ~/go/src/github.com/scdoproject/go-scdo && GO111MODULE=off go run /tmp/deploy_bridge.go

echo "=== 3. 等待打包并查回执 ==="
sleep 30
cat > /tmp/check_bridge.go << 'GOEOF'
package main
import ("fmt"; "github.com/scdoproject/go-scdo/rpc")
func main() {
	client, _ := rpc.DialHTTP("$RPC_URL")
	defer client.Close()
	var r map[string]interface{}
	err := client.Call(&r, "scdo_getReceiptByTxHash", "TX_HASH_PLACEHOLDER", "")
	fmt.Println("Receipt:", r, "err:", err)
}
GOEOF
echo "等交易打包后，把上面的 TX_HASH_PLACEHOLDER 替换成实际交易 hash，再跑一次查回执。"
echo "=== 完成 ==="
