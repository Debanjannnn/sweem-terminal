package chain

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/snehendu098/sweem-terminal/internal/config"
)

// Dial connects and refuses any chain but Monad testnet.
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
	if id.Uint64() != config.ChainID {
		c.Close()
		return nil, fmt.Errorf("rpc %s is chain %s, want %d", url, id, config.ChainID)
	}
	return c, nil
}
