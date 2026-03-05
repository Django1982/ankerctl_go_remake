package httpapi

import "context"

// HubV1 provides access to /v1/hub endpoints.
type HubV1 struct {
	*Client
}

// NewHubV1 creates a HubV1 API client.
func NewHubV1(cfg ClientConfig) (*HubV1, error) {
	c, err := NewClient(cfg, "/v1/hub")
	if err != nil {
		return nil, err
	}
	return &HubV1{Client: c}, nil
}

// QueryDeviceInfo queries device information using the v1 check code.
func (h *HubV1) QueryDeviceInfo(ctx context.Context, stationSN, checkCode string) (any, error) {
	return h.Post(ctx, "/query_device_info", nil, map[string]any{
		"station_sn": stationSN,
		"check_code": checkCode,
	})
}

// OTAGetRomVersion queries the OTA firmware version.
func (h *HubV1) OTAGetRomVersion(ctx context.Context, printerSN, checkCode, deviceType, currentVersion string) (any, error) {
	if deviceType == "" {
		deviceType = "V8111_Model"
	}
	if currentVersion == "" {
		currentVersion = "V1.0.5"
	}
	return h.Post(ctx, "/ota/get_rom_version", nil, map[string]any{
		"sn":                   printerSN,
		"check_code":           checkCode,
		"device_type":          deviceType,
		"current_version_name": currentVersion,
	})
}

// HubV2 provides access to /v2/hub endpoints.
type HubV2 struct {
	*Client
}

// NewHubV2 creates a HubV2 API client.
func NewHubV2(cfg ClientConfig) (*HubV2, error) {
	c, err := NewClient(cfg, "/v2/hub")
	if err != nil {
		return nil, err
	}
	return &HubV2{Client: c}, nil
}

// QueryDeviceInfo queries device information using the v2 sec code.
func (h *HubV2) QueryDeviceInfo(ctx context.Context, stationSN, secCode, secTS string) (any, error) {
	return h.Post(ctx, "/query_device_info", nil, map[string]any{
		"station_sn": stationSN,
		"sec_code":   secCode,
		"sec_ts":     secTS,
	})
}

// OTAGetRomVersion queries the OTA firmware version using v2 sec code.
func (h *HubV2) OTAGetRomVersion(ctx context.Context, printerSN, secCode, secTS, deviceType, currentVersion string) (any, error) {
	if deviceType == "" {
		deviceType = "V8111"
	}
	if currentVersion == "" {
		currentVersion = "V1"
	}
	return h.Post(ctx, "/ota/get_rom_version", nil, map[string]any{
		"sn":                   printerSN,
		"sec_code":             secCode,
		"sec_ts":               secTS,
		"device_type":          deviceType,
		"current_version_name": currentVersion,
	})
}

// GetP2PConnectInfo retrieves P2P connection info for a printer.
func (h *HubV2) GetP2PConnectInfo(ctx context.Context, printerSN, secCode, secTS string) (any, error) {
	return h.Post(ctx, "/get_p2p_connectinfo", nil, map[string]any{
		"station_sn": printerSN,
		"sec_code":   secCode,
		"sec_ts":     secTS,
	})
}
