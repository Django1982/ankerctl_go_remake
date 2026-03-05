package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

const (
	defaultVideoStallTimeout = 15 * time.Second
)

// VideoProfile describes a camera streaming profile.
type VideoProfile struct {
	ID       string
	Width    int
	Height   int
	Live     bool
	LiveMode int
}

var (
	VideoProfileSD  = VideoProfile{ID: "sd", Width: 848, Height: 480, Live: true, LiveMode: 0}
	VideoProfileHD  = VideoProfile{ID: "hd", Width: 1280, Height: 720, Live: true, LiveMode: 1}
	VideoProfileFHD = VideoProfile{ID: "fhd", Width: 1920, Height: 1080, Live: false, LiveMode: -1}
)

// VideoProfiles holds all supported camera profiles.
var VideoProfiles = map[string]VideoProfile{
	"sd":  VideoProfileSD,
	"hd":  VideoProfileHD,
	"fhd": VideoProfileFHD,
}

// VideoStreamController controls printer live video state.
type VideoStreamController interface {
	StartLive(ctx context.Context, mode int) error
	StopLive(ctx context.Context) error
	SetVideoMode(ctx context.Context, mode int) error
}

// SnapshotLightController controls printer light for snapshot capture.
type SnapshotLightController interface {
	SetLight(ctx context.Context, on bool) error
}

// VideoFrameEvent is emitted for each incoming H.264 frame.
type VideoFrameEvent struct {
	Generation uint64
	Profile    string
	Frame      []byte
	At         time.Time
}

// VideoStallEvent is emitted when no frames arrive within stall timeout.
type VideoStallEvent struct {
	Generation uint64
	SinceLast  time.Duration
}

type ffmpegRunner func(ctx context.Context, args []string) error

// VideoQueue streams H.264 frames, monitors stalls, and supports snapshots.
type VideoQueue struct {
	BaseWorker

	mu sync.RWMutex

	controller      VideoStreamController
	lightController SnapshotLightController
	runFFmpeg       ffmpegRunner

	VideoEnabledField bool
	profileID         string
	generation        uint64
	lastFrameAt       time.Time
	liveStartedAt     time.Time
	frameCh           chan []byte
	stallTimeout      time.Duration
	checkInterval     time.Duration
}

// NewVideoQueue creates a VideoQueue service.
func NewVideoQueue(controller VideoStreamController, light SnapshotLightController) *VideoQueue {
	q := &VideoQueue{
		BaseWorker:        NewBaseWorker("videoqueue"),
		controller:        controller,
		lightController:   light,
		runFFmpeg:         defaultFFmpegRunner,
		profileID:         VideoProfileHD.ID,
		frameCh:           make(chan []byte, 64),
		stallTimeout:      defaultVideoStallTimeout,
		checkInterval:     time.Second,
		VideoEnabledField: false,
	}
	q.BindHooks(q)
	return q
}

// VideoEnabled returns whether live video is enabled.
func (q *VideoQueue) VideoEnabled() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.VideoEnabledField
}

// SetVideoEnabled toggles the live video service and increments generation on enable.
func (q *VideoQueue) SetVideoEnabled(enabled bool) {
	q.mu.Lock()
	if q.VideoEnabledField == enabled {
		q.mu.Unlock()
		return
	}
	q.VideoEnabledField = enabled
	if enabled {
		q.generation++
		q.lastFrameAt = time.Time{}
	}
	q.mu.Unlock()

	if enabled {
		if q.State() == StateStopped {
			q.Start(context.Background())
		}
		return
	}
	if q.State() == StateRunning {
		q.Stop()
	}
}

// Generation returns the current video generation.
func (q *VideoQueue) Generation() uint64 {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.generation
}

// SetProfile selects a video profile. FHD is accepted but not used for live mode.
func (q *VideoQueue) SetProfile(profileID string) error {
	profileID = strings.ToLower(strings.TrimSpace(profileID))
	profile, ok := VideoProfiles[profileID]
	if !ok {
		return fmt.Errorf("videoqueue: unknown profile %q", profileID)
	}

	q.mu.Lock()
	q.profileID = profile.ID
	controller := q.controller
	q.mu.Unlock()

	if !profile.Live || controller == nil {
		return nil
	}
	if err := controller.SetVideoMode(context.Background(), profile.LiveMode); err != nil {
		return fmt.Errorf("videoqueue: set video mode: %w", err)
	}
	return nil
}

// FeedFrame queues one H.264 frame from PPPP/video pipeline.
func (q *VideoQueue) FeedFrame(frame []byte) {
	copyFrame := append([]byte(nil), frame...)
	select {
	case q.frameCh <- copyFrame:
	default:
		// Drop oldest frame to keep stream live under backpressure.
		select {
		case <-q.frameCh:
		default:
		}
		select {
		case q.frameCh <- copyFrame:
		default:
		}
	}
}

// LastFrameAt returns timestamp of the last observed frame.
func (q *VideoQueue) LastFrameAt() time.Time {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lastFrameAt
}

// CaptureSnapshot grabs one JPEG snapshot from the local /video endpoint.
// It turns on printer light via MQTT/light-controller before capture.
func (q *VideoQueue) CaptureSnapshot(ctx context.Context, outputPath string) error {
	if q.lightController != nil {
		_ = q.lightController.SetLight(ctx, true)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("videoqueue: create snapshot dir: %w", err)
	}

	url := videoLoopbackURL()
	args := []string{"-loglevel", "error", "-nostdin", "-y", "-f", "h264", "-i", url, "-frames:v", "1", outputPath}
	if err := q.runFFmpeg(ctx, args); err == nil {
		return nil
	}

	fallback := []string{"-loglevel", "error", "-nostdin", "-y", "-i", url, "-frames:v", "1", outputPath}
	if err := q.runFFmpeg(ctx, fallback); err != nil {
		return fmt.Errorf("videoqueue: snapshot ffmpeg failed: %w", err)
	}
	return nil
}

func (q *VideoQueue) WorkerInit() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lastFrameAt = time.Time{}
	q.liveStartedAt = time.Time{}
}

func (q *VideoQueue) WorkerStart() error {
	q.mu.RLock()
	enabled := q.VideoEnabledField
	profile := VideoProfiles[q.profileID]
	controller := q.controller
	q.mu.RUnlock()
	if !enabled || controller == nil {
		return nil
	}

	type videoHandlerRegistrar interface {
		RegisterVideoHandler(func(protocol.VideoFrame))
	}
	if reg, ok := controller.(videoHandlerRegistrar); ok {
		reg.RegisterVideoHandler(func(vf protocol.VideoFrame) {
			if vf.Cmd == protocol.P2PCmdVideoFrame {
				q.FeedFrame(vf.Data)
			}
		})
	}

	if profile.Live {
		if err := controller.StartLive(context.Background(), profile.LiveMode); err != nil {
			return fmt.Errorf("videoqueue: start live: %w", err)
		}
	}

	now := time.Now()
	q.mu.Lock()
	q.liveStartedAt = now
	q.lastFrameAt = now
	q.mu.Unlock()
	return nil
}

func (q *VideoQueue) WorkerRun(ctx context.Context) error {
	q.mu.RLock()
	enabled := q.VideoEnabledField
	stallTimeout := q.stallTimeout
	checkInterval := q.checkInterval
	q.mu.RUnlock()

	if !enabled {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case frame := <-q.frameCh:
			now := time.Now()
			q.mu.Lock()
			q.lastFrameAt = now
			generation := q.generation
			profileID := q.profileID
			q.mu.Unlock()
			q.Notify(VideoFrameEvent{Generation: generation, Profile: profileID, Frame: frame, At: now})
		case <-ticker.C:
			now := time.Now()
			q.mu.RLock()
			last := q.lastFrameAt
			generation := q.generation
			q.mu.RUnlock()
			if now.Sub(last) > stallTimeout {
				q.Notify(VideoStallEvent{Generation: generation, SinceLast: now.Sub(last)})
				return ErrServiceRestartSignal
			}
		}
	}
}

func (q *VideoQueue) WorkerStop() {
	q.mu.RLock()
	controller := q.controller
	q.mu.RUnlock()
	if controller != nil {
		_ = controller.StopLive(context.Background())
	}
}

func defaultFFmpegRunner(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func videoLoopbackURL() string {
	host := strings.TrimSpace(os.Getenv("ANKERCTL_HOST"))
	if host == "" {
		host = strings.TrimSpace(os.Getenv("FLASK_HOST"))
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	port := strings.TrimSpace(os.Getenv("ANKERCTL_PORT"))
	if port == "" {
		port = strings.TrimSpace(os.Getenv("FLASK_PORT"))
	}
	if port == "" {
		port = "4470"
	}
	if _, err := strconv.Atoi(port); err != nil {
		port = "4470"
	}

	url := fmt.Sprintf("http://%s:%s/video?for_timelapse=1", host, port)
	if apiKey := strings.TrimSpace(os.Getenv("ANKERCTL_API_KEY")); apiKey != "" {
		url += "&apikey=" + apiKey
	}
	return url
}

var errFFmpegUnavailable = errors.New("ffmpeg unavailable")

func ffmpegAvailable() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errFFmpegUnavailable
	}
	return nil
}
