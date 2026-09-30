package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnv(t *testing.T) {
	t.Setenv("A", "x")
	t.Setenv("N", "7")
	t.Setenv("BAD", "nope")
	t.Setenv("D", "3s")
	var e Env
	if e.Str("A") != "x" || e.Opt("MISSING_OPT", "d") != "d" || e.Int("N", 0) != 7 || e.Duration("D", 0) != 3*time.Second {
		t.Fatal("wrong values")
	}
	if e.Err() != nil {
		t.Fatal(e.Err())
	}
	e.Str("MISSING_REQ")
	e.Int("BAD", 1)
	err := e.Err()
	if err == nil || !strings.Contains(err.Error(), "MISSING_REQ") || !strings.Contains(err.Error(), "BAD") {
		t.Fatalf("want both errors, got %v", err)
	}
}

func TestLoadDeployments(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "d.json")
		os.WriteFile(p, []byte(s), 0o600)
		return p
	}
	d, err := LoadDeployments(write(`{"chainId":10143,"factory":"0xf","baskets":{"MAJ":"0xb"},"startBlock":5,
		"kuru":{"accountCore":"0xa","spotRouter":"0xr","usdc":"0xu","faucet":"0xc","markets":{"MON-USDC":"0xm"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Baskets["MAJ"] != "0xb" || d.StartBlock != 5 || d.Kuru.USDC != "0xu" || d.Kuru.Markets["MON-USDC"] != "0xm" {
		t.Fatalf("bad parse %+v", d)
	}
	if _, err := LoadDeployments(write(`{"chainId":143,"factory":"0xf"}`)); err == nil {
		t.Fatal("want chain id error")
	}
}

func TestLoadDeployments143(t *testing.T) {
	d, err := LoadDeployments143("../../contracts/deployments/143.json")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tokens["USDC"].Decimals != 6 || d.Kuru.Markets["MON"].PricePrecision != 1e8 || d.Perpl.PerpIds["ETH"] != 20 ||
		d.Swap.AUSD3Pool.Coins["USDC"] != 1 || d.Lending.AaveV3.ATokens["USDC"] == "" || len(d.Lending.ERC4626) == 0 {
		t.Fatalf("bad parse %+v", d)
	}
	if _, err := LoadDeployments143("../../contracts/deployments/10143.json"); err == nil {
		t.Fatal("want chain id error")
	}
}

func TestExpectedChainID(t *testing.T) {
	t.Setenv("CHAIN_ID", "")
	if ExpectedChainID() != ChainID {
		t.Fatal("default")
	}
	t.Setenv("CHAIN_ID", "143")
	if ExpectedChainID() != 143 {
		t.Fatal("env")
	}
}
