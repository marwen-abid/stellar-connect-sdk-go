// Package main implements an anchor server integrated with Etherfuse FX Ramp APIs.
//
// This example demonstrates:
//   - Serving stellar.toml with USDC and CETES assets (SEP-1)
//   - SEP-10 Web Authentication with challenge/response flow
//   - SEP-24 Interactive deposit (MXN -> USDC/CETES via Etherfuse onramp)
//   - SEP-24 Interactive withdrawal (USDC/CETES -> MXN via Etherfuse offramp)
//   - Etherfuse webhook processing for order status updates
//
// Configuration is loaded from a .env file or environment variables.
// See .env.example for all available settings.
//
// Run with: ETHERFUSE_API_KEY=xxx go run ./examples/anchor-etherfuse
package main
