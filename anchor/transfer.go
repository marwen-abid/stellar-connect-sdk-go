package anchor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	stellarconnect "github.com/stellar-connect/sdk-go"
	corecrypto "github.com/stellar-connect/sdk-go/core/crypto"
	"github.com/stellar-connect/sdk-go/errors"
)

const (
	interactiveTokenLength = 32
)

type Config struct {
	Domain             string
	InteractiveBaseURL string
}

type TransferManager struct {
	store     stellarconnect.TransferStore
	config    Config
	hooks     *HookRegistry
	tokenMu   sync.Mutex
	tokenToID map[string]string
}

func NewTransferManager(store stellarconnect.TransferStore, config Config, hooks *HookRegistry) *TransferManager {
	if hooks == nil {
		hooks = NewHookRegistry()
	}
	return &TransferManager{
		store:     store,
		config:    config,
		hooks:     hooks,
		tokenToID: make(map[string]string),
	}
}

type DepositRequest struct {
	Account   string
	AssetCode string
	Amount    string
	Mode      stellarconnect.TransferMode
	Metadata  map[string]any
}

type DepositResult struct {
	ID             string
	InteractiveURL string
	Instructions   string
	ETA            int
}

type WithdrawalRequest struct {
	Account   string
	AssetCode string
	Amount    string
	Mode      stellarconnect.TransferMode
	Dest      string
	DestExtra string
	Metadata  map[string]any
}

type WithdrawalResult struct {
	ID              string
	InteractiveURL  string
	StellarAccount  string
	StellarMemo     string
	StellarMemoType string
	ETA             int
}

type FundsReceivedDetails struct {
	ExternalRef string
	Amount      string
}

type PaymentSentDetails struct {
	StellarTxHash string
}

type PaymentReceivedDetails struct {
	StellarTxHash string
	Amount        string
	AssetCode     string
}

type DisbursementDetails struct {
	ExternalRef string
}

type TransferStatusResponse struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	StatusETA    int        `json:"status_eta,omitempty"`
	MoreInfoURL  string     `json:"more_info_url"`
	AmountIn     string     `json:"amount_in,omitempty"`
	AmountOut    string     `json:"amount_out,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	TxHash       string     `json:"stellar_transaction_id,omitempty"`
	ExternalTxID string     `json:"external_transaction_id,omitempty"`
	Message      string     `json:"message,omitempty"`
}

func (tm *TransferManager) InitiateDeposit(ctx context.Context, req DepositRequest) (*DepositResult, error) {
	if tm.store == nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "transfer store not configured", nil)
	}
	if strings.TrimSpace(req.Account) == "" || strings.TrimSpace(req.AssetCode) == "" || strings.TrimSpace(req.Amount) == "" {
		return nil, errors.NewAnchorError(errors.TRANSFER_INIT_FAILED, "account, asset_code, and amount are required", nil)
	}

	id, err := corecrypto.GenerateNonce(16)
	if err != nil {
		return nil, errors.NewAnchorError(errors.TRANSFER_INIT_FAILED, "failed to generate transfer ID", err)
	}

	now := time.Now()
	transfer := &stellarconnect.Transfer{
		ID:        id,
		Kind:      stellarconnect.KindDeposit,
		Mode:      req.Mode,
		Status:    stellarconnect.StatusInitiating,
		AssetCode: req.AssetCode,
		Account:   req.Account,
		Amount:    req.Amount,
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if req.Mode == stellarconnect.ModeInteractive {
		token, url, err := tm.generateInteractiveURL(id)
		if err != nil {
			return nil, err
		}
		transfer.InteractiveToken = token
		transfer.InteractiveURL = url
		transfer.Status = stellarconnect.StatusInteractive
	}

	if err := tm.store.Save(ctx, transfer); err != nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "failed to save transfer", err)
	}

	if transfer.Mode == stellarconnect.ModeInteractive {
		tm.hooks.Trigger(HookDepositInitiated, transfer)
		return &DepositResult{ID: transfer.ID, InteractiveURL: transfer.InteractiveURL}, nil
	}

	if err := tm.transition(ctx, transfer.ID, stellarconnect.StatusPendingExternal, ""); err != nil {
		return nil, err
	}
	tm.hooks.Trigger(HookDepositInitiated, transfer)
	return &DepositResult{ID: transfer.ID, Instructions: "deposit initiated", ETA: 0}, nil
}

func (tm *TransferManager) InitiateWithdrawal(ctx context.Context, req WithdrawalRequest) (*WithdrawalResult, error) {
	if tm.store == nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "transfer store not configured", nil)
	}
	if strings.TrimSpace(req.Account) == "" || strings.TrimSpace(req.AssetCode) == "" || strings.TrimSpace(req.Amount) == "" {
		return nil, errors.NewAnchorError(errors.TRANSFER_INIT_FAILED, "account, asset_code, and amount are required", nil)
	}

	id, err := corecrypto.GenerateNonce(16)
	if err != nil {
		return nil, errors.NewAnchorError(errors.TRANSFER_INIT_FAILED, "failed to generate transfer ID", err)
	}

	now := time.Now()
	transfer := &stellarconnect.Transfer{
		ID:        id,
		Kind:      stellarconnect.KindWithdrawal,
		Mode:      req.Mode,
		Status:    stellarconnect.StatusInitiating,
		AssetCode: req.AssetCode,
		Account:   req.Account,
		Amount:    req.Amount,
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if req.Mode == stellarconnect.ModeInteractive {
		token, url, err := tm.generateInteractiveURL(id)
		if err != nil {
			return nil, err
		}
		transfer.InteractiveToken = token
		transfer.InteractiveURL = url
		transfer.Status = stellarconnect.StatusInteractive
	} else {
		transfer.Status = stellarconnect.StatusPaymentRequired
	}

	if err := tm.store.Save(ctx, transfer); err != nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "failed to save transfer", err)
	}

	tm.hooks.Trigger(HookWithdrawalInitiated, transfer)

	result := &WithdrawalResult{
		ID:              transfer.ID,
		InteractiveURL:  transfer.InteractiveURL,
		StellarAccount:  tm.config.Domain,
		StellarMemo:     transfer.ID,
		StellarMemoType: "text",
	}
	return result, nil
}

func (tm *TransferManager) CompleteInteractive(ctx context.Context, transferID string, data map[string]any) error {
	transfer, err := tm.store.FindByID(ctx, transferID)
	if err != nil {
		return errors.NewAnchorError(errors.STORE_ERROR, "failed to load transfer", err)
	}
	if transfer.Mode != stellarconnect.ModeInteractive {
		return errors.NewAnchorError(errors.TRANSITION_INVALID, "transfer not in interactive mode", nil)
	}

	next := stellarconnect.StatusPendingExternal
	if transfer.Kind == stellarconnect.KindDeposit {
		next = stellarconnect.StatusPendingUserTransferStart
	}
	return tm.transition(ctx, transferID, next, "")
}

func (tm *TransferManager) VerifyInteractiveToken(ctx context.Context, token string) (*stellarconnect.Transfer, error) {
	tm.tokenMu.Lock()
	transferID, ok := tm.tokenToID[token]
	if ok {
		delete(tm.tokenToID, token)
	}
	tm.tokenMu.Unlock()
	if !ok {
		return nil, errors.NewAnchorError(errors.INTERACTIVE_TOKEN_INVALID, "interactive token invalid", nil)
	}
	transfer, err := tm.store.FindByID(ctx, transferID)
	if err != nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "failed to load transfer", err)
	}
	return transfer, nil
}

func (tm *TransferManager) NotifyFundsReceived(ctx context.Context, transferID string, details FundsReceivedDetails) error {
	update := &stellarconnect.TransferUpdate{ExternalRef: &details.ExternalRef}
	if strings.TrimSpace(details.Amount) != "" {
		update.Amount = &details.Amount
	}
	return tm.updateAndTransition(ctx, transferID, update, stellarconnect.StatusPendingStellar, HookDepositFundsReceived)
}

func (tm *TransferManager) NotifyPaymentSent(ctx context.Context, transferID string, details PaymentSentDetails) error {
	update := &stellarconnect.TransferUpdate{StellarTxHash: &details.StellarTxHash}
	completedAt := time.Now()
	update.CompletedAt = &completedAt
	return tm.updateAndTransition(ctx, transferID, update, stellarconnect.StatusCompleted, HookTransferStatusChanged)
}

func (tm *TransferManager) NotifyPaymentReceived(ctx context.Context, transferID string, details PaymentReceivedDetails) error {
	update := &stellarconnect.TransferUpdate{StellarTxHash: &details.StellarTxHash}
	return tm.updateAndTransition(ctx, transferID, update, stellarconnect.StatusPendingStellar, HookWithdrawalStellarPaymentSent)
}

func (tm *TransferManager) NotifyDisbursementSent(ctx context.Context, transferID string, details DisbursementDetails) error {
	update := &stellarconnect.TransferUpdate{ExternalRef: &details.ExternalRef}
	completedAt := time.Now()
	update.CompletedAt = &completedAt
	return tm.updateAndTransition(ctx, transferID, update, stellarconnect.StatusCompleted, HookTransferStatusChanged)
}

func (tm *TransferManager) Deny(ctx context.Context, transferID string, reason string) error {
	return tm.transition(ctx, transferID, stellarconnect.StatusDenied, reason)
}

func (tm *TransferManager) Cancel(ctx context.Context, transferID string, reason string) error {
	return tm.transition(ctx, transferID, stellarconnect.StatusCancelled, reason)
}

func (tm *TransferManager) GetStatus(ctx context.Context, transferID string) (*TransferStatusResponse, error) {
	transfer, err := tm.store.FindByID(ctx, transferID)
	if err != nil {
		return nil, errors.NewAnchorError(errors.STORE_ERROR, "failed to load transfer", err)
	}
	moreInfo := fmt.Sprintf("http://localhost:8000/transaction/%s", transfer.ID)
	resp := &TransferStatusResponse{
		ID:           transfer.ID,
		Kind:         string(transfer.Kind),
		Status:       string(transfer.Status),
		MoreInfoURL:  moreInfo,
		AmountIn:     transfer.Amount,
		AmountOut:    transfer.Amount,
		StartedAt:    transfer.CreatedAt,
		CompletedAt:  transfer.CompletedAt,
		TxHash:       transfer.StellarTxHash,
		ExternalTxID: transfer.ExternalRef,
		Message:      transfer.Message,
	}
	return resp, nil
}

func (tm *TransferManager) updateAndTransition(ctx context.Context, transferID string, update *stellarconnect.TransferUpdate, next stellarconnect.TransferStatus, hook HookEvent) error {
	transfer, err := tm.store.FindByID(ctx, transferID)
	if err != nil {
		return errors.NewAnchorError(errors.STORE_ERROR, "failed to load transfer", err)
	}
	if err := ValidateTransition(transfer.Status, next); err != nil {
		return err
	}
	update.Status = &next
	if err := tm.store.Update(ctx, transferID, update); err != nil {
		return errors.NewAnchorError(errors.STORE_ERROR, "failed to update transfer", err)
	}
	updated, _ := tm.store.FindByID(ctx, transferID)
	tm.hooks.Trigger(hook, updated)
	tm.hooks.Trigger(HookTransferStatusChanged, updated)
	return nil
}

func (tm *TransferManager) transition(ctx context.Context, transferID string, next stellarconnect.TransferStatus, message string) error {
	transfer, err := tm.store.FindByID(ctx, transferID)
	if err != nil {
		return errors.NewAnchorError(errors.STORE_ERROR, "failed to load transfer", err)
	}
	if err := ValidateTransition(transfer.Status, next); err != nil {
		return err
	}
	update := &stellarconnect.TransferUpdate{Status: &next}
	if strings.TrimSpace(message) != "" {
		update.Message = &message
	}
	if next == stellarconnect.StatusCompleted {
		completedAt := time.Now()
		update.CompletedAt = &completedAt
	}
	if err := tm.store.Update(ctx, transferID, update); err != nil {
		return errors.NewAnchorError(errors.STORE_ERROR, "failed to update transfer", err)
	}
	updated, _ := tm.store.FindByID(ctx, transferID)
	tm.hooks.Trigger(HookTransferStatusChanged, updated)
	return nil
}

func (tm *TransferManager) generateInteractiveURL(transferID string) (string, string, error) {
	token, err := corecrypto.GenerateNonce(interactiveTokenLength)
	if err != nil {
		return "", "", errors.NewAnchorError(errors.INTERACTIVE_TOKEN_INVALID, "failed to generate interactive token", err)
	}
	tm.tokenMu.Lock()
	tm.tokenToID[token] = transferID
	tm.tokenMu.Unlock()
	base := strings.TrimRight(tm.config.InteractiveBaseURL, "/")
	if base == "" {
		base = "http://localhost:8000/interactive"
	}
	url := fmt.Sprintf("%s?token=%s", base, token)
	return token, url, nil
}

func isTerminal(status stellarconnect.TransferStatus) bool {
	switch status {
	case stellarconnect.StatusCompleted,
		stellarconnect.StatusFailed,
		stellarconnect.StatusDenied,
		stellarconnect.StatusCancelled,
		stellarconnect.StatusExpired:
		return true
	default:
		return false
	}
}
