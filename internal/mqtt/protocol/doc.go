// Package protocol defines the MQTT message types and packet structures
// for communication with AnkerMake printers.
//
// This includes MqttMsg (encrypted packet with header), MqttPktType (single/multi),
// and MqttMsgType (all 39 command types like EVENT_NOTIFY, PRINT_SCHEDULE, etc.).
//
// The MQTT payload is AES-256-CBC encrypted with the printer's mqtt_key and
// a fixed IV of "3DPrintAnkerMake", wrapped with an XOR checksum.
//
// Python source: libflagship/mqtt.py (auto-generated from specification/mqtt.stf)
package protocol
