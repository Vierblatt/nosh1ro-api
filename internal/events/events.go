package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	Exchange     = "blog.events"
	QueueESSync  = "es.sync"
	QueueCache   = "cache.invalid"
	QueueDLX     = "dead.letter"
	ExchangeDLX  = "dead.letter.exchange"
	RoutingKeyDLX = "dead"
)

type PostEvent struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Client struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func New(url string) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq channel: %w", err)
	}
	if err := declareTopology(ch); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq topology: %w", err)
	}
	return &Client{conn: conn, ch: ch}, nil
}

func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeDLX, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx exchange: %w", err)
	}
	dlxArgs := amqp.Table{
		"x-dead-letter-exchange":    ExchangeDLX,
		"x-dead-letter-routing-key": RoutingKeyDLX,
	}
	if _, err := ch.QueueDeclare(QueueDLX, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx queue: %w", err)
	}
	if err := ch.QueueBind(QueueDLX, RoutingKeyDLX, ExchangeDLX, false, nil); err != nil {
		return fmt.Errorf("bind dlx: %w", err)
	}

	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if _, err := ch.QueueDeclare(QueueESSync, true, false, false, false, dlxArgs); err != nil {
		return fmt.Errorf("declare es sync queue: %w", err)
	}
	if err := ch.QueueBind(QueueESSync, "post.*", Exchange, false, nil); err != nil {
		return fmt.Errorf("bind es sync: %w", err)
	}
	if _, err := ch.QueueDeclare(QueueCache, true, false, false, false, dlxArgs); err != nil {
		return fmt.Errorf("declare cache queue: %w", err)
	}
	if err := ch.QueueBind(QueueCache, "post.*", Exchange, false, nil); err != nil {
		return fmt.Errorf("bind cache: %w", err)
	}
	return nil
}

func (c *Client) Close() error {
	if c.ch != nil {
		c.ch.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, routingKey string, ev PostEvent) error {
	if c == nil {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return c.ch.PublishWithContext(ctx, Exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

type Handler func(ctx context.Context, ev PostEvent) error

func (c *Client) Consume(ctx context.Context, queue string, handler Handler) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("consumer channel: %w", err)
	}
	if err := ch.Qos(1, 0, false); err != nil {
		ch.Close()
		return fmt.Errorf("qos: %w", err)
	}
	deliveries, err := ch.ConsumeWithContext(ctx, queue, "", false, false, false, false, nil)
	if err != nil {
		ch.Close()
		return fmt.Errorf("consume %s: %w", queue, err)
	}

	go func() {
		defer ch.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					return
				}
				var ev PostEvent
				if err := json.Unmarshal(d.Body, &ev); err != nil {
					slog.Error("unmarshal event", "error", err, "queue", queue)
					d.Nack(false, false) // dead-letter
					continue
				}
				if err := handler(ctx, ev); err != nil {
					slog.Error("handle event", "error", err, "queue", queue, "event", ev.Type, "post", ev.ID)
					d.Nack(false, true) // requeue
					continue
				}
				d.Ack(false)
			}
		}
	}()
	return nil
}

func (c *Client) IsReady(ctx context.Context) bool {
	if c == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := c.conn.Channel()
	return err == nil
}
