// Package client implements the MQTT client for connecting to Anker's
// cloud MQTT broker. It handles TLS connections, topic subscriptions,
// message encryption/decryption, and provides command/query/fetch APIs.
//
// Topics:
//   - Subscribe: /phone/maker/{SN}/notice, /phone/maker/{SN}/command/reply
//   - Publish:   /device/maker/{SN}/command, /device/maker/{SN}/query
//
// Python source: libflagship/mqttapi.py
package client
