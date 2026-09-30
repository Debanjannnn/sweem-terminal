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

// ExpectedChainID is the chain every service must be on: env CHAIN_ID, default testnet until services migrate.
func ExpectedChainID() uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("CHAIN_ID")), 10, 64)
	if err != nil {
		return ChainID
	}
	return id
}

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

const MainnetChainID = 143

type Token struct {
	Address  string `json:"address"`
	Decimals uint8  `json:"decimals"`
}

type Market struct {
	Orderbook      string `json:"orderbook"`
	Base           string `json:"base"`
	Quote          string `json:"quote"`
	PricePrecision uint64 `json:"pricePrecision"`
	SizePrecision  uint64 `json:"sizePrecision"`
	TickSize       uint64 `json:"tickSize"`
	MinSize        uint64 `json:"minSize"`
}

type AavePool struct {
	Pool         string            `json:"pool"`
	DataProvider string            `json:"dataProvider"`
	ATokens      map[string]string `json:"aTokens"`
}

type Vault struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Asset    string `json:"asset"`
	Vault    string `json:"vault"`
}

type Deployments143 struct {
	ChainID    uint64           `json:"chainId"`
	StartBlock uint64           `json:"startBlock"`
	Treasury   string           `json:"treasury"`
	Tokens     map[string]Token `json:"tokens"`
	Kuru       struct {
		Router        string            `json:"router"`
		MarginAccount string            `json:"marginAccount"`
		ActiveVault   string            `json:"activeVault"`
		Markets       map[string]Market `json:"markets"`
	} `json:"kuru"`
	Lending struct {
		AaveV3    AavePool `json:"aaveV3"`
		Neverland AavePool `json:"neverland"`
		ERC4626   []Vault  `json:"erc4626"`
	} `json:"lending"`
	Staking struct {
		Shmonad string `json:"shmonad"`
	} `json:"staking"`
	Perpl struct {
		Exchange string         `json:"exchange"`
		AUSD     string         `json:"ausd"`
		PerpIds  map[string]int `json:"perpIds"`
	} `json:"perpl"`
	Swap struct {
		AUSD3Pool struct {
			Pool  string         `json:"pool"`
			Coins map[string]int `json:"coins"`
		} `json:"ausd3pool"`
	} `json:"swap"`
}

func LoadDeployments143(path string) (*Deployments143, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Deployments143
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if d.ChainID != MainnetChainID {
		return nil, fmt.Errorf("%s: chainId %d, want %d", path, d.ChainID, MainnetChainID)
	}
	if d.Tokens["USDC"].Address == "" || len(d.Kuru.Markets) == 0 {
		return nil, fmt.Errorf("%s: tokens.USDC or kuru.markets missing", path)
	}
	return &d, nil
}
