// Package httpapi implements the Anker cloud HTTP API client.
//
// It provides access to the Anker cloud services for authentication,
// profile retrieval, printer list queries, and DSK key fetching.
//
// API scopes:
//   - /v1/passport: profile
//   - /v2/passport: login (ECDH-encrypted password)
//   - /v1/app: query_fdm_list, equipment_get_dsk_keys
//   - /v1/hub, /v2/hub: query_device_info, OTA, P2P connect info
//
// Python sources: libflagship/httpapi.py, libflagship/seccode.py
package httpapi
