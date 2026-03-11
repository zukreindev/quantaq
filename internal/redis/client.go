package redis

import (
	"context"
	goredis "github.com/redis/go-redis/v9"
)

type Client struct {
	*goredis.Client
}

func NewClient(addr, password string, db int) (*Client, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &Client{Client: rdb}, nil
}
