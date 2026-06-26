package pubsub

import (
	"context"
	"errors"
	"fmt"
)

// SNSClient is the minimal surface an AWS SNS client must provide. Implement it
// against aws-sdk-go-v2 (sns.Client.Publish) and pass it to NewSNSPublisher.
// Keeping it an interface means this package — and everything that depends on
// pubsub.Publisher — never imports the AWS SDK; the dependency lives only at the
// wiring site that constructs the real client.
type SNSClient interface {
	// Publish sends payload to topicARN with optional string attributes, and
	// returns the provider message id.
	Publish(ctx context.Context, topicARN string, payload []byte, attributes map[string]string) (messageID string, err error)
}

// TopicARNResolver maps a logical topic ("integrations.github.webhook_event") to
// the SNS topic ARN it should publish to. Returning ok=false means the topic is
// not mapped and Publish will error.
type TopicARNResolver func(topic string) (arn string, ok bool)

// snsPublisher publishes to AWS SNS. It is constructed with a client and a
// resolver so the same publisher can route multiple logical topics to distinct
// SNS topics without leaking ARNs into callers.
type snsPublisher struct {
	client SNSClient
	resolt TopicARNResolver
}

// NewSNSPublisher builds an SNS-backed Publisher. Wire a concrete SNSClient
// (aws-sdk-go-v2) and a resolver mapping logical topics to ARNs.
func NewSNSPublisher(client SNSClient, resolver TopicARNResolver) (Publisher, error) {
	if client == nil {
		return nil, errors.New("pubsub: SNS client is nil")
	}
	if resolver == nil {
		return nil, errors.New("pubsub: SNS topic resolver is nil")
	}
	return &snsPublisher{client: client, resolt: resolver}, nil
}

func (p *snsPublisher) Publish(ctx context.Context, msg Message) error {
	arn, ok := p.resolt(msg.Topic)
	if !ok {
		return fmt.Errorf("pubsub: no SNS topic ARN mapped for %q", msg.Topic)
	}
	if _, err := p.client.Publish(ctx, arn, msg.Payload, msg.Attributes); err != nil {
		return fmt.Errorf("pubsub: sns publish: %w", err)
	}
	return nil
}
