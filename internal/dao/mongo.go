package dao

import (
	"context"
	"errors"
	"time"

	bmongo "butterfly.orx.me/core/store/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.orx.me/xbot/internal/conf"
)

var (
	db           *mongo.Client
	usersColl    *mongo.Collection
	promtsColl   *mongo.Collection
	messagesColl *mongo.Collection
	pollColl     *mongo.Collection
	botsColl     *mongo.Collection
)

type Promt struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	ChatID    int64         `bson:"chat_id" json:"chat_id"`
	Promt     string        `bson:"promt" json:"promt"`
	CreatedAt int64         `bson:"created_at" json:"created_at"`
	UpdatedAt int64         `bson:"updated_at" json:"updated_at"`
}

func InitMongo(ctx context.Context) error {
	db = bmongo.GetClient("cloud")
	if db == nil {
		return errors.New("mongo client is nil")
	}
	usersColl = db.Database(conf.Conf.DBName).Collection("users")
	promtsColl = db.Database(conf.Conf.DBName).Collection("promts")
	messagesColl = db.Database(conf.Conf.DBName).Collection("messages")
	pollColl = db.Database(conf.Conf.DBName).Collection("pulls")
	botsColl = db.Database(conf.Conf.DBName).Collection("bots")

	return nil
}

func SavePromt(ctx context.Context, promt Promt) error {
	now := time.Now().Unix()
	// get
	_, err := GetPromt(ctx, promt.ChatID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			promt.CreatedAt = now
			promt.UpdatedAt = now
			_, err := promtsColl.InsertOne(ctx, promt)
			return err
		}
		return err
	}

	// update
	update := bson.M{
		"$set": bson.M{
			"promt":      promt.Promt,
			"updated_at": now,
		},
	}
	_, err = promtsColl.UpdateOne(ctx, bson.M{"chat_id": promt.ChatID}, update)
	return err
}

func GetPromt(ctx context.Context, chatID int64) (*Promt, error) {
	var promt Promt
	err := promtsColl.FindOne(ctx, bson.M{"chat_id": chatID}).Decode(&promt)
	return &promt, err
}
