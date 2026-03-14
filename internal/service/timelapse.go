package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

const (
	defaultTimelapseInterval = 30 * time.Second
	defaultTimelapseMaxVideo = 10
	resumeWindow             = 60 * time.Minute
	maxOrphanAge             = 24 * time.Hour
	inProgressSubdir         = "in_progress"
)

// TimelapseSnapshotter captures one JPEG frame and optionally controls light.
type TimelapseSnapshotter interface {
	CaptureSnapshot(ctx context.Context, outputPath string) error
	SetLight(ctx context.Context, on bool) error
}

type timelapseMeta struct {
	StartedAt time.Time `json:"started_at"`
	PrinterSN string    `json:"printer_sn"`
	Filename  string    `json:"filename"`
}

type captureState struct {
	Dir       string
	Filename  string
	FrameCtr  int
	StartedAt time.Time
	LastShot  time.Time
}

type resumeState struct {
	captureState
	Deadline time.Time
}

type timelapseStartCmd struct{ Filename string }
type timelapseFinishCmd struct{ Final bool }
type timelapseFailCmd struct{}
type timelapseConfigCmd struct {
	Cfg       model.TimelapseConfig
	PrinterSN string
}

// TimelapseService captures snapshots and assembles MP4 videos.
type TimelapseService struct {
	BaseWorker

	mu sync.Mutex

	snapshotter TimelapseSnapshotter
	runFFmpeg   ffmpegRunner

	capturesDir string
	enabled     bool
	interval    time.Duration
	maxVideos   int
	savePersist bool
	lightMode   string // off|on|auto
	printerSN   string

	active *captureState
	resume *resumeState

	cmdCh chan any
}

// NewTimelapseService creates a timelapse service.
func NewTimelapseService(capturesDir string, snapshotter TimelapseSnapshotter) *TimelapseService {
	if capturesDir == "" {
		// Fall back to user config directory instead of hardcoded /captures.
		if cfgDir, err := os.UserConfigDir(); err == nil {
			capturesDir = filepath.Join(cfgDir, "ankerctl", "captures")
		} else {
			capturesDir = filepath.Join(os.Getenv("HOME"), ".config", "ankerctl", "captures")
		}
	}
	s := &TimelapseService{
		BaseWorker:  NewBaseWorker("timelapse"),
		snapshotter: snapshotter,
		runFFmpeg:   defaultFFmpegRunner,
		capturesDir: capturesDir,
		enabled:     false,
		interval:    defaultTimelapseInterval,
		maxVideos:   defaultTimelapseMaxVideo,
		savePersist: true,
		lightMode:   "off",
		cmdCh:       make(chan any, 16),
	}
	s.BindHooks(s)
	return s
}

// Configure updates runtime timelapse settings.
func (s *TimelapseService) Configure(cfg model.TimelapseConfig, printerSN string) {
	mode := "off"
	if cfg.Light != nil {
		switch strings.ToLower(strings.TrimSpace(*cfg.Light)) {
		case "on":
			mode = "on"
		case "auto":
			mode = "auto"
		case "off":
			mode = "off"
		}
	}

	cmd := timelapseConfigCmd{Cfg: cfg, PrinterSN: printerSN}
	select {
	case s.cmdCh <- cmd:
	default:
		s.mu.Lock()
		s.enabled = cfg.Enabled
		s.interval = maxDuration(time.Duration(cfg.Interval)*time.Second, time.Second)
		s.maxVideos = cfg.MaxVideos
		s.savePersist = cfg.SavePersistent
		s.printerSN = printerSN
		s.lightMode = mode
		s.mu.Unlock()
	}
}

// Notify intercepts print_state events from MqttQueue and routes them to
// the timelapse capture lifecycle, then broadcasts to any WS subscribers.
func (s *TimelapseService) Notify(data any) {
	s.BaseWorker.Notify(data)

	payload, ok := data.(map[string]any)
	if !ok || payload["event"] != "print_state" {
		return
	}
	state, ok := asIntIface(payload["state"])
	if !ok {
		return
	}
	switch state {
	case 1: // printing
		filename, _ := payload["filename"].(string)
		s.StartCapture(filename)
	case 2: // paused
		s.FinishCapture(false)
	case 0: // idle
		s.FinishCapture(true)
	case 8: // aborted
		s.FailCapture()
	}
}

func asIntIface(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case int64:
		return int(x), true
	}
	return 0, false
}

// StartCapture begins periodic snapshot capture for a print.
func (s *TimelapseService) StartCapture(filename string) {
	select {
	case s.cmdCh <- timelapseStartCmd{Filename: filename}:
	default:
	}
}

// FinishCapture stops capture; final=true assembles immediately.
func (s *TimelapseService) FinishCapture(final bool) {
	select {
	case s.cmdCh <- timelapseFinishCmd{Final: final}:
	default:
	}
}

// FailCapture assembles partial timelapse (if enough frames) with _partial suffix.
func (s *TimelapseService) FailCapture() {
	select {
	case s.cmdCh <- timelapseFailCmd{}:
	default:
	}
}

func (s *TimelapseService) WorkerInit() {
	_ = os.MkdirAll(s.capturesDir, 0o755)
	_ = os.MkdirAll(s.inProgressBase(), 0o755)
}

func (s *TimelapseService) WorkerStart() error {
	if err := s.recoverOrphans(context.Background()); err != nil {
		return err
	}
	return nil
}

func (s *TimelapseService) WorkerRun(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case raw := <-s.cmdCh:
			s.handleCommand(ctx, raw)
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *TimelapseService) WorkerStop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lightMode == "auto" && s.snapshotter != nil {
		_ = s.snapshotter.SetLight(context.Background(), false)
	}
}

// ListVideos returns available MP4 files, newest first.
func (s *TimelapseService) ListVideos() ([]string, error) {
	entries, err := os.ReadDir(s.capturesDir)
	if err != nil {
		return nil, fmt.Errorf("timelapse: list videos: %w", err)
	}
	videos := make([]string, 0)
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			videos = append(videos, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(videos)))
	return videos, nil
}

// GetVideoPath resolves a timelapse filename to an absolute path.
func (s *TimelapseService) GetVideoPath(filename string) (string, bool) {
	if !strings.HasSuffix(strings.ToLower(filename), ".mp4") {
		return "", false
	}
	clean := filepath.Clean(filename)
	if clean != filename || strings.Contains(clean, "..") || strings.ContainsRune(clean, os.PathSeparator) {
		return "", false
	}
	path := filepath.Join(s.capturesDir, filename)
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", false
	}
	return path, true
}

// DeleteVideo removes a timelapse file.
func (s *TimelapseService) DeleteVideo(filename string) bool {
	path, ok := s.GetVideoPath(filename)
	if !ok {
		return false
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	return true
}

func (s *TimelapseService) handleCommand(ctx context.Context, raw any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd := raw.(type) {
	case timelapseConfigCmd:
		mode := "off"
		if cmd.Cfg.Light != nil {
			switch strings.ToLower(strings.TrimSpace(*cmd.Cfg.Light)) {
			case "on", "auto", "off":
				mode = strings.ToLower(strings.TrimSpace(*cmd.Cfg.Light))
			}
		}
		s.enabled = cmd.Cfg.Enabled
		s.interval = maxDuration(time.Duration(cmd.Cfg.Interval)*time.Second, time.Second)
		s.maxVideos = cmd.Cfg.MaxVideos
		s.savePersist = cmd.Cfg.SavePersistent
		s.lightMode = mode
		s.printerSN = cmd.PrinterSN

	case timelapseStartCmd:
		s.startCaptureLocked(ctx, cmd.Filename)
	case timelapseFinishCmd:
		s.finishCaptureLocked(ctx, cmd.Final)
	case timelapseFailCmd:
		s.failCaptureLocked(ctx)
	}
}

func (s *TimelapseService) tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.resume != nil && time.Now().After(s.resume.Deadline) {
		resume := s.resume
		s.resume = nil
		s.finalizeCaptureLocked(ctx, &resume.captureState, "")
	}

	if !s.enabled || s.active == nil {
		return
	}
	if time.Since(s.active.LastShot) < s.interval {
		return
	}
	if err := s.captureFrameLocked(ctx); err != nil {
		s.Notify(map[string]any{"type": "timelapse.snapshot_error", "error": err.Error()})
	}
}

func (s *TimelapseService) startCaptureLocked(ctx context.Context, filename string) {
	if !s.enabled || filename == "" {
		return
	}
	low := strings.ToLower(strings.TrimSpace(filename))
	if low == "unknown" || low == "unknown.gcode" {
		return
	}
	if ffmpegAvailable() != nil {
		return
	}

	if s.resume != nil && s.resume.Filename == filename && time.Now().Before(s.resume.Deadline) {
		resumed := s.resume.captureState
		now := time.Now()
		resumed.LastShot = now.Add(-s.interval)
		s.active = &resumed
		s.resume = nil
		if s.lightMode == "on" || s.lightMode == "auto" {
			s.setLightLocked(ctx, true)
		}
		return
	}

	if s.resume != nil {
		s.cleanupDirLocked(s.resume.Dir)
		s.resume = nil
	}

	ts := time.Now().Format("20060102_150405")
	dir := filepath.Join(s.inProgressBase(), sanitizeFilePart(filename)+"_"+ts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	now := time.Now()
	st := captureState{Dir: dir, Filename: filename, FrameCtr: 0, StartedAt: now, LastShot: now.Add(-s.interval)}
	s.active = &st
	s.writeMetaLocked(st)
	if s.lightMode == "on" || s.lightMode == "auto" {
		s.setLightLocked(ctx, true)
	}
}

func (s *TimelapseService) finishCaptureLocked(ctx context.Context, final bool) {
	if !s.enabled || s.active == nil {
		return
	}
	current := *s.active
	s.active = nil
	if s.lightMode == "auto" {
		s.setLightLocked(ctx, false)
	}

	if final {
		s.resume = nil
		s.finalizeCaptureLocked(ctx, &current, "")
		return
	}
	s.resume = &resumeState{
		captureState: current,
		Deadline:     time.Now().Add(resumeWindow),
	}
}

func (s *TimelapseService) failCaptureLocked(ctx context.Context) {
	if !s.enabled {
		return
	}
	if s.lightMode == "auto" {
		s.setLightLocked(ctx, false)
	}
	if s.active == nil {
		return
	}
	current := *s.active
	s.active = nil
	s.resume = nil
	s.finalizeCaptureLocked(ctx, &current, "_partial")
}

func (s *TimelapseService) captureFrameLocked(ctx context.Context) error {
	if s.active == nil || s.snapshotter == nil {
		return nil
	}
	framePath := filepath.Join(s.active.Dir, fmt.Sprintf("frame_%05d.jpg", s.active.FrameCtr))
	if err := s.snapshotter.CaptureSnapshot(ctx, framePath); err != nil {
		return fmt.Errorf("timelapse: capture snapshot: %w", err)
	}
	s.active.FrameCtr++
	s.active.LastShot = time.Now()
	s.writeMetaLocked(*s.active)
	s.Notify(map[string]any{"type": "timelapse.frame", "filename": s.active.Filename, "frames": s.active.FrameCtr})
	return nil
}

func (s *TimelapseService) finalizeCaptureLocked(ctx context.Context, cap *captureState, suffix string) {
	if cap == nil {
		return
	}
	defer s.cleanupDirLocked(cap.Dir)

	if cap.FrameCtr < 2 || !s.savePersist {
		return
	}

	outName := sanitizeFilePart(cap.Filename) + "_" + time.Now().Format("20060102_150405") + suffix + ".mp4"
	outPath := filepath.Join(s.capturesDir, outName)
	inputPattern := filepath.Join(cap.Dir, "frame_%05d.jpg")

	// Calculate fps to produce ~30s videos: ceil(frame_count / 30), capped [1,30].
	// This matches the Python formula: max(1, min(30, math.ceil(frame_count / 30)))
	fps := (cap.FrameCtr + 29) / 30 // integer ceiling division
	if fps < 1 {
		fps = 1
	}
	if fps > 30 {
		fps = 30
	}

	args := []string{
		"-loglevel", "error", "-nostdin", "-y",
		"-framerate", strconv.Itoa(fps),
		"-i", inputPattern,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
		outPath,
	}
	if err := s.runFFmpeg(ctx, args); err != nil {
		s.Notify(map[string]any{"type": "timelapse.assemble_error", "error": err.Error()})
		return
	}
	s.pruneVideosLocked()
	s.Notify(map[string]any{"type": "timelapse.assembled", "output": outName, "frames": cap.FrameCtr})
}

func (s *TimelapseService) recoverOrphans(ctx context.Context) error {
	base := s.inProgressBase()
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("timelapse: scan in_progress: %w", err)
	}

	type orphan struct {
		state captureState
		age   time.Duration
	}

	orphans := make([]orphan, 0)
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		frames, filename := scanFrameDir(dir)
		if frames == 0 {
			_ = os.RemoveAll(dir)
			continue
		}
		info, statErr := os.Stat(dir)
		if statErr != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		orphans = append(orphans, orphan{state: captureState{Dir: dir, Filename: filename, FrameCtr: frames}, age: age})
	}

	sort.Slice(orphans, func(i, j int) bool { return orphans[i].age < orphans[j].age })

	for idx, orphan := range orphans {
		if orphan.age > maxOrphanAge {
			_ = os.RemoveAll(orphan.state.Dir)
			continue
		}
		if idx == 0 && orphan.age <= resumeWindow {
			s.resume = &resumeState{captureState: orphan.state, Deadline: time.Now().Add(resumeWindow - orphan.age)}
			continue
		}
		s.finalizeCaptureLocked(ctx, &orphan.state, "_recovered")
	}
	return nil
}

func (s *TimelapseService) writeMetaLocked(cap captureState) {
	meta := timelapseMeta{StartedAt: cap.StartedAt, PrinterSN: s.printerSN, Filename: cap.Filename}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cap.Dir, ".meta"), append(data, '\n'), 0o644)
}

func (s *TimelapseService) pruneVideosLocked() {
	if s.maxVideos <= 0 {
		return
	}
	entries, err := os.ReadDir(s.capturesDir)
	if err != nil {
		return
	}
	type item struct {
		name string
		mt   time.Time
	}
	items := make([]item, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{name: e.Name(), mt: st.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mt.Before(items[j].mt) })
	for len(items) > s.maxVideos {
		oldest := items[0]
		items = items[1:]
		_ = os.Remove(filepath.Join(s.capturesDir, oldest.name))
	}
}

func (s *TimelapseService) cleanupDirLocked(dir string) {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

func (s *TimelapseService) setLightLocked(ctx context.Context, on bool) {
	if s.snapshotter != nil {
		_ = s.snapshotter.SetLight(ctx, on)
	}
}

func (s *TimelapseService) inProgressBase() string {
	return filepath.Join(s.capturesDir, inProgressSubdir)
}

func scanFrameDir(dir string) (int, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, "unknown"
	}
	count := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "frame_") && strings.HasSuffix(strings.ToLower(name), ".jpg") {
			count++
		}
	}
	filename := "unknown"
	metaPath := filepath.Join(dir, ".meta")
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta timelapseMeta
		if json.Unmarshal(data, &meta) == nil && meta.Filename != "" {
			filename = meta.Filename
		}
	}
	return count, filename
}

func sanitizeFilePart(in string) string {
	if in == "" {
		return "print"
	}
	var b strings.Builder
	for _, r := range in {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "print"
	}
	return b.String()
}

func maxDuration(a, b time.Duration) time.Duration {
	if a < b {
		return b
	}
	return a
}

