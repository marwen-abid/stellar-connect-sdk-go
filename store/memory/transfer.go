// Package memory provides in-memory implementations of store interfaces.
// The TransferStore implementation uses a map[string]*Transfer with sync.RWMutex
// for thread-safe CRUD operations. It is suitable for examples, testing, and
// small-scale anchor services without persistent storage requirements.
package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	stellarconnect "github.com/stellar-connect/sdk-go"
)

// TransferStore is an in-memory implementation of stellarconnect.TransferStore.
// It stores transfers in a map with thread-safe access via sync.RWMutex.
// All transfers are keyed by their ID field.
type TransferStore struct {
	transfers map[string]*stellarconnect.Transfer
	mu        sync.RWMutex
}

// NewTransferStore creates a new in-memory transfer store.
func NewTransferStore() *TransferStore {
	return &TransferStore{
		transfers: make(map[string]*stellarconnect.Transfer),
	}
}

// Save persists a new transfer record.
// Returns an error if a transfer with the same ID already exists.
func (s *TransferStore) Save(ctx context.Context, transfer *stellarconnect.Transfer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.transfers[transfer.ID]; exists {
		return errors.New("transfer already exists")
	}

	s.transfers[transfer.ID] = transfer
	return nil
}

// FindByID retrieves a transfer by its unique identifier.
// Returns an error if the transfer is not found.
func (s *TransferStore) FindByID(ctx context.Context, id string) (*stellarconnect.Transfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transfer, exists := s.transfers[id]
	if !exists {
		return nil, errors.New("transfer not found")
	}

	return transfer, nil
}

// FindByAccount returns all transfers for a given Stellar account.
// Returns a slice of matching transfers (or empty slice if none found).
func (s *TransferStore) FindByAccount(ctx context.Context, account string) ([]*stellarconnect.Transfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*stellarconnect.Transfer
	for _, transfer := range s.transfers {
		if transfer.Account == account {
			result = append(result, transfer)
		}
	}

	return result, nil
}

// Update applies partial updates to an existing transfer.
// Only non-nil fields in the update are applied.
// Returns an error if the transfer does not exist.
func (s *TransferStore) Update(ctx context.Context, id string, update *stellarconnect.TransferUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	transfer, exists := s.transfers[id]
	if !exists {
		return errors.New("transfer not found")
	}

	// Apply non-nil fields from the update struct
	if update.Status != nil {
		transfer.Status = *update.Status
	}
	if update.Amount != nil {
		transfer.Amount = *update.Amount
	}
	if update.ExternalRef != nil {
		transfer.ExternalRef = *update.ExternalRef
	}
	if update.StellarTxHash != nil {
		transfer.StellarTxHash = *update.StellarTxHash
	}
	if update.InteractiveToken != nil {
		transfer.InteractiveToken = *update.InteractiveToken
	}
	if update.InteractiveURL != nil {
		transfer.InteractiveURL = *update.InteractiveURL
	}
	if update.Message != nil {
		transfer.Message = *update.Message
	}
	if update.Metadata != nil {
		transfer.Metadata = update.Metadata
	}
	if update.CompletedAt != nil {
		transfer.CompletedAt = update.CompletedAt
	}

	// Always update UpdatedAt to current time
	transfer.UpdatedAt = time.Now()

	return nil
}

// List returns transfers matching the given filters.
// Filters by account, asset code, status, and kind fields.
// Returns a slice of matching transfers (or empty slice if none found).
func (s *TransferStore) List(ctx context.Context, filters stellarconnect.TransferFilters) ([]*stellarconnect.Transfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*stellarconnect.Transfer

	for _, transfer := range s.transfers {
		// Apply filters
		if filters.Account != "" && transfer.Account != filters.Account {
			continue
		}
		if filters.AssetCode != "" && transfer.AssetCode != filters.AssetCode {
			continue
		}
		if filters.Status != nil && transfer.Status != *filters.Status {
			continue
		}
		if filters.Kind != nil && transfer.Kind != *filters.Kind {
			continue
		}

		result = append(result, transfer)
	}

	return result, nil
}

// Verify that TransferStore implements stellarconnect.TransferStore
var _ stellarconnect.TransferStore = (*TransferStore)(nil)
