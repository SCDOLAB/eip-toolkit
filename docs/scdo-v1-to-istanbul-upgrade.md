# SCDO V1.0.0 → Istanbul EVM Upgrade Guide

## Problem

SCDO V1.0.0 ships with a Frontier/Homestead-era EVM jump table that does
not recognize the Byzantium `REVERT (0xfd)` opcode. Any contract bytecode
containing `0xfd` deploys but immediately fails with `evm: execution reverted`.

The go-scdo source tree already contains Byzantium/Constantinople/Istanbul
jump tables and the correct `ChainConfig` (`ByzantiumBlock: 0`,
`ConstantinopleBlock = IstanbulBlock = EmeryForkHeight = 2979594`). The only
reason the running node did not activate them is that the on-disk binary was
compiled in Jan 2021, before these config values were set.

## Upgrade steps

Run on the SCDO node machine (user `scdo`):

```bash
cd ~/go/src/github.com/scdoproject/go-scdo

# 1. Stub scdorand (PoW-consensus-only randomness; RPC/EVM never touch it)
cat > consensus/scdorand/scdorand.go << 'EOF'
package scdorand

type Source struct{ seed int64 }

func NewSource(seed int64) *Source         { return &Source{seed: seed} }
func NewSource_EmeryFork(seed int64) *Source { return &Source{seed: seed} }

type RandObj struct{ src *Source }

func NewRandObj(src *Source) *RandObj { return &RandObj{src: src} }

func (r *RandObj) Int63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	r.src.seed = r.src.seed*6364136223846793005 + 1442695040888963407
	v := r.src.seed % n
	if v < 0 {
		v = -v
	}
	return v
}
EOF

# 2. Remove precompiled .a files (Go 1.22 no longer supports binary-only pkgs)
rm -f consensus/scdorand/scdorand_*.a

# 3. Provide dummy CUDA libs (GPU mining not needed on this node)
mkdir -p /tmp/dummy-libs
cat > /tmp/dummy-libs/det.c << 'EOF'
void Determinant(int* a, double* b, int c, int d, int e, int f, int g) {}
EOF
gcc -shared -fPIC -Wl,--export-dynamic -o /tmp/dummy-libs/libgoGpuDet.so /tmp/dummy-libs/det.c
gcc -shared -fPIC -o /tmp/dummy-libs/libcudart.so -x c /dev/null

# 4. Point cgo LDFLAGS at dummy libs
sed -i 's|#cgo LDFLAGS:.*|#cgo LDFLAGS: -L/tmp/dummy-libs -lgoGpuDet -lcudart -lstdc++|' \
    consensus/zpow/engine.go

# 5. Build
GO111MODULE=off go build -o ~/node-new ./cmd/node

# 6. Restart
pkill -f "linux_v1.0.0/node"
sleep 2
cd /home/scdo/linux_v1.0.0
nohup ~/node-new start -c node1.json -m stop > /tmp/node1.log 2>&1 &
nohup ~/node-new start -c node2.json -m stop > /tmp/node2.log 2>&1 &
nohup ~/node-new start -c node3.json -m stop > /tmp/node3.log 2>&1 &
nohup ~/node-new start -c node4.json -m stop > /tmp/node4.log 2>&1 &
```

## Verify

```bash
# Height should increase
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"scdo_getBlockHeight","params":[],"id":1}' \
  http://127.0.0.1:8037
```

Then deploy a contract bytecode that contains `0xfd` (e.g. solc 0.5.x empty
contract). It should return `failed: false` instead of `evm: execution
reverted`.

## Result

After upgrade the EVM supports:
- Byzantium: REVERT (0xfd), RETURNDATASIZE, RETURNDATACOPY, STATICCALL
- Constantinople: CREATE2 (0xf5), EXTCODEHASH (0x3f)
- Istanbul: CHAINID (0x46), SELFBALANCE (0x47), BEGASLIKEX

No bytecode patching is needed anymore; standard solc output deploys directly.
