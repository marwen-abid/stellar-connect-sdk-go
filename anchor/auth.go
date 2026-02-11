package anchor

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/stellar-connect/sdk-go"
	corecrypto "github.com/stellar-connect/sdk-go/core/crypto"
	"github.com/stellar-connect/sdk-go/errors"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
)

const (
	challengeNonceLength = 48
	challengeTimeout     = 5 * time.Minute
	challengeBaseFee     = int64(100)
	authMethodWebAuth    = "web_auth"
)

type authClaimsContextKey struct{}

var claimsContextKey = authClaimsContextKey{}

type AuthConfig struct {
	Domain            string
	NetworkPassphrase string
	Signer            stellarconnect.Signer
	NonceStore        stellarconnect.NonceStore
	JWTIssuer         stellarconnect.JWTIssuer
	JWTVerifier       stellarconnect.JWTVerifier
}

type AuthIssuer struct {
	domain            string
	networkPassphrase string
	signer            stellarconnect.Signer
	nonceStore        stellarconnect.NonceStore
	jwtIssuer         stellarconnect.JWTIssuer
	jwtVerifier       stellarconnect.JWTVerifier
}

func NewAuthIssuer(config AuthConfig) (*AuthIssuer, error) {
	if strings.TrimSpace(config.Domain) == "" {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "domain is required", nil)
	}
	if strings.TrimSpace(config.NetworkPassphrase) == "" {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "network passphrase is required", nil)
	}
	if config.Signer == nil {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "signer is required", nil)
	}
	if config.NonceStore == nil {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "nonce store is required", nil)
	}
	if config.JWTIssuer == nil {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "JWT issuer is required", nil)
	}
	if config.JWTVerifier == nil {
		return nil, errors.NewAnchorError(errors.CONFIG_INVALID, "JWT verifier is required", nil)
	}

	return &AuthIssuer{
		domain:            config.Domain,
		networkPassphrase: config.NetworkPassphrase,
		signer:            config.Signer,
		nonceStore:        config.NonceStore,
		jwtIssuer:         config.JWTIssuer,
		jwtVerifier:       config.JWTVerifier,
	}, nil
}

func (a *AuthIssuer) CreateChallenge(ctx context.Context, account string) (string, error) {
	if strings.TrimSpace(account) == "" {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "account is required", nil)
	}

	nonce, err := corecrypto.GenerateNonce(challengeNonceLength)
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "failed to generate nonce", err)
	}

	expiresAt := time.Now().Add(challengeTimeout)
	if err := a.nonceStore.Add(ctx, nonce, expiresAt); err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "failed to store nonce", err)
	}

	now := time.Now().UTC()
	maxTime := now.Add(challengeTimeout)
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: account, Sequence: 0},
		IncrementSequenceNum: false,
		Operations: []txnbuild.Operation{
			&txnbuild.ManageData{Name: a.domain + " auth", Value: []byte(nonce)},
			&txnbuild.ManageData{Name: "web_auth_domain", Value: []byte(a.domain)},
		},
		BaseFee: challengeBaseFee,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimebounds(now.Unix(), maxTime.Unix()),
		},
	})
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "failed to build challenge transaction", err)
	}

	xdr, err := tx.Base64()
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "failed to encode challenge transaction", err)
	}

	signedXDR, err := a.signer.SignTransaction(ctx, xdr)
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_BUILD_FAILED, "failed to sign challenge transaction", err)
	}

	return signedXDR, nil
}

func (a *AuthIssuer) VerifyChallenge(ctx context.Context, challengeXDR string) (string, error) {
	if strings.TrimSpace(challengeXDR) == "" {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge XDR is required", nil)
	}

	parsed, err := txnbuild.TransactionFromXDR(challengeXDR)
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "failed to parse challenge transaction", err)
	}

	tx, ok := parsed.Transaction()
	if !ok {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge transaction must not be fee bump", nil)
	}

	operations := tx.Operations()
	if len(operations) < 2 {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge transaction must have at least two operations", nil)
	}

	firstOp, ok := operations[0].(*txnbuild.ManageData)
	if !ok {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "first operation must be manage_data", nil)
	}
	if firstOp.Value == nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge nonce missing", nil)
	}
	if firstOp.Name != a.domain+" auth" {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "invalid challenge operation name", nil)
	}

	nonce := string(firstOp.Value)
	consumed, err := a.nonceStore.Consume(ctx, nonce)
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "failed to consume nonce", err)
	}
	if !consumed {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "nonce already used or expired", nil)
	}

	account := tx.SourceAccount().AccountID
	if strings.TrimSpace(account) == "" {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge transaction missing source account", nil)
	}
	if err := verifyClientSignature(tx, a.networkPassphrase, account); err != nil {
		return "", err
	}

	secondOp, ok := operations[1].(*txnbuild.ManageData)
	if !ok {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "second operation must be manage_data", nil)
	}
	if secondOp.Name != "web_auth_domain" {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "web_auth_domain operation missing", nil)
	}
	if !bytes.Equal(secondOp.Value, []byte(a.domain)) {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "web_auth_domain value mismatch", nil)
	}

	claims := stellarconnect.JWTClaims{
		Subject:    account,
		Issuer:     a.domain,
		AuthMethod: authMethodWebAuth,
	}
	token, err := a.jwtIssuer.Issue(ctx, claims)
	if err != nil {
		return "", errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "failed to issue JWT", err)
	}

	return token, nil
}

func (a *AuthIssuer) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.jwtVerifier == nil {
			http.Error(w, "auth verifier not configured", http.StatusInternalServerError)
			return
		}
		if next == nil {
			http.Error(w, "handler not configured", http.StatusInternalServerError)
			return
		}

		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if token == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		claims, err := a.jwtVerifier.Verify(r.Context(), token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ClaimsFromContext(ctx context.Context) (*stellarconnect.JWTClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*stellarconnect.JWTClaims)
	return claims, ok
}

func verifyClientSignature(tx *txnbuild.Transaction, networkPassphrase, account string) error {
	kp, err := keypair.ParseAddress(account)
	if err != nil {
		return errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "invalid account address", err)
	}
	if len(tx.Signatures()) == 0 {
		return errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge transaction has no signatures", nil)
	}

	hash, err := tx.Hash(networkPassphrase)
	if err != nil {
		return errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "failed to hash challenge transaction", err)
	}

	for _, sig := range tx.Signatures() {
		if sig.Hint != kp.Hint() {
			continue
		}
		if err := kp.Verify(hash[:], sig.Signature); err == nil {
			return nil
		}
	}

	return errors.NewAnchorError(errors.CHALLENGE_VERIFY_FAILED, "challenge transaction not signed by client", nil)
}
