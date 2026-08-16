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
	ErrNoRedis = errors.New("redis not configured")
)

func keywordClient() *bredis.Client {
	return bredis.GetClient("cloud")
}

func SetKeyword(ctx context.Context, keyword, reply string) error {
	c := keywordClient()
	if c == nil {
		return ErrNoRedis
	}
	return c.HSet(ctx, keywordsKey, keyword, reply).Err()
}

func GetKeyword(ctx context.Context, keyword string) (string, error) {
	c := keywordClient()
	if c == nil {
		return "", ErrNoRedis
	}
	return c.HGet(ctx, keywordsKey, keyword).Result()
}