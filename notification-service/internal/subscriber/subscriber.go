package subscriber

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

type logLine struct {
	Time    string         `json:"time"`
	Subject string         `json:"subject"`
	Event   map[string]any `json:"event"`
}

var subjects = []string{
	"doctors.created",
	"appointments.created",
	"appointments.status_updated",
}

type Subscriber struct {
	nc   *nats.Conn
	subs []*nats.Subscription
}

// New подключается к NATS с exponential backoff.
func New(url string, maxRetries int) (*Subscriber, error) {
	var nc *nats.Conn
	var err error
	backoff := time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		nc, err = nats.Connect(url, nats.Name("notification-service"))
		if err == nil {
			break
		}
		if attempt == maxRetries {
			return nil, fmt.Errorf("cannot connect to NATS after %d attempts: %w", maxRetries, err)
		}
		log.Printf("NATS attempt %d/%d failed (%v); retry in %s", attempt, maxRetries, err, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	log.Println("Notification Service: NATS connected")
	return &Subscriber{nc: nc}, nil
}

func (s *Subscriber) Subscribe() error {
	for _, subj := range subjects {
		subj := subj
		sub, err := s.nc.Subscribe(subj, func(msg *nats.Msg) {
			s.handle(msg)
		})
		if err != nil {
			return fmt.Errorf("subscribe %q: %w", subj, err)
		}
		s.subs = append(s.subs, sub)
		log.Printf("Notification Service: subscribed to %q", subj)
	}
	return nil
}

func (s *Subscriber) handle(msg *nats.Msg) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Printf("ERROR: deserialize on %q: %v", msg.Subject, err)
		return
	}
	line := logLine{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Subject: msg.Subject,
		Event:   payload,
	}
	out, err := json.Marshal(line)
	if err != nil {
		log.Printf("ERROR: marshal log line: %v", err)
		return
	}
	fmt.Println(string(out))
}

func (s *Subscriber) Drain() {
	for _, sub := range s.subs {
		_ = sub.Drain()
	}
	if err := s.nc.Drain(); err != nil {
		log.Printf("WARN: drain: %v", err)
	}
	log.Println("Notification Service: shutdown complete")
}
