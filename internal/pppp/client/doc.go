// Package client implements the PPPP API for establishing and managing
// LAN connections to AnkerMake printers over UDP.
//
// It provides Channel management with in-flight windowing (max 64 packets),
// Wire abstraction for inter-goroutine byte streaming, and both synchronous
// (AnkerPPPPApi) and asynchronous (AnkerPPPPAsyncApi) connection modes.
//
// Key types: Channel, Wire, PPPPState, AnkerPPPPApi, FileUploadInfo.
//
// Python source: libflagship/ppppapi.py
package client
