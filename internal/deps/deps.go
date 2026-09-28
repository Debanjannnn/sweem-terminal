//go:build tools

// Package deps pins modules services need but no shared helper imports yet, so go mod tidy keeps them.
package deps

import (
	_ "github.com/coder/websocket"
	_ "github.com/ethereum/go-ethereum/accounts/abi/bind"
	_ "github.com/ethereum/go-ethereum/crypto"
	_ "github.com/golang-jwt/jwt/v5"
	_ "github.com/google/uuid"
)
