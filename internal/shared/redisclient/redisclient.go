package redisclient

import (
	"github.com/redis/go-redis/v9"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
)

func New() (*redis.Client, error) {
	opt, err := redis.ParseURL(config.GetEnv("REDIS_URL", "redis://localhost:6379"))
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opt), nil
}
