package dao

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.orx.me/xbot/internal/conf"
)

// Mem0 outbox projection delivery states.
const (
	Mem0StatusPending = "pending"
	Mem0StatusDone    = "done"
	Mem0StatusDead    = "dead"

	maxErrorLength    = 500
	ellipsisLength    = 3
	defaultQueryLimit = 100
)

// Mem0Event is one durable Telegram group message queued for Mem0 ingestion.
type Mem0Event struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	DedupKey      string        `bson:"dedup_key"`
	BotName       string        `bson:"bot_name"`
	ChatID        int64         `bson:"chat_id"`
	MessageID     int           `bson:"message_id"`
	MessageDate   int64         `bson:"message_date"`
	SenderID      int64         `bson:"sender_id"`
	SenderLabel   string        `bson:"sender_label"`
	Content       string        `bson:"content"`
	IsEdit        bool          `bson:"is_edit"`
	GroupStatus   string        `bson:"group_status"`
	ProfileStatus string        `bson:"profile_status"`
	Attempts      int           `bson:"attempts"`
	NextAttemptAt int64         `bson:"next_attempt_at"`
	LastError     string        `bson:"last_error,omitempty"`
	CreatedAt     int64         `bson:"created_at"`
}

// Mem0Outbox provides durable storage for Telegram messages destined for Mem0.
type Mem0Outbox struct {
	coll *mongo.Collection
}

var mem0Outbox *Mem0Outbox

// InitMem0Outbox wires up the Mem0 outbox collection and its indexes.
// It must be called after InitMongo succeeds.
func InitMem0Outbox(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("mongo client not initialized")
	}
	mem0Outbox = &Mem0Outbox{
		coll: db.Database(conf.Conf.DBName).Collection("mem0_outbox"),
	}

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "dedup_key", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "bot_name", Value: 1},
				{Key: "chat_id", Value: 1},
				{Key: "group_status", Value: 1},
				{Key: "message_date", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "bot_name", Value: 1},
				{Key: "chat_id", Value: 1},
				{Key: "sender_id", Value: 1},
				{Key: "profile_status", Value: 1},
				{Key: "message_date", Value: 1},
			},
		},
	}
	_, err := mem0Outbox.coll.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("create mem0 outbox indexes: %w", err)
	}
	return nil
}

// GetMem0Outbox returns the shared outbox, or nil when unavailable.
func GetMem0Outbox() *Mem0Outbox {
	return mem0Outbox
}

// Save stores a new event, deduplicating by bot+update. Existing rows are left
// untouched so Telegram webhook redelivery does not create duplicates.
func (o *Mem0Outbox) Save(ctx context.Context, event *Mem0Event) error {
	if len(event.GroupStatus) == 0 {
		event.GroupStatus = Mem0StatusPending
	}
	if len(event.ProfileStatus) == 0 {
		if profilesDisabled() {
			event.ProfileStatus = Mem0StatusDone
		} else {
			event.ProfileStatus = Mem0StatusPending
		}
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = event.MessageDate
	}

	filter := bson.M{"dedup_key": event.DedupKey}
	update := bson.M{
		"$setOnInsert": bson.M{
			"dedup_key":      event.DedupKey,
			"bot_name":       event.BotName,
			"chat_id":        event.ChatID,
			"message_id":     event.MessageID,
			"message_date":   event.MessageDate,
			"sender_id":      event.SenderID,
			"sender_label":   event.SenderLabel,
			"content":        event.Content,
			"is_edit":        event.IsEdit,
			"group_status":   event.GroupStatus,
			"profile_status": event.ProfileStatus,
			"attempts":       0,
			"created_at":     event.CreatedAt,
		},
	}
	_, err := o.coll.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save mem0 event: %w", err)
	}
	return nil
}

// markStatus updates projection delivery state for a set of events.
func (o *Mem0Outbox) markStatus(
	ctx context.Context,
	botName string,
	chatID int64,
	dedupKeys []string,
	set bson.M,
) error {
	_, err := o.coll.UpdateMany(
		ctx,
		bson.M{
			"bot_name":  botName,
			"chat_id":   chatID,
			"dedup_key": bson.M{"$in": dedupKeys},
		},
		bson.M{"$set": set},
	)
	if err != nil {
		return fmt.Errorf("mark mem0 events: %w", err)
	}
	return nil
}

// MarkGroupProcessed marks delivered group-projection events complete.
func (o *Mem0Outbox) MarkGroupProcessed(ctx context.Context, botName string, chatID int64, dedupKeys []string) error {
	return o.markStatus(ctx, botName, chatID, dedupKeys, bson.M{
		"group_status": Mem0StatusDone,
		"processed_at": nowUnix(),
	})
}

// MarkProfileProcessed marks delivered user-profile-projection events complete.
func (o *Mem0Outbox) MarkProfileProcessed(ctx context.Context, botName string, chatID int64, dedupKeys []string) error {
	return o.markStatus(ctx, botName, chatID, dedupKeys, bson.M{
		"profile_status": Mem0StatusDone,
		"processed_at":   nowUnix(),
	})
}

// MarkDead flags events whose batch permanently failed after retries.
func (o *Mem0Outbox) MarkDead(ctx context.Context, dedupKeys []string, message string) error {
	_, err := o.coll.UpdateMany(
		ctx,
		bson.M{"dedup_key": bson.M{"$in": dedupKeys}},
		bson.M{"$set": bson.M{
			"group_status":   Mem0StatusDead,
			"profile_status": Mem0StatusDead,
			"last_error":     truncate(message, maxErrorLength),
		}},
	)
	if err != nil {
		return fmt.Errorf("mark mem0 events dead: %w", err)
	}
	return nil
}

// RecordFailure increments the retry counter and schedules the next attempt.
func (o *Mem0Outbox) RecordFailure(ctx context.Context, dedupKeys []string, nextAttemptAt int64, message string) error {
	_, err := o.coll.UpdateMany(
		ctx,
		bson.M{"dedup_key": bson.M{"$in": dedupKeys}},
		bson.M{
			"$inc": bson.M{"attempts": 1},
			"$set": bson.M{
				"next_attempt_at": nextAttemptAt,
				"last_error":      truncate(message, maxErrorLength),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("record mem0 failure: %w", err)
	}
	return nil
}

// PendingProfileSenders returns distinct sender IDs with pending profile work
// in a chat.
func (o *Mem0Outbox) PendingProfileSenders(ctx context.Context, botName string, chatID int64) ([]int64, error) {
	res := o.coll.Distinct(ctx, "sender_id", bson.M{
		"bot_name":       botName,
		"chat_id":        chatID,
		"profile_status": Mem0StatusPending,
	})
	if err := res.Err(); err != nil {
		return nil, fmt.Errorf("distinct mem0 senders: %w", err)
	}
	var senders []int64
	if err := res.Decode(&senders); err != nil {
		return nil, fmt.Errorf("decode mem0 senders: %w", err)
	}
	return senders, nil
}

// PendingByChat returns pending group-projection events for one chat, in
// chronological order, capped at limit.
func (o *Mem0Outbox) PendingByChat(ctx context.Context, botName string, chatID int64, limit int) ([]*Mem0Event, error) {
	result, err := findByQuery(ctx, o.coll, bson.M{
		"bot_name":     botName,
		"chat_id":      chatID,
		"group_status": Mem0StatusPending,
	}, "message_date", 1, limit)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PendingByChatSender returns pending profile-projection events for one sender
// in a chat, in chronological order, capped at limit.
func (o *Mem0Outbox) PendingByChatSender(
	ctx context.Context,
	botName string,
	chatID int64,
	senderID int64,
	limit int,
) ([]*Mem0Event, error) {
	result, err := findByQuery(ctx, o.coll, bson.M{
		"bot_name":       botName,
		"chat_id":        chatID,
		"sender_id":      senderID,
		"profile_status": Mem0StatusPending,
	}, "message_date", 1, limit)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RecentEvents returns processed events in a chat within [minDate, maxDate],
// oldest first, capped at limit.
func (o *Mem0Outbox) RecentEvents(
	ctx context.Context,
	botName string,
	chatID int64,
	minDate, maxDate int64,
	limit int,
) ([]*Mem0Event, error) {
	result, err := findByQuery(ctx, o.coll, bson.M{
		"bot_name":     botName,
		"chat_id":      chatID,
		"message_date": bson.M{"$gte": minDate, "$lte": maxDate},
	}, "message_date", 1, limit)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func findByQuery(
	ctx context.Context,
	coll *mongo.Collection,
	filter bson.M,
	sortField string,
	order, limit int,
) ([]*Mem0Event, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	cursor, err := coll.Find(
		ctx,
		filter,
		options.Find().
			SetSort(bson.D{{Key: sortField, Value: order}}).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("query mem0 events: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var events []*Mem0Event
	for cursor.Next(ctx) {
		var event Mem0Event
		if err := cursor.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode mem0 event: %w", err)
		}
		events = append(events, &event)
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate mem0 events: %w", err)
	}
	return events, nil
}

// profilesDisabled reports whether the per-sender projection should be
// short-circuited to done so no profile work is scheduled.
func profilesDisabled() bool {
	return !conf.Conf.Mem0.Effective().UserProfiles()
}

func nowUnix() int64 {
	return time.Now().Unix()
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit <= ellipsisLength {
		return s[:limit]
	}
	return s[:limit-ellipsisLength] + "..."
}
