// Package handler implements all HTTP handler functions for the ankerctl
// web API, grouped by functional area.
//
// Handler groups:
//   - general:      /, /api/health, /api/version, /video
//   - config:       /api/ankerctl/config/*, /api/ankerctl/server/reload
//   - printer:      /api/printer/gcode, /api/printer/control, /api/printer/autolevel
//   - notification: /api/notifications/*
//   - settings:     /api/settings/timelapse, /api/settings/mqtt
//   - history:      /api/history
//   - timelapse:    /api/timelapses, /api/timelapse/*
//   - filament:     /api/filaments, /api/filaments/*
//   - printers:     /api/printers, /api/printers/active
//   - slicer:       /api/files/local (OctoPrint compatible)
//   - debug:        /api/debug/* (ANKERCTL_DEV_MODE only)
//   - bedlevel:     /api/printer/bed-leveling
//   - snapshot:     /api/snapshot
//   - video:        /video stream
//
// Python source: web/__init__.py (route functions)
package handler
