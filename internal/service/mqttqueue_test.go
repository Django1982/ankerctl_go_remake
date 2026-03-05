package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/db"
	mqttclient "github.com/django1982/ankerctl/internal/mqtt/client"
	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

type fakeMQTTClient struct {
	mu       sync.Mutex
	queue    [][]mqttclient.DecodedMessage
	commands []map[string]any
	queries  []map[string]any
}

func (f *fakeMQTTClient) Fetch() []mqttclient.DecodedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return nil
	}
	out := f.queue[0]
	f.queue = f.queue[1:]
	return out
}

func (f *fakeMQTTClient) Command(_ context.Context, msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, msg.(map[string]any))
	return nil
}

func (f *fakeMQTTClient) Query(_ context.Context, msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, msg.(map[string]any))
	return nil
}

func (f *fakeMQTTClient) Disconnect(_ time.Duration) {}

type captureSink struct {
	mu   sync.Mutex
	got  []any
	last any
}

func (c *captureSink) Notify(data any) {
	c.mu.Lock()
	c.got = append(c.got, data)
	c.last = data
	c.mu.Unlock()
}

func TestMqttQueue_StateMachineDeferredHistoryStart(t *testing.T) {
	historyDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open history db: %v", err)
	}
	defer historyDB.Close()

	client := &fakeMQTTClient{
		queue: [][]mqttclient.DecodedMessage{
			{{
				Objects: []map[string]any{
					{"commandType": int(protocol.MqttCmdEventNotify), "value": 1},
				},
			}},
			{{
				Objects: []map[string]any{
					{"commandType": int(protocol.MqttCmdModelDLProcess), "filePath": "/tmp/benchy.gcode"},
				},
			}},
		},
	}

	ha := &captureSink{}
	timelapse := &captureSink{}
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		history:            historyDB,
		clientFactory:      func(context.Context) (mqttClient, error) { return client, nil },
		queryInterval:      time.Hour,
		pollInterval:       5 * time.Millisecond,
		currentPrinterStat: -1,
		homeAssistant:      ha,
		timelapse:          timelapse,
	}
	q.BindHooks(q)
	if err := q.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer q.WorkerStop()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := q.WorkerRun(ctx); err != nil {
		t.Fatalf("WorkerRun: %v", err)
	}

	rows, err := historyDB.GetHistory(10, 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("history rows = %d, want 1", len(rows))
	}
	if rows[0].Filename != "benchy.gcode" {
		t.Fatalf("history filename = %q, want benchy.gcode", rows[0].Filename)
	}
	if rows[0].Status != "started" {
		t.Fatalf("history status = %q, want started", rows[0].Status)
	}
	if len(ha.got) == 0 || len(timelapse.got) == 0 {
		t.Fatalf("expected forwarded events to HA/timelapse, got ha=%d timelapse=%d", len(ha.got), len(timelapse.got))
	}
}

func TestNormalizeProgressFromMQTTScale(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{in: -1, want: 0},
		{in: 0, want: 0},
		{in: 42, want: 42},
		{in: 9999, want: 99},
		{in: 10000, want: 100},
		{in: 5000, want: 50},
		{in: 12000, want: 100},
	}
	for _, tc := range tests {
		if got := normalizeProgress(tc.in); got != tc.want {
			t.Fatalf("normalizeProgress(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
