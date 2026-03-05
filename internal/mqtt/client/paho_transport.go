package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// PahoTransport implements Transport using the Eclipse Paho MQTT library.
// It provides TLS-enabled connections to Anker's cloud MQTT broker (port 8789).
//
// Usage:
//
//	tr, err := NewPahoTransport(PahoConfig{
//	    Broker:   "mqtt.ankermake.com",
//	    Port:     8789,
//	    Username: "user@example.com",
//	    Password: "token",
//	    ClientID: "ankerctl-" + uuid.NewString(),
//	})
//	if err != nil { ... }
//	client, err := New(printerSN, mqttKey, Config{
//	    Broker:    "mqtt.ankermake.com",
//	    Transport: tr,
//	})
type PahoTransport struct {
	client paho.Client
	cfg    PahoConfig
}

// PahoConfig holds parameters for PahoTransport.
type PahoConfig struct {
	// Broker is the hostname or IP of the MQTT broker (no scheme, no port).
	Broker string
	// Port defaults to 8789 when zero.
	Port int
	// ClientID is the MQTT client identifier. Must be unique per connection.
	ClientID string
	// Username and Password for broker authentication.
	Username string
	Password string
	// TLSConfig overrides the default TLS configuration.
	// When nil a permissive TLS config (InsecureSkipVerify) is used,
	// mirroring the Python behaviour (verify=False by default in mqttapi.py).
	TLSConfig *tls.Config
	// ConnectTimeout defaults to 60 s when zero.
	ConnectTimeout time.Duration
	// KeepAlive defaults to 30 s when zero.
	KeepAlive time.Duration
}

// NewPahoTransport builds a PahoTransport but does not yet connect.
func NewPahoTransport(cfg PahoConfig) (*PahoTransport, error) {
	if cfg.Broker == "" {
		return nil, fmt.Errorf("paho transport: broker is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("paho transport: client ID is required")
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
	tlsCfg := cfg.TLSConfig
	if tlsCfg == nil {
		// Python default: tls_insecure_set(True) — skip certificate verification.
		tlsCfg = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // intentional: matches Python behaviour
	}

	opts := paho.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s:%d", cfg.Broker, cfg.Port))
	opts.SetClientID(cfg.ClientID)
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetTLSConfig(tlsCfg)
	opts.SetConnectTimeout(cfg.ConnectTimeout)
	opts.SetKeepAlive(cfg.KeepAlive)
	opts.SetAutoReconnect(false)
	opts.SetCleanSession(true)

	return &PahoTransport{
		client: paho.NewClient(opts),
		cfg:    cfg,
	}, nil
}

// Connect establishes the MQTT connection to the broker.
// It blocks until the connection is established or the context is cancelled.
func (p *PahoTransport) Connect(ctx context.Context) error {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		token := p.client.Connect()
		token.Wait()
		ch <- result{token.Error()}
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("paho transport: connect cancelled: %w", ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("paho transport: connect: %w", r.err)
		}
		return nil
	}
}

// Disconnect gracefully shuts down the connection.
func (p *PahoTransport) Disconnect(quiesce time.Duration) {
	p.client.Disconnect(uint(quiesce.Milliseconds()))
}

// Publish sends a raw payload to the given topic at QoS 1.
func (p *PahoTransport) Publish(ctx context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, 1, false, payload)
	select {
	case <-ctx.Done():
		return fmt.Errorf("paho transport: publish cancelled: %w", ctx.Err())
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("paho transport: publish %s: %w", topic, err)
		}
		return nil
	}
}

// Subscribe registers a handler for incoming messages on the topic.
func (p *PahoTransport) Subscribe(ctx context.Context, topic string, handler MessageHandler) error {
	token := p.client.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		handler(msg.Topic(), msg.Payload())
	})
	select {
	case <-ctx.Done():
		return fmt.Errorf("paho transport: subscribe cancelled: %w", ctx.Err())
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("paho transport: subscribe %s: %w", topic, err)
		}
		return nil
	}
}
