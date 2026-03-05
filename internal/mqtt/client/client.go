package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

const (
	// DefaultBrokerPort is the Anker cloud MQTT broker port.
	DefaultBrokerPort = 8789
)

// MessageHandler handles one raw MQTT payload from a subscribed topic.
type MessageHandler func(topic string, payload []byte)

// Transport abstracts the underlying MQTT library (for example paho).
type Transport interface {
	Connect(ctx context.Context) error
	Disconnect(quiesce time.Duration)
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(ctx context.Context, topic string, handler MessageHandler) error
}

// Config defines MQTT client connection parameters.
type Config struct {
	Broker         string
	Port           int
	ClientID       string
	Username       string
	Password       string
	GUID           string
	ConnectTimeout time.Duration
	KeepAlive      time.Duration
	Logger         *slog.Logger
	Transport      Transport
}

// DecodedMessage contains one decoded MQTT packet plus parsed JSON payload objects.
type DecodedMessage struct {
	Topic   string
	Packet  *protocol.Packet
	Objects []map[string]any
}

// Client is an MQTT API wrapper for Anker printer traffic.
type Client struct {
	transport Transport

	printerSN string
	key       []byte
	guid      string
	log       *slog.Logger

	mu    sync.Mutex
	queue []DecodedMessage
}

// New creates a new MQTT client wrapper.
func New(printerSN string, key []byte, cfg Config) (*Client, error) {
	if printerSN == "" {
		return nil, errors.New("mqtt client: printer serial is required")
	}
	if len(key) == 0 {
		return nil, errors.New("mqtt client: mqtt key is required")
	}
	if cfg.Broker == "" {
		return nil, errors.New("mqtt client: broker is required")
	}
	if cfg.Transport == nil {
		return nil, errors.New("mqtt client: transport is required")
	}

	if cfg.Port == 0 {
		cfg.Port = DefaultBrokerPort
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 60 * time.Second
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.GUID == "" {
		cfg.GUID = uuid.NewString()
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	c := &Client{
		transport: cfg.Transport,
		printerSN: printerSN,
		key:       append([]byte(nil), key...),
		guid:      cfg.GUID,
		log:       logger.With("component", "mqtt-client", "sn", printerSN),
	}
	return c, nil
}

// Connect establishes the MQTT connection and subscriptions.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	if err := c.subscribeAll(ctx); err != nil {
		return fmt.Errorf("mqtt subscribe: %w", err)
	}
	c.log.Info("mqtt connected and subscribed")
	return nil
}

// Disconnect closes the MQTT connection.
func (c *Client) Disconnect(quiesce time.Duration) {
	c.transport.Disconnect(quiesce)
}

// Command publishes a command payload to /device/maker/{sn}/command.
func (c *Client) Command(ctx context.Context, msg any) error {
	return c.Send(ctx, protocol.TopicCommand(c.printerSN), msg)
}

// Query publishes a query payload to /device/maker/{sn}/query.
func (c *Client) Query(ctx context.Context, msg any) error {
	return c.Send(ctx, protocol.TopicQuery(c.printerSN), msg)
}

// Send publishes JSON payload to a topic.
func (c *Client) Send(ctx context.Context, topic string, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mqtt send: marshal json: %w", err)
	}
	pkt := protocol.NewPacket(c.guid, data)
	return c.SendRaw(ctx, topic, pkt)
}

// SendRaw publishes a pre-built packet.
func (c *Client) SendRaw(ctx context.Context, topic string, pkt protocol.Packet) error {
	payload, err := pkt.MarshalBinary(c.key)
	if err != nil {
		return fmt.Errorf("mqtt send: marshal packet: %w", err)
	}
	if err := c.transport.Publish(ctx, topic, payload); err != nil {
		return fmt.Errorf("mqtt publish %s: %w", topic, err)
	}
	return nil
}

// Fetch returns and clears all decoded messages buffered by callbacks.
func (c *Client) Fetch() []DecodedMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]DecodedMessage(nil), c.queue...)
	c.queue = c.queue[:0]
	return out
}

func (c *Client) subscribeAll(ctx context.Context) error {
	handler := func(topic string, payload []byte) {
		if err := c.handleIncoming(topic, payload); err != nil {
			c.log.Error("mqtt decode failed", "topic", topic, "error", err)
		}
	}

	for _, topic := range []string{
		protocol.TopicNotice(c.printerSN),
		protocol.TopicCommandReply(c.printerSN),
		protocol.TopicQueryReply(c.printerSN),
	} {
		if err := c.transport.Subscribe(ctx, topic, handler); err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
	}
	return nil
}

func (c *Client) handleIncoming(topic string, payload []byte) error {
	pkt, err := protocol.UnmarshalPacket(payload, c.key)
	if err != nil {
		return err
	}

	objs, err := decodePayloadObjects(pkt.Data)
	if err != nil {
		return fmt.Errorf("decode message json: %w", err)
	}

	c.mu.Lock()
	c.queue = append(c.queue, DecodedMessage{
		Topic:   topic,
		Packet:  pkt,
		Objects: objs,
	})
	c.mu.Unlock()
	return nil
}

func decodePayloadObjects(data []byte) ([]map[string]any, error) {
	var single map[string]any
	if err := json.Unmarshal(data, &single); err == nil {
		return []map[string]any{single}, nil
	}

	var list []map[string]any
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}
