package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	stellarconnect "github.com/marwen-abid/anchor-sdk-go"
	"github.com/marwen-abid/anchor-sdk-go/anchor"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

// --- Webhook payload types (top-level key is event type) ---

// OrderUpdatedPayload is the payload for order_updated webhook events.
type OrderUpdatedPayload struct {
	OrderID              string  `json:"orderId"`
	CustomerID           string  `json:"customerId"`
	OrderType            string  `json:"orderType"` // "onramp" or "offramp"
	Status               string  `json:"status"`    // "created", "funded", "completed", "failed", "refunded", "canceled"
	BurnTransaction      string  `json:"burnTransaction,omitempty"`
	ConfirmedTxSignature string  `json:"confirmedTxSignature,omitempty"`
	DepositClabe         string  `json:"depositClabe,omitempty"`
	AmountInFiat         float64 `json:"amountInFiat,omitempty"`
	AmountInTokens       float64 `json:"amountInTokens,omitempty"`
	StatusPage           string  `json:"statusPage,omitempty"`
}

// KYCUpdatedPayload is the payload for kyc_updated webhook events.
type KYCUpdatedPayload struct {
	CustomerID      string `json:"customerId"`
	WalletPublicKey string `json:"walletPublicKey"`
	Approved        bool   `json:"approved"`
	UpdateReason    string `json:"updateReason"`
}

// CustomerUpdatedPayload is the payload for customer_updated webhook events.
type CustomerUpdatedPayload struct {
	CustomerID  string `json:"customerId"`
	DisplayName string `json:"displayName"`
}

// BankAccountUpdatedPayload is the payload for bank_account_updated webhook events.
type BankAccountUpdatedPayload struct {
	BankAccountID string `json:"bankAccountId"`
	CustomerID    string `json:"customerId"`
	Status        string `json:"status"`
	Compliant     bool   `json:"compliant"`
}

// handleWebhook returns a handler for POST /webhooks/etherfuse.
// It verifies the HMAC-SHA256 signature, parses the event, and drives
// transfer state transitions accordingly.
func handleWebhook(
	tm *anchor.TransferManager,
	store stellarconnect.TransferStore,
	webhookSecret string,
	networkPassphrase string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verify HMAC-SHA256 signature
		sig := r.Header.Get("X-Signature")
		if webhookSecret != "" && !verifyWebhookSignature(body, sig, webhookSecret) {
			log.Printf("Webhook signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		// Parse the top-level keys to determine event type.
		// Etherfuse uses the event type as the top-level JSON key:
		// {"order_updated": {...}}, {"kyc_updated": {...}}, etc.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			log.Printf("Webhook: invalid JSON: %v", err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		ctx := context.Background()

		if data, ok := raw["order_updated"]; ok {
			handleOrderUpdated(ctx, tm, store, data, networkPassphrase)
		} else if data, ok := raw["kyc_updated"]; ok {
			handleKYCUpdated(ctx, data)
		} else if data, ok := raw["customer_updated"]; ok {
			handleCustomerUpdated(data)
			_ = data
		} else if data, ok := raw["bank_account_updated"]; ok {
			handleBankAccountUpdated(data)
			_ = data
		} else {
			log.Printf("Webhook: unknown event type in payload")
		}

		// Always return 200 to acknowledge receipt
		w.WriteHeader(http.StatusOK)
	}
}

func handleOrderUpdated(ctx context.Context, tm *anchor.TransferManager, store stellarconnect.TransferStore, data json.RawMessage, networkPassphrase string) {
	var payload OrderUpdatedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("Webhook: failed to parse order_updated: %v", err)
		return
	}

	log.Printf("Webhook: order_updated orderId=%s status=%s type=%s", payload.OrderID, payload.Status, payload.OrderType)

	transfer, err := findTransferByOrderID(ctx, store, payload.OrderID)
	if err != nil || transfer == nil {
		log.Printf("Webhook: no transfer found for orderId=%s: %v", payload.OrderID, err)
		return
	}

	switch payload.Status {
	case "created":
		// For offramp orders, decode the burnTransaction to extract withdraw details
		if payload.OrderType == "offramp" && payload.BurnTransaction != "" {
			account, memo, err := decodeBurnTransaction(payload.BurnTransaction, networkPassphrase)
			if err != nil {
				log.Printf("Webhook: failed to decode burnTransaction: %v", err)
				return
			}
			log.Printf("Webhook: decoded burnTransaction: account=%s memo=%s", account, memo)
			if err := mergeMetadata(ctx, store, transfer.ID, map[string]any{
				"etherfuse_withdraw_anchor_account": account,
				"etherfuse_withdraw_memo":           memo,
				"etherfuse_burn_transaction":        payload.BurnTransaction,
			}); err != nil {
				log.Printf("Webhook: failed to update withdraw details: %v", err)
			}
		}

	case "funded":
		if payload.OrderType == "onramp" {
			// Deposit: fiat received, anchor processing
			err = tm.NotifyFundsReceived(ctx, transfer.ID, anchor.FundsReceivedDetails{
				ExternalRef: payload.OrderID,
				Amount:      fmt.Sprintf("%.7f", payload.AmountInTokens),
			})
		} else {
			// Withdrawal: user's Stellar payment received by Etherfuse
			err = tm.NotifyPaymentReceived(ctx, transfer.ID, anchor.PaymentReceivedDetails{
				StellarTxHash: payload.ConfirmedTxSignature,
				Amount:        fmt.Sprintf("%.7f", payload.AmountInTokens),
			})
		}
		if err != nil {
			log.Printf("Webhook: failed to notify funds received for %s: %v", transfer.ID, err)
		}

	case "completed":
		if payload.OrderType == "onramp" {
			// Deposit: Etherfuse sent crypto to user's Stellar account
			err = tm.NotifyPaymentSent(ctx, transfer.ID, anchor.PaymentSentDetails{
				StellarTxHash: payload.ConfirmedTxSignature,
			})
		} else {
			// Withdrawal: Etherfuse sent MXN to user's bank
			err = tm.NotifyDisbursementSent(ctx, transfer.ID, anchor.DisbursementDetails{
				ExternalRef: payload.OrderID,
			})
		}
		if err != nil {
			log.Printf("Webhook: failed to notify completion for %s: %v", transfer.ID, err)
		}

	case "failed":
		if err := tm.Cancel(ctx, transfer.ID, "Etherfuse order failed"); err != nil {
			log.Printf("Webhook: failed to cancel transfer %s: %v", transfer.ID, err)
		}

	case "refunded":
		if err := tm.Cancel(ctx, transfer.ID, "Etherfuse order refunded"); err != nil {
			log.Printf("Webhook: failed to cancel (refund) transfer %s: %v", transfer.ID, err)
		}

	case "canceled":
		if err := tm.Cancel(ctx, transfer.ID, "Etherfuse order canceled"); err != nil {
			log.Printf("Webhook: failed to cancel transfer %s: %v", transfer.ID, err)
		}

	default:
		log.Printf("Webhook: unknown order status: %s", payload.Status)
	}
}

func handleKYCUpdated(ctx context.Context, data json.RawMessage) {
	var payload KYCUpdatedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("Webhook: failed to parse kyc_updated: %v", err)
		return
	}
	log.Printf("Webhook: kyc_updated customerId=%s approved=%v reason=%s",
		payload.CustomerID, payload.Approved, payload.UpdateReason)
}

func handleCustomerUpdated(data json.RawMessage) {
	var payload CustomerUpdatedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("Webhook: failed to parse customer_updated: %v", err)
		return
	}
	log.Printf("Webhook: customer_updated customerId=%s displayName=%s",
		payload.CustomerID, payload.DisplayName)
}

func handleBankAccountUpdated(data json.RawMessage) {
	var payload BankAccountUpdatedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("Webhook: failed to parse bank_account_updated: %v", err)
		return
	}
	log.Printf("Webhook: bank_account_updated bankAccountId=%s status=%s compliant=%v",
		payload.BankAccountID, payload.Status, payload.Compliant)
}

// verifyWebhookSignature checks the HMAC-SHA256 signature from the X-Signature header.
// The header format is "sha256={hex_digest}".
func verifyWebhookSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}
	hexDigest, ok := strings.CutPrefix(signature, "sha256=")
	if !ok {
		return false
	}
	expected, err := hex.DecodeString(hexDigest)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

// findTransferByOrderID scans all transfers for one whose Metadata contains
// the given Etherfuse order ID. Returns nil if not found.
func findTransferByOrderID(ctx context.Context, store stellarconnect.TransferStore, orderID string) (*stellarconnect.Transfer, error) {
	transfers, err := store.List(ctx, stellarconnect.TransferFilters{})
	if err != nil {
		return nil, err
	}
	for _, t := range transfers {
		if t.Metadata != nil {
			if oid, ok := t.Metadata["etherfuse_order_id"].(string); ok && oid == orderID {
				return t, nil
			}
		}
	}
	return nil, nil
}

// decodeBurnTransaction parses a base64-encoded Stellar transaction XDR
// and extracts the destination account and memo from the payment operation.
// This is used to populate withdraw_anchor_account and withdraw_memo for
// SEP-24 withdrawal compliance (design doc section 6.6, Option A).
func decodeBurnTransaction(xdrBase64 string, networkPassphrase string) (account string, memo string, err error) {
	parsed, err := txnbuild.TransactionFromXDR(xdrBase64)
	if err != nil {
		return "", "", fmt.Errorf("parse XDR: %w", err)
	}

	var tx *txnbuild.Transaction
	if t, ok := parsed.Transaction(); ok {
		tx = t
	} else {
		return "", "", fmt.Errorf("expected Transaction, got FeeBumpTransaction")
	}

	// Extract memo
	if tx.Memo() != nil {
		memoXDR, err := tx.Memo().ToXDR()
		if err == nil {
			switch memoXDR.Type {
			case xdr.MemoTypeMemoText:
				memo = string(memoXDR.MustText())
			case xdr.MemoTypeMemoId:
				memo = fmt.Sprintf("%d", memoXDR.MustId())
			case xdr.MemoTypeMemoHash:
				hash := memoXDR.MustHash()
				memo = hex.EncodeToString(hash[:])
			}
		}
	}

	// Find the first payment operation and extract the destination
	for _, op := range tx.Operations() {
		if paymentOp, ok := op.(*txnbuild.Payment); ok {
			return paymentOp.Destination, memo, nil
		}
	}

	return "", "", fmt.Errorf("no payment operation found in burnTransaction")
}

// mergeMetadata reads the current transfer metadata and merges new keys into it.
// This is necessary because store/memory replaces metadata entirely on update.
func mergeMetadata(ctx context.Context, store stellarconnect.TransferStore, transferID string, newKeys map[string]any) error {
	transfer, err := store.FindByID(ctx, transferID)
	if err != nil {
		return err
	}
	merged := make(map[string]any)
	for k, v := range transfer.Metadata {
		merged[k] = v
	}
	for k, v := range newKeys {
		merged[k] = v
	}
	return store.Update(ctx, transferID, &stellarconnect.TransferUpdate{Metadata: merged})
}
