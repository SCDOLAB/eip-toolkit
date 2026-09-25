# Contributing

Thanks for contributing to eip-toolkit.

## Development setup

```bash
# Go
go test ./...
go vet ./...

# Solidity
cd solidity
npm install
npx hardhat test
npx solhint 'contracts/**/*.sol'
```

## Pull request checklist

- `go test ./...` and `go vet ./...` pass locally.
- `cd solidity && npx hardhat test && npx solhint 'contracts/**/*.sol'` pass.
- CI must be green on the PR.
- Add tests for new behavior; don't lower existing coverage.
- Update README if you change CLI flags, env vars, or public APIs.

## Commit style

Short imperative subject lines, e.g. `feat: kafka handler`, `fix: shrink batch on range error`.
