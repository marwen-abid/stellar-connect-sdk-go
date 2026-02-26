package account

import (
	"context"
	"fmt"

	stellarconnect "github.com/stellar-connect/sdk-go"
	"github.com/stellar/go-stellar-sdk/clients/horizonclient"
)

// HorizonAccountFetcher implements stellarconnect.AccountFetcher using a Horizon server.
type HorizonAccountFetcher struct {
	client *horizonclient.Client
}

// NewHorizonAccountFetcher creates an AccountFetcher backed by the given Horizon URL.
func NewHorizonAccountFetcher(horizonURL string) *HorizonAccountFetcher {
	return &HorizonAccountFetcher{
		client: &horizonclient.Client{HorizonURL: horizonURL},
	}
}

// FetchSigners returns the signers and thresholds for a Stellar account.
func (f *HorizonAccountFetcher) FetchSigners(_ context.Context, accountID string) ([]stellarconnect.AccountSigner, stellarconnect.AccountThresholds, error) {
	account, err := f.client.AccountDetail(horizonclient.AccountRequest{
		AccountID: accountID,
	})
	if err != nil {
		return nil, stellarconnect.AccountThresholds{}, fmt.Errorf("failed to fetch account %s: %w", accountID, err)
	}

	signers := make([]stellarconnect.AccountSigner, len(account.Signers))
	for i, s := range account.Signers {
		signers[i] = stellarconnect.AccountSigner{
			Key:    s.Key,
			Weight: s.Weight,
		}
	}

	thresholds := stellarconnect.AccountThresholds{
		Low:    account.Thresholds.LowThreshold,
		Medium: account.Thresholds.MedThreshold,
		High:   account.Thresholds.HighThreshold,
	}

	return signers, thresholds, nil
}
