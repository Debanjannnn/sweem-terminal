package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env collects every missing/invalid var so a service reports them all at once.
//
//	var e config.Env
//	rpc := e.Str("RPC_URL")
//	if err := e.Err(); err != nil { ... }
type Env struct{ errs []error }

func (e *Env) Str(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		e.errs = append(e.errs, fmt.Errorf("%s is required", key))
	}
	return v
}

func (e *Env) Opt(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (e *Env) Int(key string, def int) int { return parse(e, key, def, strconv.Atoi) }

func (e *Env) Bool(key string, def bool) bool { return parse(e, key, def, strconv.ParseBool) }

func (e *Env) Duration(key string, def time.Duration) time.Duration {
	return parse(e, key, def, time.ParseDuration)
}

func (e *Env) Err() error { return errors.Join(e.errs...) }

func parse[T any](e *Env, key string, def T, fn func(string) (T, error)) T {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := fn(raw)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s=%q: %w", key, raw, err))
		return def
	}
	return v
}

const ChainID = 10143

type Deployments struct {
	ChainID    uint64            `json:"chainId"`
	Factory    string            `json:"factory"`
	Baskets    map[string]string `json:"baskets"`
	StartBlock uint64            `json:"startBlock"`
	Kuru       struct {
		AccountCore string            `json:"accountCore"`
		SpotRouter  string            `json:"spotRouter"`
		USDC        string            `json:"usdc"`
		Faucet      string            `json:"faucet"`
		Markets     map[string]string `json:"markets"`
	} `json:"kuru"`
}

func LoadDeployments(path string) (*Deployments, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Deployments
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if d.ChainID != ChainID {
		return nil, fmt.Errorf("%s: chainId %d, want %d", path, d.ChainID, ChainID)
	}
	if d.Factory == "" {
		return nil, fmt.Errorf("%s: factory missing", path)
	}
	return &d, nil
}
