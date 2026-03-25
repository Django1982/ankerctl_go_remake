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
	videoFPSWindow           = 5 * time.Second
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

// PPPPLifecycleController provides PPPP service lifecycle access needed by
// VideoQueue to ensure the PPPP connection is established before issuing live
// commands. Implemented by PPPPService.
type PPPPLifecycleController interface {
	// Start requests the PPPP service to start. Safe to call when already running.
	Start(ctx context.Context)
	// IsConnected reports whether the PPPP handshake has completed.
	IsConnected() bool
	// State returns the current worker run-state.
	State() RunState
}

// PPPPVideoRegistrar allows VideoQueue to register a frame handler directly
// with the PPPP service. Implemented by PPPPService.
type PPPPVideoRegistrar interface {
	RegisterVideoHandler(fn func(protocol.VideoFrame))
}

// Compile-time assertion: PPPPService must satisfy both PPPP lifecycle interfaces.
// This is placed here (alongside the interfaces) so any breakage is caught at build time.
var (
	_ PPPPLifecycleController = (*PPPPService)(nil)
	_ PPPPVideoRegistrar      = (*PPPPService)(nil)
)

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

// VideoRuntimeStats is a lightweight runtime snapshot for debug mode.
type VideoRuntimeStats struct {
	Enabled           bool    `json:"enabled"`
	Generation        uint64  `json:"generation"`
	Profile           string  `json:"profile"`
	FramesTotal       uint64  `json:"frames_total"`
	InputDropped      uint64  `json:"input_dropped"`
	FPS5s             float64 `json:"fps_5s"`
	LastFrameAgeMS    int64   `json:"last_frame_age_ms"`
	LastFrameSize     int     `json:"last_frame_size"`
	FrameQueueLen     int     `json:"frame_queue_len"`
	FrameQueueCap     int     `json:"frame_queue_cap"`
	Consumers         int     `json:"consumers"`
	LiveUptimeMS      int64   `json:"live_uptime_ms"`
	ConnectedForVideo bool    `json:"connected_for_video"`
}

type ffmpegRunner func(ctx context.Context, args []string) error

// VideoQueue streams H.264 frames, monitors stalls, and supports snapshots.
type VideoQueue struct {
	BaseWorker

	mu sync.RWMutex

	controller      VideoStreamController
	lightController SnapshotLightController
	ppppLifecycle   PPPPLifecycleController
	ppppRegistrar   PPPPVideoRegistrar
	runFFmpeg       ffmpegRunner

	VideoEnabledField bool
	profileID         string
	lightState        *bool
	generation        uint64
	lastFrameAt       time.Time
	liveStartedAt     time.Time
	frameSamples      []time.Time
	framesTotal       uint64
	inputDropped      uint64
	lastFrameSize     int
	frameCh           chan []byte
	stallTimeout      time.Duration
	checkInterval     time.Duration
}

// NewVideoQueue creates a VideoQueue service.
//
// If controller additionally implements [PPPPLifecycleController] and/or
// [PPPPVideoRegistrar], those capabilities are extracted and stored in typed
// fields, eliminating the need for runtime type assertions in WorkerStart.
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
	// Eagerly extract typed PPPP interfaces so WorkerStart can use them
	// directly without inline type assertions.
	if lc, ok := controller.(PPPPLifecycleController); ok {
		q.ppppLifecycle = lc
	}
	if reg, ok := controller.(PPPPVideoRegistrar); ok {
		q.ppppRegistrar = reg
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
		switch q.State() {
		case StateStopped:
			q.Start(context.Background())
		default:
			// Recover from a worker that was started while video was still
			// disabled (for example via a prior Borrow on videoqueue).
			q.Restart()
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
	enabled := q.VideoEnabledField
	q.mu.Unlock()

	if !enabled || !profile.Live || controller == nil {
		return nil
	}
	if err := controller.SetVideoMode(context.Background(), profile.LiveMode); err != nil {
		if isNoPPPPClientError(err) {
			return nil
		}
		return fmt.Errorf("videoqueue: set video mode: %w", err)
	}
	return nil
}

// SetVideoMode applies a raw live mode (Python ws/ctrl "quality" compatibility).
func (q *VideoQueue) SetVideoMode(mode int) error {
	q.mu.Lock()
	switch mode {
	case VideoProfileSD.LiveMode:
		q.profileID = VideoProfileSD.ID
	case VideoProfileHD.LiveMode:
		q.profileID = VideoProfileHD.ID
	}
	controller := q.controller
	enabled := q.VideoEnabledField
	q.mu.Unlock()

	if !enabled || controller == nil {
		return nil
	}
	if err := controller.SetVideoMode(context.Background(), mode); err != nil {
		if isNoPPPPClientError(err) {
			return nil
		}
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
		q.mu.Lock()
		q.inputDropped++
		q.mu.Unlock()
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

// SetLight toggles the printer camera light.
func (q *VideoQueue) SetLight(ctx context.Context, on bool) error {
	q.mu.Lock()
	q.lightState = new(bool)
	*q.lightState = on
	lightController := q.lightController
	enabled := q.VideoEnabledField
	q.mu.Unlock()

	if !enabled || lightController == nil {
		return nil
	}
	if err := lightController.SetLight(ctx, on); err != nil {
		if isNoPPPPClientError(err) {
			return nil
		}
		return err
	}
	return nil
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
	q.frameSamples = nil
	q.framesTotal = 0
	q.inputDropped = 0
	q.lastFrameSize = 0
}

func (q *VideoQueue) WorkerStart() error {
	q.mu.RLock()
	enabled := q.VideoEnabledField
	profile := VideoProfiles[q.profileID]
	controller := q.controller
	ppppLC := q.ppppLifecycle
	ppppReg := q.ppppRegistrar
	q.mu.RUnlock()
	if !enabled || controller == nil {
		return nil
	}

	// Ensure PPPP service is started and connected before issuing live/video
	// commands. Uses the typed ppppLifecycle field populated at construction
	// time instead of an inline type assertion.
	if ppppLC != nil {
		ctx := q.LoopContext()
		ppppLC.Start(ctx)
		deadline := time.Now().Add(6 * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for !ppppLC.IsConnected() {
			if time.Now().After(deadline) {
				return errors.New("videoqueue: ppppservice connection timeout")
			}
			if ppppLC.State() == StateStopped {
				return errors.New("videoqueue: ppppservice stopped during startup")
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("videoqueue: context cancelled waiting for pppp: %w", ctx.Err())
			case <-ticker.C:
				// re-check loop condition on next iteration
			}
		}
	}

	if ppppReg != nil {
		ppppReg.RegisterVideoHandler(func(vf protocol.VideoFrame) {
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

	q.mu.RLock()
	lightState := q.lightState
	q.mu.RUnlock()
	if lightState != nil && q.lightController != nil {
		_ = q.lightController.SetLight(context.Background(), *lightState)
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
			q.lastFrameSize = len(frame)
			q.framesTotal++
			q.frameSamples = append(q.frameSamples, now)
			cutoff := now.Add(-videoFPSWindow)
			idx := 0
			for idx < len(q.frameSamples) && q.frameSamples[idx].Before(cutoff) {
				idx++
			}
			if idx > 0 {
				q.frameSamples = q.frameSamples[idx:]
			}
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
			// Python parity: only restart stalled live stream while there are
			// active consumers (ws/video or /video stream taps).
			if q.hasConsumers() && now.Sub(last) > stallTimeout {
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

func isNoPPPPClientError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no client")
}

func (q *VideoQueue) hasConsumers() bool {
	q.handlersMu.RLock()
	defer q.handlersMu.RUnlock()
	return len(q.handlers) > 0
}

// CurrentProfile returns the currently selected profile id.
func (q *VideoQueue) CurrentProfile() string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.profileID
}

// RuntimeStats returns a debug snapshot of video runtime metrics.
func (q *VideoQueue) RuntimeStats() VideoRuntimeStats {
	now := time.Now()
	q.mu.RLock()
	enabled := q.VideoEnabledField
	generation := q.generation
	profile := q.profileID
	framesTotal := q.framesTotal
	inputDropped := q.inputDropped
	lastFrameAt := q.lastFrameAt
	lastFrameSize := q.lastFrameSize
	sampleCount := len(q.frameSamples)
	liveStartedAt := q.liveStartedAt
	queueLen := len(q.frameCh)
	queueCap := cap(q.frameCh)
	q.mu.RUnlock()

	q.handlersMu.RLock()
	consumers := len(q.handlers)
	q.handlersMu.RUnlock()

	lastFrameAgeMS := int64(-1)
	if !lastFrameAt.IsZero() {
		lastFrameAgeMS = now.Sub(lastFrameAt).Milliseconds()
	}

	liveUptimeMS := int64(0)
	if !liveStartedAt.IsZero() {
		liveUptimeMS = now.Sub(liveStartedAt).Milliseconds()
	}

	den := videoFPSWindow.Seconds()
	if !liveStartedAt.IsZero() {
		elapsed := now.Sub(liveStartedAt).Seconds()
		if elapsed > 0 && elapsed < den {
			den = elapsed
		}
	}
	if den < 1 {
		den = 1
	}
	fps := float64(sampleCount) / den

	return VideoRuntimeStats{
		Enabled:        enabled,
		Generation:     generation,
		Profile:        profile,
		FramesTotal:    framesTotal,
		InputDropped:   inputDropped,
		FPS5s:          fps,
		LastFrameAgeMS: lastFrameAgeMS,
		LastFrameSize:  lastFrameSize,
		FrameQueueLen:  queueLen,
		FrameQueueCap:  queueCap,
		Consumers:      consumers,
		LiveUptimeMS:   liveUptimeMS,
		ConnectedForVideo: enabled &&
			consumers > 0 &&
			lastFrameAgeMS >= 0 &&
			lastFrameAgeMS < 2000,
	}
}

var errFFmpegUnavailable = errors.New("ffmpeg unavailable")

func ffmpegAvailable() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errFFmpegUnavailable
	}
	return nil
}
