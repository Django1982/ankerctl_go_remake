package protocol

// MqttPktType identifies whether a packet is complete or fragmented.
type MqttPktType byte

const (
	// MqttPktSingle indicates the full MQTT message is in this packet.
	MqttPktSingle MqttPktType = 0xC0
	// MqttPktMultiBegin starts a fragmented MQTT message.
	MqttPktMultiBegin MqttPktType = 0xC1
	// MqttPktMultiAppend appends data to an existing fragmented message.
	MqttPktMultiAppend MqttPktType = 0xC2
	// MqttPktMultiFinish appends final data and completes a fragmented message.
	MqttPktMultiFinish MqttPktType = 0xC3
)

// MqttMsgType identifies printer command/event codes transported in JSON payloads.
// Values are mapped directly from libflagship/mqtt.py.
type MqttMsgType uint16

const (
	MqttCmdEventNotify         MqttMsgType = 0x03E8
	MqttCmdPrintSchedule       MqttMsgType = 0x03E9
	MqttCmdFirmwareVersion     MqttMsgType = 0x03EA
	MqttCmdNozzleTemp          MqttMsgType = 0x03EB
	MqttCmdHotbedTemp          MqttMsgType = 0x03EC
	MqttCmdFanSpeed            MqttMsgType = 0x03ED
	MqttCmdPrintSpeed          MqttMsgType = 0x03EE
	MqttCmdAutoLeveling        MqttMsgType = 0x03EF
	MqttCmdPrintControl        MqttMsgType = 0x03F0
	MqttCmdFileListRequest     MqttMsgType = 0x03F1
	MqttCmdGcodeFileRequest    MqttMsgType = 0x03F2
	MqttCmdAllowFirmwareUpdate MqttMsgType = 0x03F3
	MqttCmdGcodeFileDownload   MqttMsgType = 0x03FC
	MqttCmdZAxisRecoup         MqttMsgType = 0x03FD
	MqttCmdExtrusionStep       MqttMsgType = 0x03FE
	MqttCmdEnterOrQuitMateriel MqttMsgType = 0x03FF
	MqttCmdMoveStep            MqttMsgType = 0x0400
	MqttCmdMoveDirection       MqttMsgType = 0x0401
	MqttCmdMoveZero            MqttMsgType = 0x0402
	MqttCmdAppQueryStatus      MqttMsgType = 0x0403
	MqttCmdOnlineNotify        MqttMsgType = 0x0404
	MqttCmdRecoverFactory      MqttMsgType = 0x0405
	MqttCmdBleOnOff            MqttMsgType = 0x0407
	MqttCmdDeleteGcodeFile     MqttMsgType = 0x0408
	MqttCmdResetGcodeParam     MqttMsgType = 0x0409
	MqttCmdDeviceNameSet       MqttMsgType = 0x040A
	MqttCmdDeviceLogUpload     MqttMsgType = 0x040B
	MqttCmdOnOffModal          MqttMsgType = 0x040C
	MqttCmdMotorLock           MqttMsgType = 0x040D
	MqttCmdPreheatConfig       MqttMsgType = 0x040E
	MqttCmdBreakPoint          MqttMsgType = 0x040F
	MqttCmdAICalib             MqttMsgType = 0x0410
	MqttCmdVideoOnOff          MqttMsgType = 0x0411
	MqttCmdAdvancedParameters  MqttMsgType = 0x0412
	MqttCmdGcodeCommand        MqttMsgType = 0x0413
	MqttCmdPreviewImageURL     MqttMsgType = 0x0414
	MqttCmdSystemCheck         MqttMsgType = 0x0419
	MqttCmdAISwitch            MqttMsgType = 0x041A
	MqttCmdAIInfoCheck         MqttMsgType = 0x041B
	MqttCmdModelLayer          MqttMsgType = 0x041C
	MqttCmdModelDLProcess      MqttMsgType = 0x041D
	MqttCmdPrintMaxSpeed       MqttMsgType = 0x041F
	MqttCmdAlexaMsg            MqttMsgType = 0x0BB8
)

const (
	mqttSignatureA = 'M'
	mqttSignatureB = 'A'
	mqttMagicM3    = 5
	mqttMagicM4    = 1
	mqttMagicM5M5C = 1
	mqttMagicM5M5  = 2
	mqttMagicM6    = 5
	mqttMagicM7    = 'F'
)

const (
	// HeaderLenM5 is the fixed header size for M5 packets (m5=2).
	HeaderLenM5 = 64
	// HeaderLenM5C is the fixed header size for M5C packets (m5=1).
	HeaderLenM5C = 24
)
