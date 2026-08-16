package dao

import (
	"context"
	"errors"

	bredis "butterfly.orx.me/core/store/redis"
)

const (
	keywordsKey = "xbot:keywords"
)

var (
	// ErrNoRedis is returned when the redis client is not configured.
	ErrNoRedis = errors.New("redis not configured")
)

func keywordClient() *bredis.Client {
	return bredis.GetClient("main")
}

// SetKeyword stores the keyword->reply mapping in redis.
func SetKeyword(ctx context.Context, keyword, reply string) error {
	c := keywordClient()
	if c == nil {
		return ErrNoRedis
	}
	return c.HSet(ctx, keywordsKey, keyword, reply).Err()
}

// GetKeyword returns the exact-match reply for the given keyword.
func GetKeyword(ctx context.Context, keyword string) (string, error) {
	c := keywordClient()
	if c == nil {
		return "", ErrNoRedis
	}
	return c.HGet(ctx, keywordsKey, keyword).Result()
}
