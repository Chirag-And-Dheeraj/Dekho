package statusbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"video-streaming-server/database"
	"video-streaming-server/shared"
	"video-streaming-server/types"
)

const (
	StreamName  = "dekho:events:video-status:v1"
	StreamGroup = "dekho-api"
)

type Bus struct {
	db       *sql.DB
	redis    *redis.Client
	group    string
	consumer string
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func InsertVideoStatusOutboxEvent(ctx context.Context, tx *sql.Tx, userID types.UserID, response types.VideoResponseType) (string, error) {
	eventID := uuid.NewString()
	payload, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal video status event: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_id,aggregate_id,user_id,event_type,payload) VALUES($1,$2,$3,$4,$5)`, eventID, response.ID, string(userID), "video_status", string(payload))
	if err != nil {
		return "", fmt.Errorf("insert video status outbox event: %w", err)
	}
	return eventID, nil
}

func New(db *sql.DB, client *redis.Client) (*Bus, error) {
	instanceID := os.Getenv("API_INSTANCE_ID")
	if instanceID == "" {
		var err error
		instanceID, err = os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("get API hostname for stream group: %w", err)
		}
	}
	instanceID = strings.NewReplacer(" ", "-", ":", "-", "/", "-").Replace(instanceID)
	group := StreamGroup + "-" + instanceID
	consumer := instanceID + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	return &Bus{db: db, redis: client, group: group, consumer: consumer}, nil
}

func (b *Bus) Run(ctx context.Context) error {
	if err := b.redis.XGroupCreateMkStream(ctx, StreamName, b.group, "$").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create status stream consumer group: %w", err)
	}
	wake := make(chan struct{}, 1)
	listener := pq.NewListener(database.ConnectionString(), 2*time.Second, 30*time.Second, func(event pq.ListenerEventType, err error) {
		if event == pq.ListenerEventReconnected {
			slog.Info("reconnected to PostgreSQL outbox notifications")
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		if err != nil {
			slog.Error("PostgreSQL outbox listener error", "error", err)
		}
	})
	defer listener.Close()
	if err := listener.Listen("dekho_outbox_events"); err != nil {
		return fmt.Errorf("listen for outbox events: %w", err)
	}
	initialRelayFailed := false
	if err := b.publishPending(ctx); err != nil {
		slog.Error("initial outbox publish attempt failed; will retry", "error", err)
		initialRelayFailed = true
	}

	go b.consume(ctx)
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer cleanupTicker.Stop()
	if err := b.cleanupPublished(ctx); err != nil {
		slog.Error("failed to clean published outbox events", "error", err)
	}
	retryTimer := time.NewTimer(time.Hour)
	if !retryTimer.Stop() {
		<-retryTimer.C
	}
	retryDelay := 5 * time.Second
	if initialRelayFailed {
		retryTimer.Reset(retryDelay)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-cleanupTicker.C:
			if err := b.cleanupPublished(ctx); err != nil {
				slog.Error("failed to clean published outbox events", "error", err)
			}
		case notification, ok := <-listener.Notify:
			if !ok {
				return errors.New("PostgreSQL outbox notification listener stopped")
			}
			if notification == nil {
				if err := b.publishPending(ctx); err != nil {
					slog.Error("failed to publish pending video status events", "error", err)
					retryTimer.Reset(retryDelay)
					retryDelay = min(retryDelay*2, time.Minute)
				} else {
					retryDelay = 5 * time.Second
					stopTimer(retryTimer)
				}
				continue
			}
			if err := b.publishPending(ctx); err != nil {
				slog.Error("failed to publish pending video status events", "error", err)
				retryTimer.Reset(retryDelay)
				retryDelay = min(retryDelay*2, time.Minute)
			} else {
				retryDelay = 5 * time.Second
				stopTimer(retryTimer)
			}
		case <-wake:
			if err := b.publishPending(ctx); err != nil {
				slog.Error("failed to publish outbox events after PostgreSQL reconnect", "error", err)
				retryTimer.Reset(retryDelay)
				retryDelay = min(retryDelay*2, time.Minute)
			} else {
				retryDelay = 5 * time.Second
				stopTimer(retryTimer)
			}
		case <-retryTimer.C:
			if err := b.publishPending(ctx); err != nil {
				slog.Error("outbox relay retry failed", "error", err)
				retryTimer.Reset(retryDelay)
				retryDelay = min(retryDelay*2, time.Minute)
			} else {
				retryDelay = 5 * time.Second
				stopTimer(retryTimer)
			}
		}
	}
}

func (b *Bus) cleanupPublished(ctx context.Context) error {
	for {
		result, err := b.db.ExecContext(ctx, `DELETE FROM outbox_events WHERE event_id IN (SELECT event_id FROM outbox_events WHERE published_at < NOW() - INTERVAL '30 days' ORDER BY published_at LIMIT 1000)`)
		if err != nil {
			return fmt.Errorf("delete retained outbox events: %w", err)
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read deleted outbox event count: %w", err)
		}
		if deleted < 1000 {
			return nil
		}
	}
}

func (b *Bus) publishPending(ctx context.Context) error {
	for {
		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin outbox relay transaction: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT event_id,aggregate_id,user_id,event_type,payload FROM outbox_events WHERE published_at IS NULL ORDER BY created_at,event_id LIMIT 50 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read pending outbox events: %w", err)
		}
		type row struct {
			id, videoID, userID, eventType string
			payload                        []byte
		}
		batch := make([]row, 0, 50)
		for rows.Next() {
			var item row
			if err := rows.Scan(&item.id, &item.videoID, &item.userID, &item.eventType, &item.payload); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return fmt.Errorf("scan pending outbox event: %w", err)
			}
			batch = append(batch, item)
		}
		rowsErr := rows.Err()
		_ = rows.Close()
		if rowsErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("iterate pending outbox events: %w", rowsErr)
		}
		for _, item := range batch {
			_, err := b.redis.XAdd(ctx, &redis.XAddArgs{
				Stream: StreamName,
				MaxLen: 100000,
				Approx: true,
				Values: map[string]any{
					"event_id": item.id,
					"video_id": item.videoID,
					"user_id":  item.userID,
					"event":    item.eventType,
					"data":     string(item.payload),
				},
			}).Result()
			if err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("publish status event %s to Redis Stream: %w", item.id, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET published_at=NOW() WHERE event_id=$1`, item.id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("mark outbox event %s published: %w", item.id, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit outbox relay transaction: %w", err)
		}
		if len(batch) < 50 {
			return nil
		}
	}
}

func (b *Bus) consume(ctx context.Context) {
	claimTicker := time.NewTicker(30 * time.Second)
	defer claimTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-claimTicker.C:
			messages, _, err := b.redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream: StreamName, Group: b.group, Consumer: b.consumer,
				MinIdle: time.Minute, Start: "0-0", Count: 50,
			}).Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				slog.Error("failed reclaiming pending status events", "error", err)
			} else {
				b.handleMessages(ctx, messages)
			}
		default:
			streams, err := b.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group: b.group, Consumer: b.consumer,
				Streams: []string{StreamName, ">"}, Count: 50, Block: 5 * time.Second,
			}).Result()
			if err != nil && !errors.Is(err, redis.Nil) && !errors.Is(err, context.Canceled) {
				slog.Error("failed reading status stream", "error", err)
				time.Sleep(time.Second)
				continue
			}
			for _, stream := range streams {
				b.handleMessages(ctx, stream.Messages)
			}
		}
	}
}

func (b *Bus) handleMessages(ctx context.Context, messages []redis.XMessage) {
	for _, message := range messages {
		eventType, _ := message.Values["event"].(string)
		userID, _ := message.Values["user_id"].(string)
		payload, _ := message.Values["data"].(string)
		accepted := true
		if eventType == "video_status" && userID != "" && json.Valid([]byte(payload)) {
			var data any
			if err := json.Unmarshal([]byte(payload), &data); err != nil {
				slog.Error("invalid video status event payload", "stream_id", message.ID, "error", err)
			} else {
				eventID, _ := message.Values["event_id"].(string)
				accepted = shared.SendEventToUserByID(userID, eventType, eventID, data)
			}
		}
		if !accepted {
			continue
		}
		if err := b.redis.XAck(ctx, StreamName, b.group, message.ID).Err(); err != nil {
			slog.Error("failed acknowledging status stream event", "stream_id", message.ID, "error", err)
		}
	}
}
