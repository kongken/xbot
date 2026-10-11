package dao

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	// ErrBotNotFound is returned when a bot does not exist.
	ErrBotNotFound = errors.New("bot not found")
	// ErrBotExists is returned when creating a bot whose name is taken.
	ErrBotExists = errors.New("bot already exists")
)

// BotConfig is a dynamically managed telegram bot.
type BotConfig struct {
	ID bson.ObjectID `bson:"_id,omitempty" json:"-"`
	// Name is the unique identifier, also used in the webhook path.
	Name  string `bson:"name" json:"name"`
	Token string `bson:"token" json:"-"`
	// WebhookSecret is sent by telegram in X-Telegram-Bot-Api-Secret-Token.
	WebhookSecret string `bson:"webhook_secret" json:"-"`
	Enable        bool   `bson:"enable" json:"enable"`
	CreatedAt     int64  `bson:"created_at" json:"created_at"`
	UpdatedAt     int64  `bson:"updated_at" json:"updated_at"`
}

func botsCollection() (*mongo.Collection, error) {
	if botsColl == nil {
		return nil, errors.New("mongo not initialized")
	}
	return botsColl, nil
}

// ListBots returns all stored bots ordered by name.
func ListBots(ctx context.Context) ([]*BotConfig, error) {
	coll, err := botsCollection()
	if err != nil {
		return nil, err
	}
	cur, err := coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"name": 1}))
	if err != nil {
		return nil, err
	}
	var bots []*BotConfig
	if err := cur.All(ctx, &bots); err != nil {
		return nil, err
	}
	return bots, nil
}

// GetBotConfig returns a stored bot by name.
func GetBotConfig(ctx context.Context, name string) (*BotConfig, error) {
	coll, err := botsCollection()
	if err != nil {
		return nil, err
	}
	var b BotConfig
	err = coll.FindOne(ctx, bson.M{"name": name}).Decode(&b)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrBotNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateBotConfig inserts a new bot; fails with ErrBotExists on duplicate name.
func CreateBotConfig(ctx context.Context, b *BotConfig) error {
	coll, err := botsCollection()
	if err != nil {
		return err
	}
	if _, err := GetBotConfig(ctx, b.Name); err == nil {
		return ErrBotExists
	} else if !errors.Is(err, ErrBotNotFound) {
		return err
	}
	now := time.Now().Unix()
	b.CreatedAt = now
	b.UpdatedAt = now
	res, err := coll.InsertOne(ctx, b)
	if err != nil {
		return err
	}
	b.ID, _ = res.InsertedID.(bson.ObjectID)
	return nil
}

// UpdateBotConfig overwrites token/secret/enable of an existing bot.
func UpdateBotConfig(ctx context.Context, b *BotConfig) error {
	coll, err := botsCollection()
	if err != nil {
		return err
	}
	b.UpdatedAt = time.Now().Unix()
	res, err := coll.UpdateOne(ctx, bson.M{"name": b.Name}, bson.M{"$set": bson.M{
		"token":          b.Token,
		"webhook_secret": b.WebhookSecret,
		"enable":         b.Enable,
		"updated_at":     b.UpdatedAt,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrBotNotFound
	}
	return nil
}

// DeleteBotConfig removes a bot by name.
func DeleteBotConfig(ctx context.Context, name string) error {
	coll, err := botsCollection()
	if err != nil {
		return err
	}
	res, err := coll.DeleteOne(ctx, bson.M{"name": name})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrBotNotFound
	}
	return nil
}
