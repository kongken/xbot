package dao

import (
	"context"
	"errors"
	"fmt"
	"log"

	"go.orx.me/xbot/internal/conf"
)

// ErrNoStorage is returned when no storage is configured
var ErrNoStorage = errors.New("no message storage configured")

// Init initializes all database connections and storage components
func Init(ctx context.Context) error {
	log.Println("Initializing data access layer...")

	if err := InitMongo(context.Background()); err != nil {
		if hasMem0Ingestion() {
			return fmt.Errorf("mem0 ingestion requires mongo: %w", err)
		}
		log.Printf("Warning: mongo unavailable (%v); continuing without it", err)
	}

	if hasMem0Ingestion() && db != nil {
		if err := InitMem0Outbox(context.Background()); err != nil {
			return fmt.Errorf("init mem0 outbox: %w", err)
		}
		log.Println("Mem0 outbox initialized")
	}

	// Message storage configuration
	storage := conf.Conf.MessageStorage
	log.Printf("Message storage configuration: %s", storage)

	// Initialize based on configuration or initialize both with priority
	switch storage {
	case storageTypeMongoDB:
		defaultMessageStorage = &MongoDBStorage{
			messagesColl: messagesColl,
		}

	case storageTypeS3:
		// Initialize only S3/MinIO
		errMinio := InitMinio()
		if errMinio != nil {
			log.Printf("Failed to initialize MinIO: %v", errMinio)
			return fmt.Errorf("failed to initialize configured storage S3: %w", errMinio)
		}
		log.Println("MinIO initialized and set as message storage")

	default:
	}

	return nil
}

// hasMem0Ingestion reports whether any enabled bot configures memory Chat IDs.
func hasMem0Ingestion() bool {
	for _, bot := range conf.Conf.Bots {
		if len(bot.Memory.ChatIDs) > 0 {
			return true
		}
	}
	return false
}
