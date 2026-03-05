package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/django1982/ankerctl/internal/model"
)

const (
	defaultHATopicPrefix = "ankerctl"
)

// HomeAssistantLightController forwards light switch commands to printer control.
type HomeAssistantLightController interface {
	SetLight(ctx context.Context, on bool) error
}

// HomeAssistantToken models async MQTT operation completion.
type HomeAssistantToken interface {
	WaitTimeout(timeout time.Duration) bool
	Error() error
}

// HomeAssistantMQTTClient is the external HA broker client contract.
type HomeAssistantMQTTClient interface {
	Connect() HomeAssistantToken
	Disconnect(quiesce uint)
	Publish(topic string, qos byte, retained bool, payload interface{}) HomeAssistantToken
	Subscribe(topic string, qos byte, callback paho.MessageHandler) HomeAssistantToken
	IsConnected() bool
}

type pahoToken struct{ token paho.Token }

func (t pahoToken) WaitTimeout(timeout time.Duration) bool { return t.token.WaitTimeout(timeout) }
func (t pahoToken) Error() error                           { return t.token.Error() }

type pahoClientAdapter struct{ client paho.Client }

func (c *pahoClientAdapter) Connect() HomeAssistantToken {
	return pahoToken{token: c.client.Connect()}
}
func (c *pahoClientAdapter) Disconnect(quiesce uint) { c.client.Disconnect(quiesce) }
func (c *pahoClientAdapter) Publish(topic string, qos byte, retained bool, payload interface{}) HomeAssistantToken {
	return pahoToken{token: c.client.Publish(topic, qos, retained, payload)}
}
func (c *pahoClientAdapter) Subscribe(topic string, qos byte, callback paho.MessageHandler) HomeAssistantToken {
	return pahoToken{token: c.client.Subscribe(topic, qos, callback)}
}
func (c *pahoClientAdapter) IsConnected() bool { return c.client.IsConnected() }

// HomeAssistantService bridges state to an external HA MQTT broker.
type HomeAssistantService struct {
	BaseWorker

	mu sync.RWMutex

	cfg         model.HomeAssistantConfig
	printerSN   string
	printerName string
	nodeID      string
	topicPrefix string
	enabled     bool

	light HomeAssistantLightController

	client            HomeAssistantMQTTClient
	newClient         func(opts *paho.ClientOptions) HomeAssistantMQTTClient
	connectTimeout    time.Duration
	heartbeatInterval time.Duration

	state   map[string]any
	updateQ chan map[string]any
	cmdQ    chan string
}

// NewHomeAssistantService creates HomeAssistantService.
func NewHomeAssistantService(
	cfg model.HomeAssistantConfig,
	printerSN string,
	printerName string,
	light HomeAssistantLightController,
) *HomeAssistantService {
	topicPrefix := strings.TrimSpace(os.Getenv("HA_MQTT_TOPIC_PREFIX"))
	if topicPrefix == "" {
		topicPrefix = defaultHATopicPrefix
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "ankerctl_" + printerSN
	}

	s := &HomeAssistantService{
		BaseWorker:        NewBaseWorker("homeassistant"),
		cfg:               cfg,
		printerSN:         nonEmpty(printerSN, "ankerctl"),
		printerName:       nonEmpty(printerName, "AnkerMake M5"),
		nodeID:            nodeID,
		topicPrefix:       topicPrefix,
		enabled:           cfg.Enabled,
		light:             light,
		newClient:         defaultHANewClient,
		connectTimeout:    10 * time.Second,
		heartbeatInterval: 60 * time.Second,
		state: map[string]any{
			"print_progress":    0,
			"print_status":      "idle",
			"print_filename":    "",
			"print_layer":       "",
			"print_speed":       0,
			"nozzle_temp":       0,
			"nozzle_temp_target": 0,
			"bed_temp":          0,
			"bed_temp_target":   0,
			"time_elapsed":      0,
			"time_remaining":    0,
			"mqtt_connected":    false,
			"pppp_connected":    false,
			"light":             false,
		},
		updateQ: make(chan map[string]any, 64),
		cmdQ:    make(chan string, 16),
	}
	s.BindHooks(s)
	return s
}

// UpdateState merges and publishes new Home Assistant entity state.
func (s *HomeAssistantService) UpdateState(update map[string]any) {
	copyMap := make(map[string]any, len(update))
	for k, v := range update {
		copyMap[k] = v
	}
	select {
	case s.updateQ <- copyMap:
	default:
	}
}

func (s *HomeAssistantService) WorkerInit() {}

func (s *HomeAssistantService) WorkerStart() error {
	s.mu.RLock()
	enabled := s.enabled
	cfg := s.cfg
	s.mu.RUnlock()
	if !enabled {
		return nil
	}

	opts := paho.NewClientOptions()
	host := nonEmpty(cfg.MQTTHost, "localhost")
	port := cfg.MQTTPort
	if port <= 0 {
		port = 1883
	}
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", host, port))
	opts.SetClientID(fmt.Sprintf("ankerctl-%s", s.printerSN))
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetConnectTimeout(s.connectTimeout)
	if cfg.MQTTUsername != "" {
		opts.SetUsername(cfg.MQTTUsername)
		opts.SetPassword(cfg.MQTTPassword)
	}
	opts.SetWill(s.availabilityTopic(), "offline", 1, true)
	opts.SetConnectionLostHandler(func(_ paho.Client, _ error) {
		s.setOnline(false)
	})
	opts.SetOnConnectHandler(func(_ paho.Client) {
		s.onConnected()
	})

	s.mu.Lock()
	s.client = s.newClient(opts)
	client := s.client
	s.mu.Unlock()

	tok := client.Connect()
	if !tok.WaitTimeout(s.connectTimeout) {
		return fmt.Errorf("homeassistant: connect timeout")
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("homeassistant: connect: %w", err)
	}
	return nil
}

func (s *HomeAssistantService) WorkerRun(ctx context.Context) error {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-s.updateQ:
			s.applyUpdate(update)
		case payload := <-s.cmdQ:
			s.handleLightCommand(ctx, payload)
		case <-ticker.C:
			s.publishAvailability("online")
		}
	}
}

func (s *HomeAssistantService) WorkerStop() {
	s.publishAvailability("offline")
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.mu.Unlock()
	if client != nil {
		client.Disconnect(250)
	}
}

func (s *HomeAssistantService) onConnected() {
	s.publishDiscovery()
	s.publishAvailability("online")
	s.setOnline(true)

	topic := fmt.Sprintf("%s/%s/light/set", s.topicPrefix, s.printerSN)
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil {
		return
	}
	tok := client.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		payload := strings.TrimSpace(string(msg.Payload()))
		select {
		case s.cmdQ <- payload:
		default:
		}
	})
	_ = tok.WaitTimeout(5 * time.Second)
}

func (s *HomeAssistantService) handleLightCommand(ctx context.Context, payload string) {
	on := strings.EqualFold(payload, "on")
	if s.light != nil {
		_ = s.light.SetLight(ctx, on)
	}
	s.applyUpdate(map[string]any{"light": on})
}

func (s *HomeAssistantService) applyUpdate(update map[string]any) {
	s.mu.Lock()
	for k, v := range update {
		s.state[k] = v
	}
	payload, _ := json.Marshal(s.state)
	s.mu.Unlock()
	s.publish(s.stateTopic(), payload, true)
}

func (s *HomeAssistantService) publishDiscovery() {
	stateTopic := s.stateTopic()
	availability := []map[string]any{{
		"topic":                 s.availabilityTopic(),
		"payload_available":     "online",
		"payload_not_available": "offline",
	}}
	device := map[string]any{
		"identifiers":  []string{s.nodeID},
		"name":         s.printerName,
		"manufacturer": "AnkerMake",
		"model":        "M5",
		"sw_version":   "ankerctl",
	}

	sensors := []map[string]string{
		{"id": "print_progress", "name": "Print Progress", "unit": "%", "value": "{{ value_json.print_progress | default(0) }}"},
		{"id": "print_status", "name": "Print Status", "value": "{{ value_json.print_status | default('idle') }}"},
		{"id": "print_filename", "name": "Print Filename", "value": "{{ value_json.print_filename | default('') }}"},
		{"id": "print_layer", "name": "Print Layer", "value": "{{ value_json.print_layer | default('') }}"},
		{"id": "print_speed", "name": "Print Speed", "unit": "mm/s", "value": "{{ value_json.print_speed | default(0) }}"},
		{"id": "nozzle_temp", "name": "Nozzle Temperature", "unit": "°C", "value": "{{ value_json.nozzle_temp | default(0) }}"},
		{"id": "bed_temp", "name": "Bed Temperature", "unit": "°C", "value": "{{ value_json.bed_temp | default(0) }}"},
		{"id": "nozzle_temp_target", "name": "Nozzle Target", "unit": "°C", "value": "{{ value_json.nozzle_temp_target | default(0) }}"},
		{"id": "bed_temp_target", "name": "Bed Target", "unit": "°C", "value": "{{ value_json.bed_temp_target | default(0) }}"},
		{"id": "time_elapsed", "name": "Time Elapsed", "unit": "s", "value": "{{ value_json.time_elapsed | default(0) }}"},
		{"id": "time_remaining", "name": "Time Remaining", "unit": "s", "value": "{{ value_json.time_remaining | default(0) }}"},
	}

	for _, sensor := range sensors {
		cfg := map[string]any{
			"name":           sensor["name"],
			"unique_id":      fmt.Sprintf("%s_%s", s.nodeID, sensor["id"]),
			"object_id":      fmt.Sprintf("%s_%s", s.nodeID, sensor["id"]),
			"state_topic":    stateTopic,
			"value_template": sensor["value"],
			"device":         device,
			"availability":   availability,
		}
		if unit := sensor["unit"]; unit != "" {
			cfg["unit_of_measurement"] = unit
		}
		topic := fmt.Sprintf("%s/sensor/%s/%s/config", s.cfg.DiscoveryPrefix, s.nodeID, sensor["id"])
		s.publishJSON(topic, cfg, true)
	}

	binarySensors := []map[string]string{
		{"id": "mqtt_connected", "name": "MQTT Connected", "value": "{{ 'ON' if value_json.mqtt_connected else 'OFF' }}"},
		{"id": "pppp_connected", "name": "PPPP Connected", "value": "{{ 'ON' if value_json.pppp_connected else 'OFF' }}"},
	}
	for _, bs := range binarySensors {
		cfg := map[string]any{
			"name":           bs["name"],
			"unique_id":      fmt.Sprintf("%s_%s", s.nodeID, bs["id"]),
			"object_id":      fmt.Sprintf("%s_%s", s.nodeID, bs["id"]),
			"state_topic":    stateTopic,
			"value_template": bs["value"],
			"device_class":   "connectivity",
			"payload_on":     "ON",
			"payload_off":    "OFF",
			"device":         device,
			"availability":   availability,
		}
		topic := fmt.Sprintf("%s/binary_sensor/%s/%s/config", s.cfg.DiscoveryPrefix, s.nodeID, bs["id"])
		s.publishJSON(topic, cfg, true)
	}

	switchCfg := map[string]any{
		"name":           "Printer Light",
		"unique_id":      fmt.Sprintf("%s_light", s.nodeID),
		"object_id":      fmt.Sprintf("%s_light", s.nodeID),
		"state_topic":    stateTopic,
		"command_topic":  fmt.Sprintf("%s/%s/light/set", s.topicPrefix, s.printerSN),
		"value_template": "{{ 'ON' if value_json.light else 'OFF' }}",
		"payload_on":     "ON",
		"payload_off":    "OFF",
		"device":         device,
		"availability":   availability,
	}
	s.publishJSON(fmt.Sprintf("%s/switch/%s/light/config", s.cfg.DiscoveryPrefix, s.nodeID), switchCfg, true)

	cameraCfg := map[string]any{
		"name":         "Camera",
		"unique_id":    fmt.Sprintf("%s_camera", s.nodeID),
		"object_id":    fmt.Sprintf("%s_camera", s.nodeID),
		"topic":        fmt.Sprintf("%s/%s/camera", s.topicPrefix, s.printerSN),
		"device":       device,
		"availability": availability,
	}
	s.publishJSON(fmt.Sprintf("%s/camera/%s/camera/config", s.cfg.DiscoveryPrefix, s.nodeID), cameraCfg, true)
}

func (s *HomeAssistantService) publishAvailability(value string) {
	s.publish(s.availabilityTopic(), []byte(value), true)
}

func (s *HomeAssistantService) publish(topic string, payload []byte, retained bool) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		return
	}
	tok := client.Publish(topic, 1, retained, payload)
	_ = tok.WaitTimeout(3 * time.Second)
}

func (s *HomeAssistantService) publishJSON(topic string, payload map[string]any, retained bool) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.publish(topic, data, retained)
}

func (s *HomeAssistantService) availabilityTopic() string {
	return fmt.Sprintf("%s/%s/availability", s.topicPrefix, s.printerSN)
}

func (s *HomeAssistantService) stateTopic() string {
	return fmt.Sprintf("%s/%s/state", s.topicPrefix, s.printerSN)
}

func (s *HomeAssistantService) setOnline(v bool) {
	s.mu.Lock()
	s.state["mqtt_connected"] = v
	s.mu.Unlock()
}

func defaultHANewClient(opts *paho.ClientOptions) HomeAssistantMQTTClient {
	return &pahoClientAdapter{client: paho.NewClient(opts)}
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

