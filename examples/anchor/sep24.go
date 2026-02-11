package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	stellarconnect "github.com/stellar-connect/sdk-go"
	"github.com/stellar-connect/sdk-go/anchor"
)

// SEP-24 Info response structure
type sep24InfoResponse struct {
	Deposit  map[string]assetInfo `json:"deposit"`
	Withdraw map[string]assetInfo `json:"withdraw"`
}

type assetInfo struct {
	Enabled    bool    `json:"enabled"`
	FeeFixed   float64 `json:"fee_fixed"`
	FeePercent float64 `json:"fee_percent"`
	MinAmount  float64 `json:"min_amount"`
	MaxAmount  float64 `json:"max_amount"`
}

// SEP-24 Interactive response structure
type sep24InteractiveResponse struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	ID   string `json:"id"`
}

// SEP-24 Transactions list response
type sep24TransactionsResponse struct {
	Transactions []*anchor.TransferStatusResponse `json:"transactions"`
}

// handleSEP24Info returns asset information for SEP-24 deposits and withdrawals.
// No authentication required per SEP-24 spec.
func handleSEP24Info() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := sep24InfoResponse{
			Deposit: map[string]assetInfo{
				"USDC": {
					Enabled:    true,
					FeeFixed:   0,
					FeePercent: 0,
					MinAmount:  0.1,
					MaxAmount:  10000,
				},
			},
			Withdraw: map[string]assetInfo{
				"USDC": {
					Enabled:    true,
					FeeFixed:   0,
					FeePercent: 0,
					MinAmount:  0.1,
					MaxAmount:  10000,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// handleDepositInteractive initiates an interactive deposit flow.
// Requires JWT authentication.
func handleDepositInteractive(tm *anchor.TransferManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := anchor.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		assetCode, account, amount, err := parseDepositRequest(r)
		if err != nil {
			http.Error(w, `{"error":"invalid request format"}`, http.StatusBadRequest)
			return
		}

		// Use account from JWT claims if not provided
		if strings.TrimSpace(account) == "" {
			account = claims.Subject
		}

		if strings.TrimSpace(assetCode) == "" {
			http.Error(w, `{"error":"asset_code is required"}`, http.StatusBadRequest)
			return
		}

		req := anchor.DepositRequest{
			Account:   account,
			AssetCode: assetCode,
			Amount:    amount,
			Mode:      stellarconnect.ModeInteractive,
		}

		result, err := tm.InitiateDeposit(context.Background(), req)
		if err != nil {
			http.Error(w, `{"error":"failed to initiate deposit"}`, http.StatusInternalServerError)
			return
		}

		response := sep24InteractiveResponse{
			Type: "interactive",
			URL:  result.InteractiveURL,
			ID:   result.ID,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// handleWithdrawInteractive initiates an interactive withdrawal flow.
// Requires JWT authentication.
func handleWithdrawInteractive(tm *anchor.TransferManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := anchor.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		assetCode, account, amount, dest, err := parseWithdrawRequest(r)
		if err != nil {
			http.Error(w, `{"error":"invalid request format"}`, http.StatusBadRequest)
			return
		}

		// Use account from JWT claims if not provided
		if strings.TrimSpace(account) == "" {
			account = claims.Subject
		}

		if strings.TrimSpace(assetCode) == "" {
			http.Error(w, `{"error":"asset_code is required"}`, http.StatusBadRequest)
			return
		}

		req := anchor.WithdrawalRequest{
			Account:   account,
			AssetCode: assetCode,
			Amount:    amount,
			Dest:      dest,
			Mode:      stellarconnect.ModeInteractive,
		}

		result, err := tm.InitiateWithdrawal(context.Background(), req)
		if err != nil {
			http.Error(w, `{"error":"failed to initiate withdrawal"}`, http.StatusInternalServerError)
			return
		}

		response := sep24InteractiveResponse{
			Type: "interactive",
			URL:  result.InteractiveURL,
			ID:   result.ID,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// handleGetTransaction returns the status of a single transfer by ID.
// Requires JWT authentication.
func handleGetTransaction(tm *anchor.TransferManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := anchor.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		id := r.URL.Query().Get("id")
		if strings.TrimSpace(id) == "" {
			http.Error(w, `{"error":"id parameter is required"}`, http.StatusBadRequest)
			return
		}

		status, err := tm.GetStatus(context.Background(), id)
		if err != nil {
			http.Error(w, `{"error":"transfer not found"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(status)
	}
}

// handleGetTransactions returns a list of transfers for the authenticated account.
// Requires JWT authentication.
func handleGetTransactions(store stellarconnect.TransferStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := anchor.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		assetCode := r.URL.Query().Get("asset_code")

		filters := stellarconnect.TransferFilters{
			Account: claims.Subject,
		}
		if strings.TrimSpace(assetCode) != "" {
			filters.AssetCode = assetCode
		}

		transfers, err := store.List(context.Background(), filters)
		if err != nil {
			http.Error(w, `{"error":"failed to list transfers"}`, http.StatusInternalServerError)
			return
		}

		// Convert Transfer objects to TransferStatusResponse objects
		responses := make([]*anchor.TransferStatusResponse, 0, len(transfers))
		for _, transfer := range transfers {
			moreInfo := "http://localhost:8000/transaction/" + transfer.ID
			resp := &anchor.TransferStatusResponse{
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
			responses = append(responses, resp)
		}

		response := sep24TransactionsResponse{
			Transactions: responses,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// parseDepositRequest parses deposit request from either JSON or FormData
func parseDepositRequest(r *http.Request) (assetCode, account, amount string, err error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req struct {
			AssetCode string `json:"asset_code"`
			Account   string `json:"account"`
			Amount    string `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return "", "", "", err
		}
		return req.AssetCode, req.Account, req.Amount, nil
	}
	// FormData parsing
	if err := r.ParseForm(); err != nil {
		return "", "", "", err
	}
	return r.FormValue("asset_code"), r.FormValue("account"), r.FormValue("amount"), nil
}

// parseWithdrawRequest parses withdrawal request from either JSON or FormData
func parseWithdrawRequest(r *http.Request) (assetCode, account, amount, dest string, err error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req struct {
			AssetCode string `json:"asset_code"`
			Account   string `json:"account"`
			Amount    string `json:"amount"`
			Dest      string `json:"dest"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return "", "", "", "", err
		}
		return req.AssetCode, req.Account, req.Amount, req.Dest, nil
	}
	// FormData parsing
	if err := r.ParseForm(); err != nil {
		return "", "", "", "", err
	}
	return r.FormValue("asset_code"), r.FormValue("account"), r.FormValue("amount"), r.FormValue("dest"), nil
}
