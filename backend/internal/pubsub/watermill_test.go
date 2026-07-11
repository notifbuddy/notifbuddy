package pubsub

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresPubSub_FanoutTwoGroups is an integration test of the whole
// backend: publish once, and every consumer group receives the message with
// its attributes intact. Requires a database:
//
//	docker compose up -d postgres
//	TEST_DATABASE_URL=postgres://xolo:xolo@localhost:5432/xolo?sslmode=disable go test ./internal/pubsub/
func TestPostgresPubSub_FanoutTwoGroups(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Unique topic per run so offsets/messages from earlier runs can't leak in.
	topic := fmt.Sprintf("test.pubsub.fanout.%d", time.Now().UnixNano())
	t.Cleanup(func() {
		table := strings.ReplaceAll(topic, ".", "_")
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS "watermill_`+table+`", "watermill_offsets_`+table+`"`)
	})

	logger := watermill.NopLogger{}
	pub, err := NewPostgresPublisher(pool, logger)
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	defer pub.Close()

	subs := map[string]<-chan *message.Message{}
	for _, group := range []string{"group-a", "group-b"} {
		sub, err := NewPostgresSubscriber(pool, group, 50*time.Millisecond, logger)
		if err != nil {
			t.Fatalf("subscriber %s: %v", group, err)
		}
		defer sub.Close()
		if group == "group-a" {
			if err := InitializeTopics(sub, topic); err != nil {
				t.Fatalf("initialize: %v", err)
			}
		}
		ch, err := sub.Subscribe(ctx, topic)
		if err != nil {
			t.Fatalf("subscribe %s: %v", group, err)
		}
		subs[group] = ch
	}

	want := `{"hello":"fanout"}`
	if err := pub.Publish(ctx, Message{
		Topic:      topic,
		Payload:    []byte(want),
		Attributes: map[string]string{"org_id": "org1", "event_type": "test"},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Every group independently receives the single published message.
	for group, ch := range subs {
		select {
		case m := <-ch:
			if got := string(m.Payload); got != want {
				t.Errorf("%s: payload = %q, want %q", group, got, want)
			}
			if got := m.Metadata.Get("org_id"); got != "org1" {
				t.Errorf("%s: metadata org_id = %q, want %q", group, got, "org1")
			}
			if got := m.Metadata.Get("event_type"); got != "test" {
				t.Errorf("%s: metadata event_type = %q, want %q", group, got, "test")
			}
			m.Ack()
		case <-ctx.Done():
			t.Fatalf("%s: timed out waiting for message", group)
		}
	}
}
