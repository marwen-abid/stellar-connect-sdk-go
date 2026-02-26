// Package toml provides functionality for fetching, parsing, and generating
// stellar.toml files as specified in SEP-1.
//
// The Resolver fetches and caches stellar.toml files from anchor domains,
// while the Publisher generates stellar.toml content for anchor servers.
package toml

// AnchorInfo represents the parsed contents of a stellar.toml file.
// It contains SEP-1, SEP-10, SEP-6, and SEP-24 required fields for anchor discovery.
type AnchorInfo struct {
	// NETWORK_PASSPHRASE identifies the Stellar network (testnet/mainnet).
	NetworkPassphrase string

	// SIGNING_KEY is the anchor's public key used for SEP-10 authentication.
	SigningKey string

	// WEB_AUTH_ENDPOINT is the URL for SEP-10 Stellar Web Authentication.
	WebAuthEndpoint string

	// TransferServerSep6 is the URL for SEP-6 Non-Interactive Deposit/Withdrawal.
	TransferServerSep6 string

	// TransferServerSep24 is the URL for SEP-24 Interactive Deposit/Withdrawal.
	TransferServerSep24 string

	// Currencies lists assets supported by the anchor.
	Currencies []CurrencyInfo
}

// CurrencyInfo describes a Stellar asset supported by an anchor.
// Only fields required by SEP-1 are included.
type CurrencyInfo struct {
	// Code is the asset code (e.g., "USDC", "BTC").
	Code string

	// Issuer is the Stellar public key of the asset issuer.
	Issuer string

	// Status indicates if the asset is live, test, or disabled (optional).
	Status string

	// DisplayDecimals indicates the number of decimals to display (optional).
	DisplayDecimals int

	// AnchorAssetType indicates the asset type (e.g., "crypto", "fiat") (optional).
	AnchorAssetType string

	// IsAssetAnchored indicates whether the asset is anchored to a real-world asset (required by anchor-tests).
	IsAssetAnchored bool

	// Desc is a short description of the asset (required by anchor-tests).
	Desc string

	// Description provides a human-readable description of the asset (optional).
	Description string
}
