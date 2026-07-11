package pubsub

import (
	_ "embed"
	"fmt"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// manifest.yaml is the single source of truth for the eventing topology,
// shared with infra (infra reads the same file). Go code binds
// behavior to it: publishers use topic constants that must appear in the
// topics list, and main.go binds a Handler to each subscription name.
//
//go:embed manifest.yaml
var manifestYAML []byte

type manifestSubscription struct {
	Topic string `yaml:"topic"`
	Group string `yaml:"group"`
}

type manifest struct {
	Topics        []string                        `yaml:"topics"`
	Subscriptions map[string]manifestSubscription `yaml:"subscriptions"`
}

var (
	manifestOnce   sync.Once
	manifestParsed manifest
	manifestErr    error
)

func loadManifest() (manifest, error) {
	manifestOnce.Do(func() {
		if err := yaml.Unmarshal(manifestYAML, &manifestParsed); err != nil {
			manifestErr = fmt.Errorf("pubsub: parse manifest.yaml: %w", err)
			return
		}
		topics := map[string]bool{}
		for _, t := range manifestParsed.Topics {
			if topics[t] {
				manifestErr = fmt.Errorf("pubsub: manifest.yaml lists topic %q twice", t)
				return
			}
			topics[t] = true
		}
		for name, sub := range manifestParsed.Subscriptions {
			if !topics[sub.Topic] {
				manifestErr = fmt.Errorf("pubsub: manifest.yaml subscription %q references unknown topic %q", name, sub.Topic)
				return
			}
			if sub.Group == "" {
				manifestErr = fmt.Errorf("pubsub: manifest.yaml subscription %q has no group", name)
				return
			}
		}
	})
	return manifestParsed, manifestErr
}

// Topics returns every topic in the manifest.
func Topics() ([]string, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, err
	}
	return m.Topics, nil
}

// BindSubscriptions joins the manifest's subscriptions with their handlers,
// keyed by subscription name. It errors on any mismatch in either direction —
// a manifest entry with no handler or a handler with no manifest entry — so
// the topology file and the code can't drift apart silently.
func BindSubscriptions(handlers map[string]Handler) ([]Subscription, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, err
	}
	subs := make([]Subscription, 0, len(m.Subscriptions))
	for name, spec := range m.Subscriptions {
		h, ok := handlers[name]
		if !ok {
			return nil, fmt.Errorf("pubsub: manifest subscription %q has no handler bound in main.go", name)
		}
		subs = append(subs, Subscription{Name: name, Group: spec.Group, Topic: spec.Topic, Handle: h})
	}
	for name := range handlers {
		if _, ok := m.Subscriptions[name]; !ok {
			return nil, fmt.Errorf("pubsub: handler %q is not declared in manifest.yaml", name)
		}
	}
	// Deterministic start order (maps iterate randomly).
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })
	return subs, nil
}