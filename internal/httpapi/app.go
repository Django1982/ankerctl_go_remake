package httpapi

import "context"

// AppV1 provides access to /v1/app endpoints.
type AppV1 struct {
	*Client
}

// NewAppV1 creates an AppV1 API client.
func NewAppV1(cfg ClientConfig) (*AppV1, error) {
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		return nil, err
	}
	return &AppV1{Client: c}, nil
}

// GetAppVersion retrieves the OTA app version info.
func (a *AppV1) GetAppVersion(ctx context.Context, appName string, appVersion int, model string) (any, error) {
	if appName == "" {
		appName = "Ankermake_Windows"
	}
	if appVersion == 0 {
		appVersion = 1
	}
	if model == "" {
		model = "-"
	}
	return a.Post(ctx, "/ota/get_app_version", nil, map[string]any{
		"app_name":    appName,
		"app_version": appVersion,
		"model":       model,
	})
}

// QueryFDMList retrieves the list of FDM printers for the authenticated user.
func (a *AppV1) QueryFDMList(ctx context.Context) (any, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
	return a.Post(ctx, "/query_fdm_list", a.AuthHeaders(), nil)
}

// EquipmentGetDSKKeys retrieves DSK keys for the given station serial numbers.
func (a *AppV1) EquipmentGetDSKKeys(ctx context.Context, stationSNs []string, invalidDSKs map[string]any) (any, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
	if invalidDSKs == nil {
		invalidDSKs = map[string]any{}
	}
	return a.Post(ctx, "/equipment/get_dsk_keys", a.AuthHeaders(), map[string]any{
		"invalid_dsks": invalidDSKs,
		"station_sns":  stationSNs,
	})
}
