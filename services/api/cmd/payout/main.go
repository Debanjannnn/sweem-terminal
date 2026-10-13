// payout pays creators their unpaid fee_ledger share in USDC from the treasury. Dry run unless --send.
package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/snehendu098/sweem-terminal/internal/config"
	"github.com/snehendu098/sweem-terminal/internal/pg"
	"github.com/snehendu098/sweem-terminal/services/api/internal/api"
)

func main() {
	send := flag.Bool("send", false, "sign and send the transfers (default: dry run)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *send); err != nil {
		fmt.Fprintln(os.Stderr, "payout:", err)
		os.Exit(1)
	}
}

var erc20, _ = abi.JSON(strings.NewReader(`[{"name":"transfer","type":"function","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[{"type":"bool"}]}]`))

func run(ctx context.Context, send bool) error {
	var e config.Env
	dbURL, depPath := e.Str("DATABASE_URL"), e.Opt("DEPLOYMENTS_PATH", "contracts/deployments/143.json")
	var rpcURL, keyHex string
	if send {
		rpcURL, keyHex = e.Str("RPC_URL"), e.Str("TREASURY_PRIVATE_KEY")
	}
	if err := e.Err(); err != nil {
		return err
	}
	dep, err := config.LoadDeployments143(depPath)
	if err != nil {
		return err
	}
	usdc := dep.Tokens["USDC"]
	db, err := pg.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	ds, err := dues(ctx, db, "")
	if err != nil {
		return err
	}
	for _, d := range ds {
		fmt.Printf("%s\t%s\t%s USDC\t%d rows\n", d.Creator, or(d.Wallet, "<no wallet: skipped>"), d.Total, len(d.IDs))
	}
	if !send || len(ds) == 0 {
		return nil
	}

	key, err := crypto.HexToECDSA(strings.TrimPrefix(keyHex, "0x"))
	if err != nil {
		return errors.New("TREASURY_PRIVATE_KEY: not a hex key")
	}
	if from := crypto.PubkeyToAddress(key.PublicKey); !strings.EqualFold(from.Hex(), dep.Treasury) {
		return fmt.Errorf("key is %s, deployments treasury is %q: refusing", from, dep.Treasury)
	}
	eth, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return err
	}
	defer eth.Close()
	if id, err := eth.ChainID(ctx); err != nil || id.Uint64() != config.MainnetChainID {
		return fmt.Errorf("rpc chain %v (%v), want %d", id, err, config.MainnetChainID)
	}
	t := transferer(eth, key, common.HexToAddress(usdc.Address))
	for _, d := range ds {
		if d.Wallet == "" {
			continue
		}
		paid, hash, err := pay(ctx, db, d.Creator, int32(usdc.Decimals), t)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Creator, err)
		}
		fmt.Printf("paid %s %s USDC (%d rows) tx %s\n", paid.Wallet, paid.Total, len(paid.IDs), hash)
	}
	return nil
}

type due struct {
	Creator, Wallet string
	Total           decimal.Decimal
	IDs             []string
}

// creator's payout wallet: active delegation, else the last client-signing wallet (as api KnownWallet).
const unpaidQ = `select f.execution_id::text, f.creator, f.creator_owed_usdc::text,
  coalesce((select lower(wallet_address) from delegations where user_id = f.creator and active), (select lower(wallet_address) from users where id = f.creator), '')
from fee_ledger f where not f.paid and f.creator is not null and f.creator_owed_usdc > 0 and ($1::text = '' or f.creator = $1) and ` + api.FeeCollected

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// dues sums unpaid, collected creator shares per creator (one creator when set), creator order.
func dues(ctx context.Context, q querier, creator string, lock ...string) ([]due, error) {
	rows, err := q.Query(ctx, unpaidQ+` order by f.creator, f.created_at`+strings.Join(lock, ""), creator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ds []due
	for rows.Next() {
		var id, c, owed, wallet string
		if err := rows.Scan(&id, &c, &owed, &wallet); err != nil {
			return nil, err
		}
		v, err := decimal.NewFromString(owed)
		if err != nil {
			return nil, err
		}
		if len(ds) == 0 || ds[len(ds)-1].Creator != c {
			ds = append(ds, due{Creator: c, Wallet: wallet})
		}
		d := &ds[len(ds)-1]
		d.Total, d.IDs = d.Total.Add(v), append(d.IDs, id)
	}
	return ds, rows.Err()
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

type transfer func(ctx context.Context, to common.Address, atoms *big.Int) (string, error)

func transferer(eth *ethclient.Client, key *ecdsa.PrivateKey, token common.Address) transfer {
	c := bind.NewBoundContract(token, erc20, eth, eth, eth)
	return func(ctx context.Context, to common.Address, atoms *big.Int) (string, error) {
		opts, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(config.MainnetChainID))
		if err != nil {
			return "", err
		}
		opts.Context = ctx
		tx, err := c.Transact(opts, "transfer", to, atoms)
		if err != nil {
			return "", err
		}
		r, err := bind.WaitMined(ctx, eth, tx)
		if err != nil {
			return tx.Hash().Hex(), err
		}
		if r.Status != types.ReceiptStatusSuccessful {
			return tx.Hash().Hex(), errors.New("transfer reverted")
		}
		return tx.Hash().Hex(), nil
	}
}

// pay locks creator's unpaid rows, sends their sum and marks exactly those rows paid.
// ponytail: sub-atom dust of the sum is floored away; a crash between a mined transfer and commit leaves the rows unpaid
// (re-paid next run) — the tx hash in the error is the audit trail; add a payout_tx column if this runs unattended.
func pay(ctx context.Context, db *pgxpool.Pool, creator string, dec int32, send transfer) (d due, hash string, err error) {
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ds, err := dues(ctx, tx, creator, ` for update of f`)
		if err != nil || len(ds) == 0 {
			return err
		}
		d = ds[0]
		atoms := d.Total.Shift(dec).Floor()
		if d.Wallet == "" || !atoms.IsPositive() {
			return nil
		}
		if hash, err = send(ctx, common.HexToAddress(d.Wallet), atoms.BigInt()); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update fee_ledger set paid = true where execution_id = any($1::uuid[])`, d.IDs)
		return err
	})
	if err != nil && hash != "" {
		err = fmt.Errorf("tx %s (rows %v): %w", hash, d.IDs, err)
	}
	return
}
