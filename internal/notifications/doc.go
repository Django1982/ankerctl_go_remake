// Package notifications implements the Apprise notification system.
//
// It provides the AppriseClient for sending notifications via an external
// Apprise API server, and the AppriseNotifier which integrates with the
// print lifecycle to send events (started, finished, failed, progress,
// gcode_uploaded) with optional camera snapshot attachments.
//
// Python sources: libflagship/notifications/apprise_client.py,
//                 web/notifications.py
package notifications
