# Stellar Connect SDK

### Composable Anchor Infrastructure & Integration Toolkit for Stellar

**RFC v1.0.0**

February 2026

| Field | Value |
| :---- | :---- |
| Status | **DRAFT — RFC** |
| Classification | Public / Technical |
| First Implementation | **Go** |
| Stable API Contract | Go, TypeScript (future) |
| SEPs Covered (v1) | SEP-1, SEP-6, SEP-10, SEP-24, SEP-45 |
| SEPs Planned (v2) | SEP-12, SEP-31, SEP-38 |

---

## Table of Contents

1. [Goals & Non-Goals](#1-goals--non-goals)
2. [Design Principles](#2-design-principles)
3. [Architecture Overview](#3-architecture-overview)
4. [API Contracts (Interface Layer)](#4-api-contracts-interface-layer)
5. [Core Primitives](#5-core-primitives)
6. [Mode A: Anchor Server](#6-mode-a-anchor-server)
7. [Mode B: Client SDK](#7-mode-b-client-sdk)
8. [Observer: RPC Event Bridge](#8-observer-rpc-event-bridge)
9. [Error Taxonomy](#9-error-taxonomy)
10. [Package Structure](#10-package-structure)
11. [Testing Strategy](#11-testing-strategy)
12. [Security Model](#12-security-model)
13. [SEP Coverage Matrix](#13-sep-coverage-matrix)
14. [Phased Delivery](#14-phased-delivery)
15. [Open Questions](#15-open-questions)

---

## 1. Goals & Non-Goals

### Goals

- **Composable toolkit** — every capability (auth, transfers, discovery, observation) is an independent module. Developers adopt only what they need.
- **API-first design** — all public contracts are Go interfaces, tested independently of any implementation. The same API shapes will be ported to TypeScript and future languages.
- **Minimal dependencies** — outside of the Stellar Go SDK (`stellar/go`), the library depends only on the Go standard library. No web frameworks, no ORMs, no opinionated infrastructure.
- **Dual-mode** — serves both anchor operators (running SEP endpoints) and anchor integrators (wallets, exchanges, automated systems) through the same foundational primitives.
- **User-owned infrastructure** — the SDK never manages databases, HTTP servers, or deployment. It produces and consumes plain Go types. Persistence, transport, and lifecycle are the caller's responsibility, mediated through interfaces the caller implements.
- **Observable** — an `Observer` abstraction wraps Stellar RPC/Horizon streaming to emit typed hooks for on-chain events (payments received, transactions confirmed), enabling reactive anchor workflows without the developer building their own watcher.

### Non-Goals

- Not a framework — does not own your `main()`, your router, or your middleware stack.
- Not a wallet connection library — the `Signer` is an interface, not an implementation.
- Not a database — persistence is behind an interface the caller implements.
- Not a deployment tool — no Docker, no Helm, no managed infrastructure.

---

## 2. Design Principles

Ordered by priority when trade-offs arise.

**P1: API Contracts Are the Product.** Interfaces are defined, documented, and tested before any implementation exists. An implementation is correct if and only if it satisfies the interface's test suite. This enables multiple implementations (in-memory for tests, PostgreSQL for production, custom for specific infrastructure) to share the same correctness guarantees.

**P2: Zero-SEP Awareness.** A developer should never need to know what a SEP number is. Protocols are implementation details.

**P3: Composable, Not Monolithic.** No module forces adoption of another. Auth works without transfers. Transfers work without the observer. The observer works without auth.

**P4: Bring Your Own Everything.** No opinions about databases, HTTP frameworks, message queues, or deployment. The SDK produces and consumes plain Go structs and interfaces.

**P5: Signing Is the Caller's Problem.** The SDK requires exactly one capability: the ability to sign. It does not manage keys, wallet connections, or browser extensions.

**P6: Fail Loudly.** Every failure produces a typed, actionable error with a machine-readable code and human-readable message. Silent failures are bugs.

**P7: Observable by Default.** On-chain events that drive anchor workflows (payments received, transactions confirmed) are surfaced through a first-class `Observer` abstraction, not left as an exercise for the developer.

---

## 3. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                     Developer Code                               │
│                                                                  │
│  ┌────────────────────────┐    ┌───────────────────────────────┐ │
│  │   Mode A: Anchor       │    │   Mode B: Client SDK          │ │
│  │   (any HTTP framework) │    │   (any environment)           │ │
│  └───────────┬────────────┘    └──────────────┬────────────────┘ │
│              │                                │                  │
│  ┌───────────┴────────────┐    ┌──────────────┴────────────────┐ │
│  │   Anchor Engine        │    │   Client Engine                │ │
│  │   - AuthIssuer         │    │   - AuthConsumer               │ │
│  │   - TransferManager    │    │   - TransferConsumer           │ │
│  │   - TomlPublisher      │    │   - SessionManager             │ │
│  └───────────┬────────────┘    └──────────────┬────────────────┘ │
│              │                                │                  │
│              └──────────┬─────────────────────┘                  │
│                         │                                        │
│  ┌──────────────────────┴──────────────────────┐                 │
│  │            Core Primitives                   │                 │
│  │   Signer · TomlResolver · NetworkClient      │                 │
│  │   AccountInspector · Crypto · Errors         │                 │
│  └──────────────────────┬──────────────────────┘                 │
│                         │                                        │
│  ┌──────────────────────┴──────────────────────┐                 │
│  │            Observer (RPC Event Bridge)        │                 │
│  │   PaymentObserver · TransactionObserver      │                 │
│  │   EventBus · Filters                         │                 │
│  └──────────────────────┬──────────────────────┘                 │
│                         │                                        │
│  ┌──────────────────────┴──────────────────────┐                 │
│  │   User-Provided Implementations              │                 │
│  │   Signer · TransferStore · NonceStore        │                 │
│  │   JWTStore · Custom Observer Handlers        │                 │
│  └──────────────────────────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────┘
```

### Dependency Rules

1. Dependencies flow strictly downward.
2. No engine imports from the other mode's engine.
3. Both engines depend on Core Primitives.
4. Core Primitives depend only on `stellar/go` and the Go standard library.
5. The Observer depends only on Core Primitives and `stellar/go`.
6. User-provided implementations satisfy interfaces defined in Core or Engine layers — they never import implementation details.

---

## 4. API Contracts (Interface Layer)

This is the most critical section of the RFC. Every interface here is a **stable contract** — tested independently, versioned carefully, and portable across language implementations.

### 4.1 Signer

The boundary between Stellar Connect and everything upstream. Deliberately minimal.

```go
// Signer is the minimal contract for proving identity and authorizing actions.
// The SDK does not manage keys, wallet connections, or signing infrastructure.
// The caller provides a Signer; the SDK uses it.
type Signer interface {
    // PublicKey returns the Stellar address (G...) identifying this signer.
    PublicKey() string

    // SignTransaction signs a Stellar transaction envelope (base64 XDR).
    // Returns the signed envelope as base64 XDR.
    SignTransaction(ctx context.Context, xdr string) (string, error)
}

// MessageSigner is an optional extension for SEP-45 smart contract wallet auth.
// If a Signer also implements MessageSigner, the SDK will route to SEP-45
// when the anchor supports it.
type MessageSigner interface {
    Signer
    SignMessage(ctx context.Context, message string) (string, error)
}
```

**Convenience constructors** (in a `signers` sub-package):

```go
package signers

// FromSecret creates a Signer from a Stellar secret key (S...).
// Intended for server-side use (exchanges, backends, bots).
func FromSecret(secret string) (stellarconnect.Signer, error)

// FromCallback creates a Signer from a public key and an arbitrary signing function.
// Intended for wrapping HSMs, custodial APIs, or any external signing service.
func FromCallback(
    publicKey string,
    sign func(ctx context.Context, xdr string) (string, error),
) stellarconnect.Signer
```

### 4.2 TransferStore

The persistence boundary. The SDK defines the interface; the developer provides the implementation. The SDK's state machine drives all writes — the developer never writes status directly.

```go
// TransferStore is the persistence interface for transfer records.
// The SDK calls these methods internally during state transitions.
// The developer implements this interface against their own database.
type TransferStore interface {
    // Save persists a new transfer record.
    Save(ctx context.Context, transfer *Transfer) error

    // FindByID retrieves a transfer by its unique identifier.
    FindByID(ctx context.Context, id string) (*Transfer, error)

    // FindByAccount returns all transfers for a given Stellar account,
    // ordered by creation time descending.
    FindByAccount(ctx context.Context, account string) ([]*Transfer, error)

    // Update applies partial updates to an existing transfer.
    // Only non-nil fields in the update are applied.
    Update(ctx context.Context, id string, update *TransferUpdate) error

    // List returns transfers matching the given filters.
    List(ctx context.Context, filters TransferFilters) ([]*Transfer, error)
}

// Transfer is the canonical transfer record.
type Transfer struct {
    ID                  string
    Kind                TransferKind     // "deposit" | "withdrawal"
    Mode                TransferMode     // "interactive" | "api"
    Status              TransferStatus   // Set by SDK state machine, never by developer
    AssetCode           string
    AssetIssuer         string
    Account             string           // Stellar account
    Amount              string           // Decimal string
    InteractiveToken    string           // One-time token for interactive flows
    InteractiveURL      string
    ExternalRef         string           // Banking/payment reference
    StellarTxHash       string           // On-chain transaction hash
    Message             string           // Human-readable status message
    Metadata            map[string]any   // Arbitrary developer data
    CreatedAt           time.Time
    UpdatedAt           time.Time
    CompletedAt         *time.Time
}

// TransferUpdate contains the mutable fields for a transfer update.
// Only non-zero-value fields are applied. Status is always set by the SDK.
type TransferUpdate struct {
    Status           *TransferStatus
    Amount           *string
    ExternalRef      *string
    StellarTxHash    *string
    InteractiveToken *string
    InteractiveURL   *string
    Message          *string
    Metadata         map[string]any
    CompletedAt      *time.Time
}

// TransferFilters for listing transfers.
type TransferFilters struct {
    Account   string
    AssetCode string
    Status    *TransferStatus
    Kind      *TransferKind
    Limit     int
    Offset    int
}
```

### 4.3 NonceStore

Replay protection for SEP-10 challenges.

```go
// NonceStore tracks challenge nonces for replay protection.
// Nonces are added when a challenge is issued and consumed when verified.
type NonceStore interface {
    // Add records a nonce as issued. It should be retrievable until it
    // expires or is consumed.
    Add(ctx context.Context, nonce string, expiresAt time.Time) error

    // Consume marks a nonce as used. Returns false if the nonce was not
    // found or was already consumed.
    Consume(ctx context.Context, nonce string) (bool, error)
}
```

### 4.4 JWTIssuer / JWTVerifier

Decoupled token management — the SDK defines what it needs, not how tokens are signed.

```go
// JWTIssuer creates authentication tokens after successful SEP-10 verification.
type JWTIssuer interface {
    Issue(ctx context.Context, claims JWTClaims) (string, error)
}

// JWTVerifier validates and decodes authentication tokens.
type JWTVerifier interface {
    Verify(ctx context.Context, token string) (*JWTClaims, error)
}

// JWTClaims are the standard claims for a Stellar Connect auth token.
type JWTClaims struct {
    Subject    string    // Stellar address (G...)
    Issuer     string    // Anchor domain
    IssuedAt   time.Time
    ExpiresAt  time.Time
    AuthMethod string    // "sep10" | "sep45"
    Memo       string    // Optional memo from auth challenge
}
```

A default HMAC-SHA256 implementation is provided:

```go
package auth

// NewHMACJWT returns a JWTIssuer and JWTVerifier backed by HMAC-SHA256.
func NewHMACJWT(secret []byte, issuerDomain string, expiry time.Duration) (JWTIssuer, JWTVerifier)
```

### 4.5 Observer (RPC Event Bridge)

The bridge between on-chain events and anchor business logic. Wraps Stellar Horizon streaming or Soroban RPC to emit typed events.

```go
// PaymentEvent represents an incoming or outgoing Stellar payment
// detected by the Observer.
type PaymentEvent struct {
    TxHash      string
    From        string
    To          string
    AssetCode   string
    AssetIssuer string
    Amount      string
    Memo        string
    MemoType    string
    LedgerSeq   uint32
    Timestamp   time.Time
}

// PaymentHandler is a callback invoked when the Observer detects
// a payment matching the registered filters.
type PaymentHandler func(ctx context.Context, event PaymentEvent) error

// Observer watches the Stellar network for events relevant to anchor operations.
// It wraps Horizon streaming or Soroban RPC and emits typed events through
// registered handlers.
type Observer interface {
    // OnPayment registers a handler for incoming payments to the given account.
    // The handler is called for every payment matching the filters.
    // Multiple handlers can be registered; they execute sequentially.
    OnPayment(account string, handler PaymentHandler, opts ...PaymentFilter) error

    // Start begins watching the network. Blocks until ctx is cancelled.
    Start(ctx context.Context) error

    // Stop gracefully shuts down the observer.
    Stop(ctx context.Context) error
}

// PaymentFilter narrows which payments trigger a handler.
type PaymentFilter func(*PaymentFilterConfig)

type PaymentFilterConfig struct {
    AssetCode   string
    AssetIssuer string
    MinAmount   string
    MemoPrefix  string
}

// Filter constructors.
func WithAsset(code, issuer string) PaymentFilter
func WithMinAmount(amount string) PaymentFilter
func WithMemoPrefix(prefix string) PaymentFilter
```

### 4.6 Transfer State Machine

The state machine is the SDK's core invariant. State transitions are enforced, never bypassed.

```go
type TransferStatus string

const (
    StatusInitiating              TransferStatus = "initiating"
    StatusInteractive             TransferStatus = "interactive"
    StatusPendingUserTransferStart TransferStatus = "pending_user_transfer_start"
    StatusPendingExternal         TransferStatus = "pending_external"
    StatusPendingStellar          TransferStatus = "pending_stellar"
    StatusPaymentRequired         TransferStatus = "payment_required"
    StatusCompleted               TransferStatus = "completed"
    StatusFailed                  TransferStatus = "failed"
    StatusDenied                  TransferStatus = "denied"
    StatusCancelled               TransferStatus = "cancelled"
    StatusExpired                 TransferStatus = "expired"
)

type TransferKind string

const (
    KindDeposit    TransferKind = "deposit"
    KindWithdrawal TransferKind = "withdrawal"
)

type TransferMode string

const (
    ModeInteractive TransferMode = "interactive"
    ModeAPI         TransferMode = "api"
)
```

Legal transitions:

| From State | To States |
| :--- | :--- |
| `initiating` | `interactive`, `pending_user_transfer_start`, `pending_external`, `failed`, `denied` |
| `interactive` | `pending_user_transfer_start`, `pending_external`, `failed`, `expired` |
| `pending_user_transfer_start` | `pending_external`, `pending_stellar`, `failed`, `cancelled` |
| `pending_external` | `pending_stellar`, `failed`, `cancelled` |
| `pending_stellar` | `completed`, `failed` |
| `payment_required` | `pending_stellar`, `failed` |
| `completed` | — (terminal) |
| `failed` | — (terminal) |
| `denied` | — (terminal) |
| `cancelled` | — (terminal) |
| `expired` | — (terminal) |

The state machine is tested exhaustively: every legal transition succeeds, every illegal transition returns `ErrTransitionInvalid`.

---

## 5. Core Primitives

Shared foundation used by both Anchor and Client engines.

### 5.1 TOML Discovery

```go
package toml

// AnchorInfo is the parsed representation of a stellar.toml file.
type AnchorInfo struct {
    SigningKey             string
    NetworkPassphrase      string
    WebAuthEndpoint        string
    TransferServer         string   // SEP-6
    TransferServerSep24    string   // SEP-24
    KYCServer              string   // SEP-12
    DirectPaymentServer    string   // SEP-31
    AnchorQuoteServer      string   // SEP-38
    Currencies             []CurrencyInfo
    Documentation          DocumentationInfo
}

type CurrencyInfo struct {
    Code           string
    Issuer         string
    Status         string
    DisplayDecimals int
    AnchorAssetType string
    Description    string
}

// Resolver fetches and parses stellar.toml files.
type Resolver struct {
    httpClient *http.Client
    cache      map[string]*cacheEntry
    cacheTTL   time.Duration
}

// Resolve fetches and parses the stellar.toml for the given domain.
// Results are cached for the configured TTL (default 5 minutes).
func (r *Resolver) Resolve(ctx context.Context, domain string) (*AnchorInfo, error)
```

### 5.2 TOML Publisher (Anchor-Side)

The stellar.toml is a **derived artifact** — generated from the anchor's configuration, never hand-written.

```go
package toml

// Publisher generates stellar.toml content from anchor configuration.
type Publisher struct {
    config AnchorConfig
}

// Render returns the stellar.toml content as a string.
func (p *Publisher) Render() string

// Handler returns an http.HandlerFunc that serves the stellar.toml.
// Compatible with any HTTP framework that accepts http.HandlerFunc.
func (p *Publisher) Handler() http.HandlerFunc
```

### 5.3 Network Client

```go
package net

// Client is a thin HTTP client with timeout, retry, and circuit-breaker policies.
type Client struct {
    httpClient     *http.Client
    maxRetries     int
    retryBackoff   time.Duration
    circuitBreaker *circuitBreaker
}

func NewClient(opts ...ClientOption) *Client

type ClientOption func(*Client)
func WithTimeout(d time.Duration) ClientOption          // default: 10s
func WithMaxRetries(n int) ClientOption                  // default: 2
func WithRetryBackoff(d time.Duration) ClientOption      // default: 1s
```

### 5.4 Account Inspector

```go
package account

type AccountType int

const (
    ClassicFunded AccountType = iota
    ClassicUnfunded
    SorobanContract
    Unknown
)

type AuthPath int

const (
    AuthSEP10 AuthPath = iota
    AuthSEP45
    AuthSEP10OrSEP45
    AuthUnsupported
)

// Inspector determines account type and appropriate auth path.
type Inspector struct {
    horizonURL string
    client     *net.Client
}

func (i *Inspector) Inspect(ctx context.Context, address string) (AccountType, error)
func (i *Inspector) ResolveAuthPath(ctx context.Context, address string, anchorCaps *AnchorInfo) (AuthPath, error)
```

---

## 6. Mode A: Anchor Server

For developers who want to **operate** an anchor. The SDK provides handler functions and lifecycle managers — the developer owns the HTTP server, database, and routing.

### 6.1 Configuration

```go
package anchor

// Config is the single source of truth for all anchor settings.
// The stellar.toml is derived from this — there is no separate TOML config.
type Config struct {
    // Identity
    Domain     string
    SigningKey  string  // Stellar secret key (S...)
    Network    string  // "testnet" | "public"

    // SEP endpoints
    SEP10URL   string  // Web auth endpoint
    SEP6URL    string  // Non-interactive transfer server
    SEP24URL   string  // Interactive transfer server

    // Assets
    Assets []AssetConfig

    // Auth settings
    JWTSecret       []byte
    ChallengeExpiry time.Duration  // default: 5 minutes
    JWTExpiry       time.Duration  // default: 24 hours

    // Transfer settings
    SupportedModes     []TransferMode
    InteractiveBaseURL string  // For SEP-24 interactive flows

    // Documentation (for stellar.toml)
    OrgName        string
    OrgURL         string
    OrgDescription string
}

type AssetConfig struct {
    Code   string
    Issuer string
    Status string  // "live", "test", "dead"
}
```

### 6.2 Anchor Server

```go
package anchor

// Server is the top-level orchestrator for anchor operations.
// It validates configuration, instantiates internal modules, and
// exposes handler factories for HTTP integration.
type Server struct {
    config         Config
    toml           *toml.Publisher
    auth           *AuthIssuer
    transfers      *TransferManager
    observer       Observer  // optional
}

// NewServer creates a validated anchor server from config.
// Returns an error if the configuration is internally inconsistent.
func NewServer(cfg Config, opts ...ServerOption) (*Server, error)

type ServerOption func(*Server)
func WithTransferStore(store TransferStore) ServerOption
func WithNonceStore(store NonceStore) ServerOption
func WithJWTIssuer(issuer JWTIssuer) ServerOption
func WithJWTVerifier(verifier JWTVerifier) ServerOption
func WithObserver(obs Observer) ServerOption

// Toml returns the TOML publisher for serving stellar.toml.
func (s *Server) Toml() *toml.Publisher

// Auth returns the auth issuer for SEP-10/45 challenge-response.
func (s *Server) Auth() *AuthIssuer

// Transfers returns the transfer manager for SEP-6/24 flows.
func (s *Server) Transfers() *TransferManager
```

### 6.3 Auth Issuer (SEP-10 / SEP-45)

```go
package anchor

// AuthIssuer handles the anchor side of authentication:
// generating challenges, verifying signed responses, issuing JWTs.
type AuthIssuer struct {
    signingKey       string
    publicKey        string
    domain           string
    networkPassphrase string
    jwtIssuer        JWTIssuer
    jwtVerifier      JWTVerifier
    nonceStore       NonceStore
    inspector        *account.Inspector
    challengeExpiry  time.Duration
}

// ChallengeResponse is returned when a challenge is issued.
type ChallengeResponse struct {
    Transaction       string `json:"transaction"`
    NetworkPassphrase string `json:"network_passphrase"`
}

// AuthResult is returned after successful challenge verification.
type AuthResult struct {
    Token      string `json:"token"`
    Account    string `json:"account"`
    AuthMethod string `json:"auth_method"`
}

// CreateChallenge builds a SEP-10 challenge transaction for the given account.
func (a *AuthIssuer) CreateChallenge(ctx context.Context, account string) (*ChallengeResponse, error)

// VerifyChallenge validates a signed challenge and returns a JWT.
func (a *AuthIssuer) VerifyChallenge(ctx context.Context, signedXDR string) (*AuthResult, error)

// RequireAuth returns middleware-compatible functions for JWT verification.
// The returned function extracts the Bearer token, verifies it, and returns
// the decoded claims. It is framework-agnostic — the caller integrates it
// into their middleware stack.
func (a *AuthIssuer) RequireAuth(ctx context.Context, authHeader string) (*JWTClaims, error)
```

**Implementation Note**: SEP-10 challenges MUST include a `web_auth_domain` ManageData operation (key="web_auth_domain", value=domain). This prevents cross-domain challenge replay attacks and is required by stellar/anchor-tests validation.

### 6.4 Transfer Manager (SEP-6 + SEP-24)

The transfer manager owns the state machine. The developer notifies it of real-world events; the SDK determines what state transition each event triggers.

```go
package anchor

// TransferManager orchestrates deposit and withdrawal lifecycles.
type TransferManager struct {
    store          TransferStore
    config         Config
    stateMachine   *transferFSM
    hooks          *hookRegistry
}

// --- Initiation ---

// InitiateDeposit starts a new deposit flow.
// For mode "interactive": returns an interactive URL.
// For mode "api": returns deposit instructions via the Initiated hook.
func (tm *TransferManager) InitiateDeposit(ctx context.Context, req DepositRequest) (*DepositResult, error)

// InitiateWithdrawal starts a new withdrawal flow.
func (tm *TransferManager) InitiateWithdrawal(ctx context.Context, req WithdrawalRequest) (*WithdrawalResult, error)

type DepositRequest struct {
    Account   string
    AssetCode string
    Amount    string
    Mode      TransferMode
    Metadata  map[string]any
}

type DepositResult struct {
    ID             string
    InteractiveURL string // Only for interactive mode
    Instructions   string // Only for API mode (e.g., "Wire to Acct #1234")
    ETA            int    // Estimated seconds
}

type WithdrawalRequest struct {
    Account   string
    AssetCode string
    Amount    string
    Mode      TransferMode
    Dest      string
    DestExtra string
    Metadata  map[string]any
}

type WithdrawalResult struct {
    ID             string
    InteractiveURL string // Only for interactive mode
    StellarAccount string // Anchor's Stellar address for receiving payment
    StellarMemo    string // Memo to include in the Stellar payment
    StellarMemoType string
    ETA            int
}

// --- Interactive Flow ---

// CompleteInteractive signals that the user finished the interactive flow.
// Transitions: interactive -> pending_user_transfer_start or pending_external.
func (tm *TransferManager) CompleteInteractive(ctx context.Context, transferID string, data map[string]any) error

// VerifyInteractiveToken validates a one-time token from an interactive URL.
// Returns the transfer context if valid.
func (tm *TransferManager) VerifyInteractiveToken(ctx context.Context, token string) (*Transfer, error)

// --- Developer -> SDK Notifications ---
// The developer calls these when real-world events occur.
// The SDK validates state and drives the appropriate transition.

// NotifyFundsReceived signals that off-chain funds arrived (deposit flow).
// Transitions: pending_external -> pending_stellar.
func (tm *TransferManager) NotifyFundsReceived(ctx context.Context, transferID string, details FundsReceivedDetails) error

type FundsReceivedDetails struct {
    ExternalRef string
    Amount      string
}

// NotifyPaymentSent signals that the on-chain Stellar payment was submitted (deposit flow).
// Transitions: pending_stellar -> completed.
func (tm *TransferManager) NotifyPaymentSent(ctx context.Context, transferID string, details PaymentSentDetails) error

type PaymentSentDetails struct {
    StellarTxHash string
}

// NotifyPaymentReceived signals that an on-chain payment was detected (withdrawal flow).
// Typically called by the Observer, but can be called manually.
// Transitions: payment_required -> pending_stellar.
func (tm *TransferManager) NotifyPaymentReceived(ctx context.Context, transferID string, details PaymentReceivedDetails) error

type PaymentReceivedDetails struct {
    StellarTxHash string
    Amount        string
    AssetCode     string
}

// NotifyDisbursementSent signals that off-chain funds were sent to the user (withdrawal flow).
// Transitions: pending_stellar -> completed.
func (tm *TransferManager) NotifyDisbursementSent(ctx context.Context, transferID string, details DisbursementDetails) error

type DisbursementDetails struct {
    ExternalRef string
}

// Deny denies a transfer (compliance, fraud, etc.).
// Transitions: any non-terminal -> denied.
func (tm *TransferManager) Deny(ctx context.Context, transferID string, reason string) error

// Cancel cancels a transfer.
// Transitions: any non-terminal -> cancelled.
func (tm *TransferManager) Cancel(ctx context.Context, transferID string, reason string) error

// --- Query ---

// GetStatus returns the current transfer state in SEP-compliant format.
func (tm *TransferManager) GetStatus(ctx context.Context, transferID string) (*TransferStatusResponse, error)
```

**Implementation Notes for SEP-24**:
- The `more_info_url` field in transfer status responses is required by the Demo Wallet. Format: `{base_url}/transaction/{id}`. This URL allows users to view detailed transaction information.
- Interactive endpoints MUST accept both `application/json` and `application/x-www-form-urlencoded` content types. The Demo Wallet submits deposit/withdraw requests using FormData (URL-encoded), not JSON.

### 6.5 Lifecycle Hooks (SDK -> Developer)

Hooks are the mechanism by which the SDK notifies the developer of state transitions. The developer subscribes to hooks to execute business logic.

```go
package anchor

// HookEvent identifies a lifecycle event.
type HookEvent string

const (
    HookDepositInitiated               HookEvent = "deposit:initiated"
    HookDepositKYCComplete             HookEvent = "deposit:kyc_complete"
    HookDepositFundsReceived           HookEvent = "deposit:funds_received"
    HookWithdrawalInitiated            HookEvent = "withdrawal:initiated"
    HookWithdrawalStellarPaymentReceived HookEvent = "withdrawal:stellar_payment_received"
)

// HookHandler is a callback invoked by the SDK when a lifecycle event occurs.
type HookHandler func(ctx context.Context, transfer *Transfer, data map[string]any) error

// On registers a handler for a lifecycle event.
// Multiple handlers per event execute sequentially in registration order.
func (tm *TransferManager) On(event HookEvent, handler HookHandler)
```

### 6.6 Full Anchor Example

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"
    "os"

    "github.com/stellarconnect/sdk/anchor"
    "github.com/stellarconnect/sdk/observer"
)

func main() {
    srv, err := anchor.NewServer(anchor.Config{
        Domain:     "myanchor.com",
        SigningKey:  os.Getenv("STELLAR_SECRET"),
        Network:    "testnet",
        SEP10URL:   "https://myanchor.com/auth",
        SEP6URL:    "https://myanchor.com/sep6",
        SEP24URL:   "https://myanchor.com/sep24",
        Assets: []anchor.AssetConfig{
            {Code: "USDC", Issuer: "GA5ZS...", Status: "live"},
        },
        JWTSecret:          []byte(os.Getenv("JWT_SECRET")),
        ChallengeExpiry:    5 * time.Minute,
        JWTExpiry:          24 * time.Hour,
        SupportedModes:     []anchor.TransferMode{anchor.ModeInteractive, anchor.ModeAPI},
        InteractiveBaseURL: "https://myanchor.com/kyc",
        OrgName:            "My Anchor Inc.",
    })
    if err != nil {
        log.Fatal(err)
    }

    // SEP-1: stellar.toml — derived from config
    http.HandleFunc("/.well-known/stellar.toml", srv.Toml().Handler())

    // SEP-10: Auth
    http.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
        challenge, err := srv.Auth().CreateChallenge(r.Context(), r.URL.Query().Get("account"))
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        json.NewEncoder(w).Encode(challenge)
    })

    http.HandleFunc("POST /auth", func(w http.ResponseWriter, r *http.Request) {
        var body struct{ Transaction string `json:"transaction"` }
        json.NewDecoder(r.Body).Decode(&body)

        result, err := srv.Auth().VerifyChallenge(r.Context(), body.Transaction)
        if err != nil {
            http.Error(w, err.Error(), http.StatusUnauthorized)
            return
        }
        json.NewEncoder(w).Encode(result)
    })

    // SEP-24: Interactive deposit
    http.HandleFunc("POST /sep24/transactions/deposit/interactive", func(w http.ResponseWriter, r *http.Request) {
        claims, err := srv.Auth().RequireAuth(r.Context(), r.Header.Get("Authorization"))
        if err != nil {
            http.Error(w, err.Error(), http.StatusUnauthorized)
            return
        }

        result, err := srv.Transfers().InitiateDeposit(r.Context(), anchor.DepositRequest{
            Account:   claims.Subject,
            AssetCode: r.FormValue("asset_code"),
            Amount:    r.FormValue("amount"),
            Mode:      anchor.ModeInteractive,
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        json.NewEncoder(w).Encode(map[string]string{
            "type": "interactive_customer_info_needed",
            "url":  result.InteractiveURL,
            "id":   result.ID,
        })
    })

    // Lifecycle hooks — your business logic
    srv.Transfers().On(anchor.HookDepositFundsReceived, func(ctx context.Context, tx *anchor.Transfer, data map[string]any) error {
        stellarTxHash, err := submitStellarPayment(tx.Account, tx.AssetCode, tx.Amount)
        if err != nil {
            return err
        }
        return srv.Transfers().NotifyPaymentSent(ctx, tx.ID, anchor.PaymentSentDetails{
            StellarTxHash: stellarTxHash,
        })
    })

    // Observer: watch for incoming payments (withdrawal flows)
    obs := observer.NewHorizonObserver("https://horizon-testnet.stellar.org")
    obs.OnPayment(os.Getenv("DISTRIBUTION_ACCOUNT"), func(ctx context.Context, event observer.PaymentEvent) error {
        // Match payment to pending withdrawal by memo
        transfer, err := matchPaymentToWithdrawal(event)
        if err != nil {
            return err
        }
        return srv.Transfers().NotifyPaymentReceived(ctx, transfer.ID, anchor.PaymentReceivedDetails{
            StellarTxHash: event.TxHash,
            Amount:        event.Amount,
            AssetCode:     event.AssetCode,
        })
    })

    go obs.Start(context.Background())

    log.Println("Anchor server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

---

## 7. Mode B: Client SDK

For any system that connects to anchors — wallets, exchanges, automated backends.

### 7.1 Client

```go
package sdk

// Client is the entry point for integrating with Stellar anchors.
type Client struct {
    network      string
    tomlResolver *toml.Resolver
    netClient    *net.Client
    inspector    *account.Inspector
}

// NewClient creates a new Stellar Connect client.
func NewClient(network string, opts ...ClientOption) *Client

type ClientOption func(*Client)
func WithHorizonURL(url string) ClientOption
func WithHTTPClient(client *http.Client) ClientOption
```

### 7.2 Login & Session

```go
package sdk

// LoginParams configures an authentication attempt.
type LoginParams struct {
    Anchor string // Anchor domain (e.g., "myanchor.com")
    Signer Signer
}

// Login authenticates with an anchor through the full SEP-10/45 flow:
// TOML discovery -> account inspection -> challenge request ->
// signing delegation -> JWT receipt.
func (c *Client) Login(ctx context.Context, params LoginParams) (*Session, error)

// Session represents an authenticated connection to a single anchor.
// JWTs are scoped per-anchor; no token from one anchor is ever sent to another.
type Session struct {
    Address      string
    Anchor       string
    AuthMethod   string    // "sep10" | "sep45"
    Capabilities AnchorCapabilities
    ExpiresAt    time.Time
    jwt          string
    client       *Client
}

// AnchorCapabilities describes what the anchor supports, as discovered from TOML.
type AnchorCapabilities struct {
    SEP6   bool
    SEP24  bool
    SEP31  bool
    SEP38  bool
    SEP45  bool
    Assets []toml.CurrencyInfo
}

// IsValid returns true if the session has not expired.
func (s *Session) IsValid() bool

// Refresh re-authenticates and returns a new session.
func (s *Session) Refresh(ctx context.Context) (*Session, error)
```

### 7.3 Transfer Process

```go
package sdk

// TransferProcess represents an in-progress deposit or withdrawal.
// It manages the client-side state machine, status polling, and event emission.
type TransferProcess struct {
    ID                  string
    Status              TransferStatus
    InteractiveURL      string
    Instructions        string // SEP-6: deposit instructions
    StellarTxHash       string
    ExternalTxID        string
    MoreInfoURL         string
    StatusMessage       string
}

// Deposit initiates a deposit with the anchor.
// The SDK determines the appropriate flow (SEP-6 or SEP-24) based on
// anchor capabilities and the caller's options.
func (s *Session) Deposit(ctx context.Context, asset string, opts ...TransferOption) (*TransferProcess, error)

// Withdraw initiates a withdrawal from the anchor.
func (s *Session) Withdraw(ctx context.Context, asset string, opts ...TransferOption) (*TransferProcess, error)

type TransferOption func(*transferConfig)
func WithAmount(amount string) TransferOption
func WithAccount(account string) TransferOption
func WithMode(mode TransferMode) TransferOption
func WithMemo(memo, memoType string) TransferOption

// OnStatusChange registers a callback for status transitions.
func (tp *TransferProcess) OnStatusChange(handler func(newStatus, oldStatus TransferStatus))

// OnInteractive registers a callback for when the anchor returns an interactive URL.
// This is the wallet's hook for opening a popup or redirect.
func (tp *TransferProcess) OnInteractive(handler func(url string))

// OnInstructions registers a callback for when the anchor returns deposit instructions.
// This is the exchange's hook for initiating a bank transfer.
func (tp *TransferProcess) OnInstructions(handler func(instructions string))

// WaitForCompletion blocks until the transfer reaches a terminal state.
// Returns the final transfer state or an error.
func (tp *TransferProcess) WaitForCompletion(ctx context.Context) (*TransferProcess, error)

// StartPolling begins polling the anchor for status updates.
// Uses adaptive backoff: 2s for first 30s, then 10s up to 5m, then 30s.
func (tp *TransferProcess) StartPolling(ctx context.Context) error

// StopPolling stops the status polling loop.
func (tp *TransferProcess) StopPolling()
```

### 7.4 Full Client Example: Exchange (Non-Interactive)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/stellarconnect/sdk"
    "github.com/stellarconnect/sdk/signers"
)

func main() {
    client := sdk.NewClient("testnet")
    signer, _ := signers.FromSecret(os.Getenv("STELLAR_SECRET"))

    session, err := client.Login(context.Background(), sdk.LoginParams{
        Anchor: "myanchor.com",
        Signer: signer,
    })
    if err != nil {
        log.Fatal(err)
    }

    deposit, err := session.Deposit(context.Background(), "USDC",
        sdk.WithAmount("50000"),
        sdk.WithAccount(signer.PublicKey()),
        sdk.WithMode(sdk.ModeAPI),
    )
    if err != nil {
        log.Fatal(err)
    }

    deposit.OnInstructions(func(instructions string) {
        fmt.Println("Deposit instructions:", instructions)
    })

    result, err := deposit.WaitForCompletion(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Deposit completed:", result.StellarTxHash)
}
```

### 7.5 Full Client Example: Wallet (Interactive)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/stellarconnect/sdk"
    "github.com/stellarconnect/sdk/signers"
)

func main() {
    client := sdk.NewClient("testnet")

    // The wallet provides its own signer — however it signs transactions.
    signer := signers.FromCallback(
        userPublicKey,
        func(ctx context.Context, xdr string) (string, error) {
            return walletExtension.SignTransaction(xdr)
        },
    )

    session, err := client.Login(context.Background(), sdk.LoginParams{
        Anchor: "myanchor.com",
        Signer: signer,
    })
    if err != nil {
        log.Fatal(err)
    }

    deposit, err := session.Deposit(context.Background(), "USDC",
        sdk.WithAmount("100"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Interactive: anchor returns a URL for the user to complete KYC
    deposit.OnInteractive(func(url string) {
        fmt.Println("Open in browser:", url)
        // In a real wallet: openPopup(url) or redirect
    })

    result, err := deposit.WaitForCompletion(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Deposit completed:", result.StellarTxHash)
}
```

---

## 8. Observer: RPC Event Bridge

The Observer is the reactive backbone for anchor operations. It wraps Horizon streaming (and, in the future, Soroban RPC) and surfaces on-chain events through typed handlers.

### 8.1 Why a First-Class Observer

Anchors must react to on-chain events: incoming payments for withdrawals, outgoing payment confirmations for deposits, trustline changes. Without a first-class abstraction, every anchor builds its own Horizon watcher with its own cursor management, reconnection logic, and event matching. This is error-prone and repetitive.

The Observer provides:
- **Cursor management** — automatically tracks the last-seen ledger sequence, resumable after restart.
- **Reconnection** — exponential backoff on stream disconnects.
- **Typed events** — `PaymentEvent` with parsed fields, not raw JSON.
- **Filtering** — register handlers for specific accounts, assets, or memo patterns.
- **Integration with TransferManager** — the Observer can be wired to automatically call `NotifyPaymentReceived` when a matching payment arrives.

### 8.2 Horizon Observer Implementation

```go
package observer

// HorizonObserver implements Observer by streaming Horizon payment operations.
type HorizonObserver struct {
    horizonURL string
    client     *net.Client
    handlers   map[string][]handlerEntry
    cursor     string          // Last-seen cursor for resumability
    cursorStore CursorStore    // Optional: persist cursor across restarts
}

// CursorStore persists the streaming cursor for resumability across restarts.
type CursorStore interface {
    Save(ctx context.Context, account string, cursor string) error
    Load(ctx context.Context, account string) (string, error)
}

func NewHorizonObserver(horizonURL string, opts ...ObserverOption) *HorizonObserver

type ObserverOption func(*HorizonObserver)
func WithCursorStore(store CursorStore) ObserverOption
func WithStartCursor(cursor string) ObserverOption
func WithReconnectBackoff(initial, max time.Duration) ObserverOption
```

### 8.3 Wiring Observer to TransferManager

```go
// AutoMatchPayments wires an Observer to a TransferManager.
// When a payment arrives for the anchor's distribution account,
// the observer matches it to a pending withdrawal by memo and
// automatically calls NotifyPaymentReceived.
func AutoMatchPayments(obs Observer, tm *TransferManager, distributionAccount string) error
```

This is opt-in. Anchors with their own Horizon infrastructure can call `NotifyPaymentReceived` directly.

---

## 9. Error Taxonomy

Every error extends `StellarConnectError`. Errors carry a machine-readable code, a human-readable message, a layer identifier, and optional context.

```go
package errors

// Code is a machine-readable error identifier.
type Code string

// StellarConnectError is the base error type for all SDK errors.
type StellarConnectError struct {
    Code    Code
    Message string
    Layer   string  // "core", "anchor", "client", "observer"
    Cause   error
    Context map[string]any
}

func (e *StellarConnectError) Error() string
func (e *StellarConnectError) Unwrap() error
```

### 9.1 Core Errors

| Code | Description | Recoverable? |
| :--- | :--- | :--- |
| `TOML_FETCH_FAILED` | Could not fetch stellar.toml | Retry |
| `TOML_INVALID` | TOML fails schema validation | No (anchor config) |
| `TOML_SIGNING_KEY_MISMATCH` | Signing key changed from pinned value | Manual review |
| `NETWORK_ERROR` | Horizon/Soroban RPC unreachable | Retry |
| `ACCOUNT_NOT_FOUND` | Address not funded on network | Fund account |

### 9.2 Anchor Errors

| Code | Description | Recoverable? |
| :--- | :--- | :--- |
| `CONFIG_INVALID` | Config failed validation | Fix config |
| `CHALLENGE_BUILD_FAILED` | Could not build challenge XDR | Check signing key |
| `CHALLENGE_VERIFY_FAILED` | Signed challenge failed validation | Re-issue challenge |
| `JWT_ISSUE_FAILED` | Could not sign JWT | Check JWT secret |
| `STORE_ERROR` | Storage adapter returned error | Check persistence |
| `INVALID_ASSET` | Requested asset not configured | No (config) |
| `TRANSITION_INVALID` | Illegal state transition attempted | Check transfer state |
| `INTERACTIVE_TOKEN_INVALID` | Token expired, consumed, or malformed | Re-initiate flow |
| `PAYMENT_MISMATCH` | Payment doesn't match expected details | Verify payment |

### 9.3 Client Errors

| Code | Description | Recoverable? |
| :--- | :--- | :--- |
| `SIGNER_ERROR` | Signer rejected or failed | Depends |
| `SIGNER_TIMEOUT` | Signer did not respond | Retry |
| `AUTH_UNSUPPORTED` | Anchor doesn't support auth for account type | No |
| `CHALLENGE_FETCH_FAILED` | SEP-10 GET /auth failed | Retry |
| `CHALLENGE_INVALID` | Challenge XDR fails validation | No (anchor issue) |
| `AUTH_REJECTED` | Anchor rejected signed challenge | Re-authenticate |
| `JWT_EXPIRED` | Session token expired | `session.Refresh()` |
| `TRANSFER_INIT_FAILED` | POST to transfer endpoint failed | Retry |
| `ROUTE_UNAVAILABLE` | Anchor doesn't support requested mode | Try alternate mode |

### 9.4 Observer Errors

| Code | Description | Recoverable? |
| :--- | :--- | :--- |
| `STREAM_DISCONNECTED` | Horizon stream dropped | Auto-reconnect |
| `CURSOR_SAVE_FAILED` | Could not persist cursor | Check cursor store |
| `HANDLER_PANIC` | Payment handler panicked | Handler bug |

---

## 10. Package Structure

```
github.com/stellarconnect/sdk/
├── stellarconnect.go          # Root types: Signer, MessageSigner, TransferStatus, etc.
├── errors/                    # Error types and codes
│   └── errors.go
├── core/                      # Core primitives (shared foundation)
│   ├── toml/                  # TOML discovery + publisher
│   │   ├── resolver.go
│   │   ├── publisher.go
│   │   └── types.go
│   ├── net/                   # HTTP client with retry/circuit-breaker
│   │   └── client.go
│   ├── account/               # Account inspector
│   │   └── inspector.go
│   └── crypto/                # Crypto utilities
│       └── crypto.go
├── anchor/                    # Mode A: Anchor server engine
│   ├── server.go              # AnchorServer orchestrator
│   ├── config.go              # Configuration types + validation
│   ├── auth.go                # AuthIssuer (SEP-10/45)
│   ├── transfer.go            # TransferManager (SEP-6/24)
│   ├── fsm.go                 # Transfer state machine
│   ├── hooks.go               # Lifecycle hook registry
│   └── jwt.go                 # Default HMAC JWT implementation
├── sdk/                       # Mode B: Client SDK engine
│   ├── client.go              # StellarConnect client
│   ├── session.go             # Session (per-anchor auth)
│   ├── transfer.go            # TransferProcess (client-side FSM + polling)
│   └── auth.go                # Auth consumer (SEP-10/45 client)
├── observer/                  # RPC event bridge
│   ├── observer.go            # Observer interface
│   ├── horizon.go             # Horizon streaming implementation
│   ├── match.go               # AutoMatchPayments wiring
│   └── cursor.go              # Cursor persistence interface
├── signers/                   # Convenience signer constructors
│   ├── keypair.go
│   └── callback.go
└── store/                     # Reference store implementations
    ├── memory/                # In-memory (for development/testing)
    │   ├── transfer.go
    │   ├── nonce.go
    │   └── cursor.go
    └── postgres/              # Reference PostgreSQL adapter (optional)
        ├── transfer.go
        ├── nonce.go
        └── cursor.go
```

### Dependency Graph

```
signers  ──> stellarconnect (Signer interface only)
core     ──> stellarconnect + stellar/go + stdlib
anchor   ──> core + stellarconnect
sdk      ──> core + stellarconnect
observer ──> core + stellarconnect + stellar/go
store/*  ──> stellarconnect (interfaces only)
```

No package imports from a peer. `anchor` never imports `sdk`. `sdk` never imports `anchor`. Both import `core`. `store` implementations import only the interfaces from the root package.

**Note on Stellar SDK Dependency**: This project uses `github.com/stellar/go-stellar-sdk` (v0.1.0+). The original `github.com/stellar/go` repository has been archived and deprecated, with functionality migrated to go-stellar-sdk. Both versions remain functional, but all new implementations should use go-stellar-sdk.

---

## 11. Testing Strategy

### 11.1 Interface Conformance Tests

Every interface in Section 4 has a corresponding conformance test suite. These are **exported test helpers** that any implementation can run to verify correctness.

```go
package stellarconnect_test

// TestTransferStoreConformance runs the full conformance suite against
// any TransferStore implementation.
func TestTransferStoreConformance(t *testing.T, factory func() TransferStore) {
    t.Run("Save and FindByID", func(t *testing.T) { /* ... */ })
    t.Run("FindByAccount returns ordered results", func(t *testing.T) { /* ... */ })
    t.Run("Update applies partial fields", func(t *testing.T) { /* ... */ })
    t.Run("List with filters", func(t *testing.T) { /* ... */ })
    t.Run("Update nonexistent returns error", func(t *testing.T) { /* ... */ })
}

func TestNonceStoreConformance(t *testing.T, factory func() NonceStore) {
    t.Run("Add and Consume", func(t *testing.T) { /* ... */ })
    t.Run("Double consume returns false", func(t *testing.T) { /* ... */ })
    t.Run("Expired nonce returns false", func(t *testing.T) { /* ... */ })
}

func TestObserverConformance(t *testing.T, factory func() Observer) {
    t.Run("OnPayment handler receives events", func(t *testing.T) { /* ... */ })
    t.Run("Filters narrow event delivery", func(t *testing.T) { /* ... */ })
    t.Run("Stop is graceful", func(t *testing.T) { /* ... */ })
}
```

The in-memory implementations in `store/memory/` run these conformance tests. Any custom implementation (PostgreSQL, Redis, DynamoDB) runs the same suite:

```go
package postgres_test

func TestPostgresTransferStore(t *testing.T) {
    stellarconnect_test.TestTransferStoreConformance(t, func() stellarconnect.TransferStore {
        return postgres.NewTransferStore(testDB)
    })
}
```

### 11.2 State Machine Tests

The transfer FSM is tested exhaustively:
- Every legal transition succeeds and produces the correct new state.
- Every illegal transition returns `ErrTransitionInvalid`.
- Terminal states reject all transitions.
- Hooks fire on the correct transitions with correct data.

### 11.3 Integration Tests

End-to-end flows using the in-memory stores:
- **Auth round-trip:** Anchor issues challenge -> client signs with keypair signer -> anchor verifies and returns JWT.
- **SEP-24 interactive deposit:** Client initiates -> receives URL -> completes interactive -> anchor notifies funds received -> anchor sends Stellar payment -> completed.
- **SEP-6 non-interactive deposit:** Client initiates -> receives instructions -> anchor notifies funds received -> anchor sends Stellar payment -> completed.
- **Observer integration:** Mock Horizon stream -> observer detects payment -> auto-matches to pending withdrawal -> transition fires.

### 11.4 Anchor Compliance Tests

A test harness that exercises the anchor's HTTP endpoints against the SEP specification:
- SEP-1: TOML is valid, contains required fields, matches config.
- SEP-10: Challenge-response flow with valid and invalid signatures, replay protection, multisig.
- SEP-24: Interactive flow lifecycle, token verification, status polling responses.
- SEP-6: Non-interactive flow lifecycle, deposit instructions, status polling.

---

## 12. Security Model

### 12.1 Threat Model

| Threat | Mitigation | Severity |
| :--- | :--- | :--- |
| TOML Poisoning (DNS/CDN) | HTTPS-only fetching; signing key pinning after first auth (TOFU) | **CRITICAL** |
| Challenge Replay | Nonce uniqueness + time-bound expiry (default 300s) | **HIGH** |
| JWT Forgery | HMAC-SHA256 default; pluggable via JWTIssuer/JWTVerifier interfaces | **HIGH** |
| JWT Theft | SDK never persists JWTs to disk; in-memory only; caller manages storage | **HIGH** |
| Anchor Impersonation | TOML signing key verification; domain-bound JWTs | **HIGH** |
| Cross-Network Replay | Network passphrase bound to every challenge | **MEDIUM** |
| Signer Compromise | Out of scope — library cannot mitigate upstream key management | **Varies** |

### 12.2 Defaults (All Enabled, No Opt-In)

- HTTPS-only TOML fetching
- Nonce freshness validation (both issuance and consumption)
- JWT expiry enforcement with 30-second clock skew tolerance
- Network passphrase binding on all challenges
- Automatic session invalidation on expiry
- TOML signing key pinning after first successful auth (TOFU)

---

## 13. SEP Coverage Matrix

| SEP | Name | Anchor (Mode A) | Client (Mode B) | Version |
| :--- | :--- | :--- | :--- | :--- |
| SEP-1 | stellar.toml | Publish & serve | Discover & parse | **v1** |
| SEP-10 | Web Authentication | Issue & verify challenges | Consume & sign challenges | **v1** |
| SEP-24 | Interactive Deposit/Withdraw | Manage interactive flows | Initiate, poll | **v1** |
| SEP-6 | Non-Interactive Deposit/Withdraw | Manage API-driven transfers | Initiate, poll | **v1** |
| SEP-45 | Soroban Smart Wallet Auth | Verify message-based auth | Route to message signing | **v1** |
| SEP-12 | KYC API | Serve & manage KYC data | Submit KYC data | **v2** |
| SEP-38 | Anchor RFQ (Quotes) | Serve conversion quotes | Request & compare quotes | **v2** |
| SEP-31 | Cross-Border Payments | Receiving anchor flow | Sending anchor flow | **v2** |

---

## 14. Phased Delivery

### Phase 1: Core + Auth + TOML (4 weeks)

- `stellarconnect.go` — all interface definitions
- `core/` — TOML resolver, TOML publisher, network client, account inspector, crypto
- `anchor/auth.go` — AuthIssuer (SEP-10 challenge-response)
- `sdk/auth.go` — Auth consumer (SEP-10 client-side)
- `signers/` — keypair and callback signers
- `store/memory/` — in-memory implementations
- Interface conformance test suites
- **Exit criteria:** Working auth round-trip on testnet. Anchor issues challenge, client SDK signs, anchor returns JWT.

### Phase 2: Transfers + Observer (6 weeks)

- `anchor/transfer.go` — TransferManager (SEP-6 + SEP-24)
- `anchor/fsm.go` — Transfer state machine with exhaustive tests
- `anchor/hooks.go` — Lifecycle hook registry
- `sdk/transfer.go` — TransferProcess with polling
- `sdk/session.go` — Session with deposit/withdraw methods
- `observer/` — Horizon observer with cursor management
- Full integration tests
- **Exit criteria:** Complete deposit and withdrawal flows on testnet, both interactive and non-interactive. Observer detects payments and wires to TransferManager.

### Phase 3: Hardening + Reference (4 weeks)

- `store/postgres/` — Reference PostgreSQL adapter
- Anchor compliance test harness
- Security audit preparation
- Documentation and examples
- **Exit criteria:** Passing anchor compliance tests (SEP-1, SEP-10, SEP-24, SEP-6). Reference anchor and reference client running on testnet.

### Phase 4: Expansion (v2)

- SEP-12, SEP-31, SEP-38 support
- Soroban RPC observer variant
- TypeScript port (same API contracts)
- Community adapters

---

## 15. Open Questions

**Q1: Module path and naming.** ~~Should the Go module be `github.com/stellarconnect/sdk` or `github.com/stellarconnect/go`?~~ **RESOLVED**: The module path is `github.com/stellar-connect/sdk-go`. This follows Go conventions (org/project-lang format) and leaves namespace room for other language SDKs.

**Q2: Observer granularity.** Should the Observer expose lower-level events beyond payments (e.g., trustline changes, account merges, clawbacks)? Starting with payments covers the primary anchor use case; expanding later is additive.

**Q3: Soroban RPC observer.** ~~Should the Observer interface be designed now to accommodate both, or should we start with Horizon-only?~~ **RESOLVED**: The Observer interface is designed generically to support both Horizon and Soroban RPC. v1 implements `HorizonObserver` only; `SorobanObserver` can be added in v2 without breaking changes to the interface.

**Q4: Reference implementation scope.** Should Phase 3 include a complete reference anchor (a minimal but working exchange/on-ramp) to validate the architecture end-to-end?

**Q5: TOML strict vs. lenient parsing.** Real-world stellar.toml files are inconsistent. Should the TOML resolver default to lenient parsing (log warnings, proceed) or strict (reject invalid)? Proposal: lenient by default, strict as an opt-in.

**Q6: Context propagation.** All methods accept `context.Context` for cancellation and deadlines. Should the Observer also propagate context per-event (allowing individual event handling to be cancelled), or is a single top-level context sufficient?
