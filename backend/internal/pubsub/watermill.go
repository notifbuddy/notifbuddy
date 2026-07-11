package pubsub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	wsql "github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The Postgres backend stores messages in one table per topic and tracks a
// per-(topic, consumer group) offset, so every group receives every message —
// that offset fanout is the whole reason this backend exists. Delivery is
// polling-based (no LISTEN/NOTIFY), which works on Neon's pooled endpoints and
// bare-metal Postgres alike.
//
// The schema and offsets adapters below are the single source of truth for
// table naming. Publishers, transactional publishers, and subscribers must all
// use these exact adapters: messages are addressed by generated table name, so
// changing the naming scheme orphans everything already stored.

// tableName maps a dotted topic to a Postgres-friendly identifier. The
// returned name must be pre-quoted — the adapters splice it into DDL/queries
// verbatim.
func tableName(prefix, topic string) string {
	return `"` + prefix + strings.ReplaceAll(topic, ".", "_") + `"`
}

// schemaAdapter returns the messages-table adapter shared by every publisher
// and subscriber.
func schemaAdapter() wsql.DefaultPostgreSQLSchema {
	return wsql.DefaultPostgreSQLSchema{
		GenerateMessagesTableName: func(topic string) string {
			return tableName("watermill_", topic)
		},
	}
}

// offsetsAdapter returns the offsets-table adapter shared by every subscriber.
func offsetsAdapter() wsql.DefaultPostgreSQLOffsetsAdapter {
	return wsql.DefaultPostgreSQLOffsetsAdapter{
		GenerateMessagesOffsetsTableName: func(topic string) string {
			return tableName("watermill_offsets_", topic)
		},
	}
}

// PostgresPublisher implements Publisher over the shared Postgres pool. A
// publish is a single INSERT into the topic's messages table.
type PostgresPublisher struct {
	pub message.Publisher
}

// NewPostgresPublisher builds the durable publisher. Schema is NOT auto-created
// on publish; call InitializeTopics at startup so DDL never runs mid-request.
func NewPostgresPublisher(pool *pgxpool.Pool, logger watermill.LoggerAdapter) (*PostgresPublisher, error) {
	pub, err := wsql.NewPublisher(
		wsql.BeginnerFromPgx(pool),
		wsql.PublisherConfig{
			SchemaAdapter:        schemaAdapter(),
			AutoInitializeSchema: false,
		},
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("pubsub: build postgres publisher: %w", err)
	}
	return &PostgresPublisher{pub: pub}, nil
}

// Publish stores the message durably. Attributes map to watermill metadata
// (both are string maps; the round trip through the metadata JSON column is
// lossless).
func (p *PostgresPublisher) Publish(ctx context.Context, msg Message) error {
	m := message.NewMessage(watermill.NewUUID(), msg.Payload)
	for k, v := range msg.Attributes {
		m.Metadata.Set(k, v)
	}
	m.SetContext(ctx)
	return p.pub.Publish(msg.Topic, m)
}

// Close releases the publisher.
func (p *PostgresPublisher) Close() error { return p.pub.Close() }

// NewPostgresSubscriber builds a polling subscriber for one consumer group.
// Distinct groups each receive every message of the topics they subscribe to;
// handlers within a group share the group's offset.
func NewPostgresSubscriber(pool *pgxpool.Pool, group string, pollInterval time.Duration, logger watermill.LoggerAdapter) (*wsql.Subscriber, error) {
	sub, err := wsql.NewSubscriber(
		wsql.BeginnerFromPgx(pool),
		wsql.SubscriberConfig{
			ConsumerGroup:  group,
			PollInterval:   pollInterval,
			SchemaAdapter:  schemaAdapter(),
			OffsetsAdapter: offsetsAdapter(),
		},
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("pubsub: build subscriber (group %s): %w", group, err)
	}
	return sub, nil
}

// InitializeTopics creates the messages + offsets tables for every topic.
// The DDL is CREATE TABLE IF NOT EXISTS guarded by an advisory lock, so this
// is idempotent and safe to run on every startup, in the same spirit as
// store.Migrate. Run it once before the router starts and publishers are used.
func InitializeTopics(sub *wsql.Subscriber, topics ...string) error {
	for _, topic := range topics {
		if err := sub.SubscribeInitialize(topic); err != nil {
			return fmt.Errorf("pubsub: initialize topic %s: %w", topic, err)
		}
	}
	return nil
}

// watermillHandler adapts a provider-neutral Handler to a watermill handler.
// The returned error nacks the message, which is then redelivered after the
// subscriber's resend interval.
func watermillHandler(topic string, fn Handler) message.NoPublishHandlerFunc {
	return func(m *message.Message) error {
		return fn(m.Context(), Message{
			Topic:      topic,
			Payload:    m.Payload,
			Attributes: map[string]string(m.Metadata),
		})
	}
}
