package dao

import (
	"context"
	"errors"
	"fmt"

	bredis "butterfly.orx.me/core/store/redis"
)

const (
	keywordsKeyPrefix = "xbot:keywords"
)

func keywordsKey(chatID int64) string {
	return fmt.Sprintf("%s:%d", keywordsKeyPrefix, chatID)
}

var (
	// ErrNoRedis is returned when the redis client is not configured.
	ErrNoRedis = errors.New("redis not configured")
)

func keywordClient() *bredis.Client {
	return bredis.GetClient("main")
}

// SetKeyword stores the keyword->reply mapping in redis, scoped to the given chat.
func SetKeyword(ctx context.Context, chatID int64, keyword, reply string) error {
	c := keywordClient()
	if c == nil {
		return ErrNoRedis
	}
	return c.HSet(ctx, keywordsKey(chatID), keyword, reply).Err()
}

// GetKeyword returns the exact-match reply for the given keyword in the given chat.
func GetKeyword(ctx context.Context, chatID int64, keyword string) (string, error) {
	c := keywordClient()
	if c == nil {
		return "", ErrNoRedis
	}
	return c.HGet(ctx, keywordsKey(chatID), keyword).Result()
}
