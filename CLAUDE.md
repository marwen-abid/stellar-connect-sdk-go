# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Anchor SDK Go — a composable anchor infrastructure and integration toolkit for the Stellar network. Implements SEP-1 (discovery), SEP-10 (authentication), SEP-6 (non-interactive transfers), and SEP-24 (interactive transfers). Currently at POC stage (not production-ready).

**Module**: `github.com/marwen-abid/anchor-sdk-go`
**Go version**: 1.25.4

## Build & Development Commands

```bash
# Run the example anchor server (default port 8000)
go run ./examples/anchor
go run ./examples/anchor -port 9000

# Run all tests
go test ./...

# Run a single test
go test ./anchor -run TestFunctionName

# Sync dependencies
go mod tidy
```

No Makefile — uses standard Go tooling. No test files exist yet; testify (`stretchr/testify`) is available as a dependency.

## Architecture

### Design Principles

- **Composable toolkit, not a framework**: Each capability (auth, transfers, discovery, observation) is independent. There is no `anchor.Server` orchestrator — developers wire components together.
- **Interface-based contracts**: All public contracts are Go interfaces defined in `stellarconnect.go`. Developers implement `TransferStore`, `NonceStore`, and `Signer` against their own infrastructure.
- **Delegation model**: The SDK does not manage keys, persistence, or business logic.

### Package Layout

| Package | Role |
|---------|------|
| `stellarconnect` (root) | Core interfaces: `Signer`, `TransferStore`, `NonceStore`, `JWTIssuer`/`JWTVerifier`, `Observer`, `Transfer` types |
| `anchor/` | Server-side: `AuthIssuer` (SEP-10), `TransferManager` (SEP-6/24 lifecycle), `HookRegistry` (events), FSM (state transitions), `HMACJWTIssuer`/`Verifier` |
| `sdk/` | Client-side: `Client` (anchor discovery + login), `Session` (authenticated operations), `TransferProcess` (polling + status callbacks) |
| `observer/` | `HorizonObserver` (payment streaming), `PaymentFilter` constructors (`WithAsset`, `WithMinAmount`, `WithAccount`), `AutoMatchPayments` helper |
| `core/crypto/` | Nonce generation, signature verification |
| `core/toml/` | SEP-1 stellar.toml resolution (`Resolver`) and generation (`Publisher`) |
| `core/net/` | HTTP client wrapper |
| `signers/` | `FromSecret()` keypair signer, `callbackSigner` for custom signing |
| `store/memory/` | In-memory `TransferStore` and `NonceStore` (for examples/tests) |
| `errors/` | Typed `StellarConnectError` with code, layer, cause, and context |
| `examples/anchor/` | Full working reference anchor (SEP-1/6/10/24 endpoints + interactive KYC templates) |

### Key Data Flows

**SEP-10 Auth**: `GET /auth` → `AuthIssuer.CreateChallenge` (generate nonce, build challenge tx) → client signs → `POST /auth` → `AuthIssuer.VerifyChallenge` (verify sigs, consume nonce, issue JWT)

**SEP-24 Transfer**: `TransferManager.InitiateDeposit/Withdrawal` → store transfer → return interactive URL → user completes KYC → `TransferManager.CompleteInteractive` → status transitions via FSM → hooks fire on each transition

**Observer**: `HorizonObserver.Start` streams payments from Horizon → filters applied (AND logic) → matched handlers called sequentially → `AutoMatchPayments` can auto-link payments to pending withdrawals

### Transfer State Machine (`anchor/fsm.go`)

States: `initiating` → `interactive` → `pending_user_transfer_start` → `pending_external` → `pending_stellar` → `completed`/`failed`/`denied`/`cancelled`/`expired`. The FSM enforces valid transitions; invalid transitions return errors.

### Thread Safety

`HookRegistry`, `TransferStore` (memory), `NonceStore` (memory), and `HorizonObserver` all use `sync.RWMutex`.

## SEP Coverage

- **SEP-1**: stellar.toml publish + resolve (with caching)
- **SEP-10**: Full web authentication (challenge/verify/JWT middleware)
- **SEP-6**: Non-interactive transfers (example only)
- **SEP-24**: Interactive deposit/withdrawal with KYC
- **SEP-12, 31, 38, 45**: Not implemented in v1

## Verification with anchor-tests

```bash
# Start the example anchor server in the background
go run ./examples/anchor &
sleep 2

# Run stellar anchor-tests against it (SEP-1, SEP-10, SEP-24)
docker run --rm --network host stellar/anchor-tests:latest \
  --home-domain http://localhost:8000 --seps 1 10 24

# Kill the background server when done
kill %1
```

4 "Account Signer Support" tests are expected to fail (requires Horizon account lookups, out of v1 scope).
