package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/django1982/ankerctl/internal/gcode"
)

const uploadProgressInterval = 250 * time.Millisecond

// UploadInfo describes one GCode transfer request.
type UploadInfo struct {
	Name        string
	UserName    string
	UserID      string
	MachineID   string
	Size        int64
	LayerCount  int
	StartPrint  bool
	RateLimitMB int
}

// PPPPFileUploader uploads GCode bytes via PPPP AABB transfer.
type PPPPFileUploader interface {
	Upload(ctx context.Context, info UploadInfo, payload []byte, progress func(sent, total int64)) error
}

// FileTransferMqttQueue receives layer count extracted from GCode header.
type FileTransferMqttQueue interface {
	SetGCodeLayerCount(layerCount int)
}

// FileTransferEvent describes upload lifecycle notifications.
type FileTransferEvent struct {
	Status     string `json:"status"`
	Name       string `json:"name,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Sent       int64  `json:"sent,omitempty"`
	Percentage int    `json:"percentage,omitempty"`
	Err        string `json:"error,omitempty"`
}

type uploadRequest struct {
	ctx         context.Context
	name        string
	userName    string
	userID      string
	data        []byte
	startPrint  bool
	rateLimitMB int
	result      chan error
}

// FileTransferService performs GCode patching and PPPP upload with progress.
type FileTransferService struct {
	BaseWorker

	uploader PPPPFileUploader
	mqtt     FileTransferMqttQueue

	reqCh chan uploadRequest

	mu sync.RWMutex
}

// NewFileTransferService creates a FileTransferService.
func NewFileTransferService(uploader PPPPFileUploader, mqtt FileTransferMqttQueue) *FileTransferService {
	s := &FileTransferService{
		BaseWorker: NewBaseWorker("filetransfer"),
		uploader:   uploader,
		mqtt:       mqtt,
		reqCh:      make(chan uploadRequest, 8),
	}
	s.BindHooks(s)
	return s
}

func (s *FileTransferService) WorkerInit() {}

func (s *FileTransferService) WorkerStart() error {
	if s.uploader == nil {
		return fmt.Errorf("filetransfer: uploader not configured")
	}
	return nil
}

func (s *FileTransferService) WorkerRun(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case req := <-s.reqCh:
			err := s.handleUpload(req)
			select {
			case req.result <- err:
			default:
			}
		}
	}
}

func (s *FileTransferService) WorkerStop() {}

// SendFile patches GCode metadata and uploads it via PPPP.
func (s *FileTransferService) SendFile(
	ctx context.Context,
	fileName string,
	userName string,
	userID string,
	data []byte,
	rateLimitMB int,
	startPrint bool,
) error {
	req := uploadRequest{
		ctx:         ctx,
		name:        fileName,
		userName:    userName,
		userID:      userID,
		data:        append([]byte(nil), data...),
		rateLimitMB: rateLimitMB,
		startPrint:  startPrint,
		result:      make(chan error, 1),
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.reqCh <- req:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.result:
		return err
	}
}

func (s *FileTransferService) handleUpload(req uploadRequest) error {
	patched := gcode.PatchGCodeTime(req.data)
	layerCount, hasLayerCount := gcode.ExtractLayerCount(req.data)
	if hasLayerCount && s.mqtt != nil {
		s.mqtt.SetGCodeLayerCount(layerCount)
	}

	machineID := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	info := UploadInfo{
		Name:        req.name,
		UserName:    req.userName,
		UserID:      req.userID,
		MachineID:   machineID,
		Size:        int64(len(patched)),
		LayerCount:  layerCount,
		StartPrint:  req.startPrint,
		RateLimitMB: req.rateLimitMB,
	}

	s.Notify(FileTransferEvent{Status: "start", Name: req.name, Size: int64(len(patched)), Sent: 0, Percentage: 0})

	lastEmit := time.Time{}
	progress := func(sent, total int64) {
		now := time.Now()
		if total > 0 && sent < total && !lastEmit.IsZero() && now.Sub(lastEmit) < uploadProgressInterval {
			return
		}
		lastEmit = now
		pct := 0
		if total > 0 {
			pct = int((sent * 100) / total)
			if pct > 100 {
				pct = 100
			}
		}
		s.Notify(FileTransferEvent{Status: "progress", Name: req.name, Size: total, Sent: sent, Percentage: pct})
	}

	if err := s.uploader.Upload(req.ctx, info, patched, progress); err != nil {
		s.Notify(FileTransferEvent{Status: "error", Name: req.name, Size: int64(len(patched)), Sent: 0, Percentage: 0, Err: err.Error()})
		return fmt.Errorf("filetransfer: upload failed: %w", err)
	}

	s.Notify(FileTransferEvent{Status: "done", Name: req.name, Size: int64(len(patched)), Sent: int64(len(patched)), Percentage: 100})
	return nil
}
