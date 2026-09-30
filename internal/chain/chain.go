package chain

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

// Dial connects and refuses any chain but config.ExpectedChainID (env CHAIN_ID).
func Dial(ctx context.Context, url string) (*ethclient.Client, error) {
	c, err := ethclient.DialContext(ctx, url)
	if err != nil {
		return nil, err
	}
	id, err := c.ChainID(ctx)
	if err != nil {
		c.Close()
		return nil, err
	}
	if want := config.ExpectedChainID(); id.Uint64() != want {
		c.Close()
		return nil, fmt.Errorf("rpc %s is chain %s, want %d", url, id, want)
	}
	return c, nil
}
