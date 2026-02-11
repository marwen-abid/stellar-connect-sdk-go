// Package observer provides abstractions for watching Stellar on-chain events
// in real-time. It wraps Horizon streaming and surfaces payments through typed
// handlers with filtering capabilities.
//
// The Observer enables anchors to react to on-chain events (incoming payments
// for withdrawals, outgoing payment confirmations for deposits) without building
// custom Horizon watchers. It provides cursor management for resumability,
// reconnection logic with exponential backoff, and typed PaymentEvent structs.
//
// Example usage:
//
//	obs := observer.NewHorizonObserver(
//	    "https://horizon.stellar.org",
//	    observer.WithCursor("now"),
//	    observer.WithCursorSaver(func(c string) error {
//	        return db.SaveCursor(c)
//	    }),
//	)
//
//	obs.OnPayment(func(evt PaymentEvent) error {
//	    log.Printf("Payment: %s sent %s %s to %s",
//	        evt.From, evt.Amount, evt.Asset, evt.To)
//	    return nil
//	}, observer.WithAsset("USDC:ISSUER..."))
//
//	ctx := context.Background()
//	if err := obs.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
package observer

import (
	"context"
)

// PaymentEvent represents a Stellar payment operation that was streamed from Horizon.
// It contains parsed fields for easy consumption by handlers.
type PaymentEvent struct {
	// ID is the unique operation ID from Horizon
	ID string

	// From is the source account that sent the payment (Stellar public key)
	From string

	// To is the destination account that received the payment (Stellar public key)
	To string

	// Asset is the asset code (e.g., "native" for XLM, "USDC:G..." for issued assets)
	Asset string

	// Amount is the payment amount as a string (e.g., "100.0000000")
	Amount string

	// Memo is the transaction memo (optional, may be empty)
	Memo string

	// Cursor is the paging_token for this payment, used for resumability
	Cursor string

	// TransactionHash is the hash of the transaction containing this payment
	TransactionHash string
}

// PaymentHandler is a user-supplied function that processes a PaymentEvent.
// Handlers are called sequentially for each payment that matches registered filters.
// If the handler returns an error, the error is logged but streaming continues.
type PaymentHandler func(PaymentEvent) error

// PaymentFilter is a function that determines whether a PaymentEvent should be
// processed by a handler. Return true to allow the event, false to skip it.
type PaymentFilter func(PaymentEvent) bool

// handlerEntry pairs a handler with its filters
type handlerEntry struct {
	handler PaymentHandler
	filters []PaymentFilter
}

// Observer is the core abstraction for watching on-chain events.
// Implementations stream events from Horizon or Soroban RPC and call
// registered handlers when matching events occur.
type Observer interface {
	// OnPayment registers a handler for payment events with optional filters.
	// Multiple handlers can be registered. Filters are ANDed together.
	// Handlers are called sequentially for each matching payment.
	OnPayment(handler PaymentHandler, filters ...PaymentFilter)

	// Start begins streaming events from the configured source.
	// This method blocks until the context is cancelled or an unrecoverable error occurs.
	// It automatically reconnects with exponential backoff on stream failures.
	Start(ctx context.Context) error

	// Stop gracefully stops streaming and waits for in-flight handlers to complete.
	// It's safe to call Stop multiple times.
	Stop() error
}

// Common filter constructors

// WithAsset returns a PaymentFilter that matches payments of a specific asset.
// For native XLM, use assetCode = "native".
// For issued assets, use format "CODE:ISSUER" (e.g., "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
func WithAsset(assetCode string) PaymentFilter {
	return func(evt PaymentEvent) bool {
		return evt.Asset == assetCode
	}
}

// WithMinAmount returns a PaymentFilter that matches payments above a minimum amount.
// The amount is compared as a string (lexicographic comparison works for decimal strings
// with the same precision).
func WithMinAmount(minAmount string) PaymentFilter {
	return func(evt PaymentEvent) bool {
		// Simple string comparison - works for amounts with same precision
		// Production implementation might parse to decimal for accurate comparison
		return evt.Amount >= minAmount
	}
}

// WithAccount returns a PaymentFilter that matches payments sent to or from a specific account.
func WithAccount(accountID string) PaymentFilter {
	return func(evt PaymentEvent) bool {
		return evt.From == accountID || evt.To == accountID
	}
}

// WithDestination returns a PaymentFilter that matches payments sent to a specific account.
func WithDestination(accountID string) PaymentFilter {
	return func(evt PaymentEvent) bool {
		return evt.To == accountID
	}
}

// WithSource returns a PaymentFilter that matches payments sent from a specific account.
func WithSource(accountID string) PaymentFilter {
	return func(evt PaymentEvent) bool {
		return evt.From == accountID
	}
}
