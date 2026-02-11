package signers

import (
	"context"
	"fmt"

	"github.com/stellar-connect/sdk-go"
	"github.com/stellar/go/keypair"
)

// keypairSigner wraps a stellar/go keypair for signing transactions.
type keypairSigner struct {
	kp *keypair.Full
}

// FromSecret creates a Signer from a Stellar secret key (S...).
// Intended for server-side use (exchanges, backends, bots).
// Returns an error if the secret key is invalid.
func FromSecret(secret string) (stellarconnect.Signer, error) {
	kp, err := keypair.ParseFull(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid secret key: %w", err)
	}
	return &keypairSigner{kp: kp}, nil
}

// PublicKey returns the Stellar address (G...) for this keypair.
func (s *keypairSigner) PublicKey() string {
	return s.kp.Address()
}

// SignTransaction signs a Stellar transaction envelope (base64 XDR).
// Returns the signed envelope as base64 XDR.
func (s *keypairSigner) SignTransaction(ctx context.Context, xdr string) (string, error) {
	signed, err := s.kp.SignBase64([]byte(xdr))
	if err != nil {
		return "", err
	}
	return signed, nil
}
