package mavlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// MAVLink protocol framing constants
const (
	MagicV1 byte = 0xFE
	MagicV2 byte = 0xFD

	HeaderLenV2 int = 10
	ChecksumLen int = 2
)

// Supported MAVLink Message IDs
const (
	MsgIDHeartbeat         uint32 = 0
	MsgIDSysStatus         uint32 = 1
	MsgIDAttitude          uint32 = 30
	MsgIDGlobalPositionInt uint32 = 33
	MsgIDCommandLong       uint32 = 76
	MsgIDCommandAck        uint32 = 77
)

// Common MAVLink Commands (MAV_CMD)
const (
	MavCmdNavReturnToLaunch uint16 = 20
	MavCmdNavLand           uint16 = 21
	MavCmdNavTakeoff        uint16 = 22
	MavCmdDoSetMode         uint16 = 176
	MavCmdComponentArmDisarm uint16 = 400
)

// MAVLink Frame represents a parsed MAVLink v2 binary packet.
type Frame struct {
	Magic         byte
	PayloadLength byte
	IncompatFlags byte
	CompatFlags   byte
	Sequence      byte
	SystemID      byte
	ComponentID   byte
	MessageID     uint32
	Payload       []byte
	Checksum      uint16
}

// Heartbeat message (ID: 0)
type Heartbeat struct {
	CustomMode     uint32
	Type           uint8
	Autopilot      uint8
	BaseMode       uint8
	SystemStatus   uint8
	MavlinkVersion uint8
}

// IsArmed returns true if the MAV_MODE_FLAG_SAFETY_ARMED bit (0x80) is set in BaseMode.
func (h *Heartbeat) IsArmed() bool {
	return (h.BaseMode & 0x80) != 0
}

// GlobalPositionInt message (ID: 33)
type GlobalPositionInt struct {
	TimeBootMs  uint32
	Lat         int32  // Latitude (deg * 1e7)
	Lon         int32  // Longitude (deg * 1e7)
	Alt         int32  // Altitude (mm MSL)
	RelativeAlt int32  // Altitude above ground (mm)
	Vx          int16  // Ground X speed (cm/s, North)
	Vy          int16  // Ground Y speed (cm/s, East)
	Vz          int16  // Ground Z speed (cm/s, Down)
	Hdg         uint16 // Vehicle heading (centidegrees: 0..35999)
}

// LatDegrees converts integer coordinates to float64 degrees.
func (g *GlobalPositionInt) LatDegrees() float64 {
	return float64(g.Lat) / 1e7
}

// LonDegrees converts integer coordinates to float64 degrees.
func (g *GlobalPositionInt) LonDegrees() float64 {
	return float64(g.Lon) / 1e7
}

// AltMeters converts relative altitude from mm to meters.
func (g *GlobalPositionInt) AltMeters() float64 {
	return float64(g.RelativeAlt) / 1000.0
}

// GroundSpeedMps calculates 2D ground speed in m/s from Vx and Vy.
func (g *GlobalPositionInt) GroundSpeedMps() float64 {
	vx := float64(g.Vx) / 100.0
	vy := float64(g.Vy) / 100.0
	return math.Sqrt(vx*vx + vy*vy)
}

// HeadingDeg converts centidegrees to degrees.
func (g *GlobalPositionInt) HeadingDeg() float64 {
	return float64(g.Hdg) / 100.0
}

// SysStatus message (ID: 1)
type SysStatus struct {
	SensorsPresent   uint32
	SensorsEnabled   uint32
	SensorsHealth    uint32
	Load             uint16
	VoltageBattery   uint16 // Millivolts
	CurrentBattery   int16  // Centiamperes
	BatteryRemaining int8   // Percentage (0..100, or -1 if unknown)
	DropRateComm     uint16
	ErrorsComm       uint16
	ErrorsCount1     uint16
	ErrorsCount2     uint16
	ErrorsCount3     uint16
	ErrorsCount4     uint16
}

// Attitude message (ID: 30)
type Attitude struct {
	TimeBootMs uint32
	Roll       float32 // Roll angle (rad, -pi..+pi)
	Pitch      float32 // Pitch angle (rad, -pi..+pi)
	Yaw        float32 // Yaw angle (rad, -pi..+pi)
	RollSpeed  float32 // Roll angular speed (rad/s)
	PitchSpeed float32 // Pitch angular speed (rad/s)
	YawSpeed   float32 // Yaw angular speed (rad/s)
}

// CommandLong message (ID: 76)
type CommandLong struct {
	Param1          float32
	Param2          float32
	Param3          float32
	Param4          float32
	Param5          float32
	Param6          float32
	Param7          float32
	Command         uint16
	TargetSystem    uint8
	TargetComponent uint8
	Confirmation    uint8
}

// DecodeFrame parses a raw byte slice into a MAVLink v2 Frame.
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < HeaderLenV2+ChecksumLen {
		return nil, errors.New("mavlink packet too short")
	}

	if data[0] != MagicV2 {
		return nil, fmt.Errorf("unsupported magic byte 0x%02X (expected MAVLink v2 0xFD)", data[0])
	}

	payloadLen := int(data[1])
	expectedTotalLen := HeaderLenV2 + payloadLen + ChecksumLen
	if len(data) < expectedTotalLen {
		return nil, fmt.Errorf("packet length %d shorter than expected %d", len(data), expectedTotalLen)
	}

	msgID := uint32(data[7]) | (uint32(data[8]) << 8) | (uint32(data[9]) << 16)

	frame := &Frame{
		Magic:         data[0],
		PayloadLength: data[1],
		IncompatFlags: data[2],
		CompatFlags:   data[3],
		Sequence:      data[4],
		SystemID:      data[5],
		ComponentID:   data[6],
		MessageID:     msgID,
		Payload:       make([]byte, payloadLen),
		Checksum:      binary.LittleEndian.Uint16(data[HeaderLenV2+payloadLen : expectedTotalLen]),
	}
	copy(frame.Payload, data[HeaderLenV2:HeaderLenV2+payloadLen])

	// Validate CRC
	crcExtra, ok := GetCRCExtra(msgID)
	if ok {
		crc := NewX25CRC()
		// Accumulate bytes 1 through HeaderLenV2+payloadLen-1 (everything except magic byte)
		crc.AccumulateBytes(data[1 : HeaderLenV2+payloadLen])
		crc.Accumulate(crcExtra)
		if crc.Value() != frame.Checksum {
			return nil, fmt.Errorf("crc mismatch: packet has 0x%04X, computed 0x%04X", frame.Checksum, crc.Value())
		}
	}

	return frame, nil
}

// EncodeFrame serializes a MAVLink v2 packet into a byte slice with valid CRC.
func EncodeFrame(sysID, compID, seq byte, msgID uint32, payload []byte) ([]byte, error) {
	payloadLen := len(payload)
	if payloadLen > 255 {
		return nil, errors.New("payload length exceeds maximum 255 bytes")
	}

	totalLen := HeaderLenV2 + payloadLen + ChecksumLen
	buf := make([]byte, totalLen)

	buf[0] = MagicV2
	buf[1] = byte(payloadLen)
	buf[2] = 0 // Incompat flags
	buf[3] = 0 // Compat flags
	buf[4] = seq
	buf[5] = sysID
	buf[6] = compID
	buf[7] = byte(msgID & 0xFF)
	buf[8] = byte((msgID >> 8) & 0xFF)
	buf[9] = byte((msgID >> 16) & 0xFF)

	copy(buf[HeaderLenV2:HeaderLenV2+payloadLen], payload)

	// Compute CRC
	crc := NewX25CRC()
	crc.AccumulateBytes(buf[1 : HeaderLenV2+payloadLen])
	if crcExtra, ok := GetCRCExtra(msgID); ok {
		crc.Accumulate(crcExtra)
	}
	checksum := crc.Value()
	binary.LittleEndian.PutUint16(buf[HeaderLenV2+payloadLen:], checksum)

	return buf, nil
}

// DecodeHeartbeat unpacks a Heartbeat message from payload bytes.
func DecodeHeartbeat(payload []byte) (*Heartbeat, error) {
	if len(payload) < 9 {
		return nil, errors.New("heartbeat payload too short")
	}
	return &Heartbeat{
		CustomMode:     binary.LittleEndian.Uint32(payload[0:4]),
		Type:           payload[4],
		Autopilot:      payload[5],
		BaseMode:       payload[6],
		SystemStatus:   payload[7],
		MavlinkVersion: payload[8],
	}, nil
}

// EncodeHeartbeat packs a Heartbeat into payload bytes.
func EncodeHeartbeat(h *Heartbeat) []byte {
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint32(buf[0:4], h.CustomMode)
	buf[4] = h.Type
	buf[5] = h.Autopilot
	buf[6] = h.BaseMode
	buf[7] = h.SystemStatus
	buf[8] = h.MavlinkVersion
	return buf
}

// DecodeGlobalPositionInt unpacks a GlobalPositionInt message.
func DecodeGlobalPositionInt(payload []byte) (*GlobalPositionInt, error) {
	if len(payload) < 28 {
		return nil, errors.New("global_position_int payload too short")
	}
	return &GlobalPositionInt{
		TimeBootMs:  binary.LittleEndian.Uint32(payload[0:4]),
		Lat:         int32(binary.LittleEndian.Uint32(payload[4:8])),
		Lon:         int32(binary.LittleEndian.Uint32(payload[8:12])),
		Alt:         int32(binary.LittleEndian.Uint32(payload[12:16])),
		RelativeAlt: int32(binary.LittleEndian.Uint32(payload[16:20])),
		Vx:          int16(binary.LittleEndian.Uint16(payload[20:22])),
		Vy:          int16(binary.LittleEndian.Uint16(payload[22:24])),
		Vz:          int16(binary.LittleEndian.Uint16(payload[24:26])),
		Hdg:         binary.LittleEndian.Uint16(payload[26:28]),
	}, nil
}

// EncodeGlobalPositionInt packs a GlobalPositionInt into payload bytes.
func EncodeGlobalPositionInt(g *GlobalPositionInt) []byte {
	buf := make([]byte, 28)
	binary.LittleEndian.PutUint32(buf[0:4], g.TimeBootMs)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(g.Lat))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(g.Lon))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(g.Alt))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(g.RelativeAlt))
	binary.LittleEndian.PutUint16(buf[20:22], uint16(g.Vx))
	binary.LittleEndian.PutUint16(buf[22:24], uint16(g.Vy))
	binary.LittleEndian.PutUint16(buf[24:26], uint16(g.Vz))
	binary.LittleEndian.PutUint16(buf[26:28], g.Hdg)
	return buf
}

// DecodeSysStatus unpacks a SysStatus message.
func DecodeSysStatus(payload []byte) (*SysStatus, error) {
	if len(payload) < 31 {
		return nil, errors.New("sys_status payload too short")
	}
	return &SysStatus{
		SensorsPresent:   binary.LittleEndian.Uint32(payload[0:4]),
		SensorsEnabled:   binary.LittleEndian.Uint32(payload[4:8]),
		SensorsHealth:    binary.LittleEndian.Uint32(payload[8:12]),
		Load:             binary.LittleEndian.Uint16(payload[12:14]),
		VoltageBattery:   binary.LittleEndian.Uint16(payload[14:16]),
		CurrentBattery:   int16(binary.LittleEndian.Uint16(payload[16:18])),
		BatteryRemaining: int8(payload[18]),
		DropRateComm:     binary.LittleEndian.Uint16(payload[19:21]),
		ErrorsComm:       binary.LittleEndian.Uint16(payload[21:23]),
		ErrorsCount1:     binary.LittleEndian.Uint16(payload[23:25]),
		ErrorsCount2:     binary.LittleEndian.Uint16(payload[25:27]),
		ErrorsCount3:     binary.LittleEndian.Uint16(payload[27:29]),
		ErrorsCount4:     binary.LittleEndian.Uint16(payload[29:31]),
	}, nil
}

// EncodeSysStatus packs a SysStatus into payload bytes.
func EncodeSysStatus(s *SysStatus) []byte {
	buf := make([]byte, 31)
	binary.LittleEndian.PutUint32(buf[0:4], s.SensorsPresent)
	binary.LittleEndian.PutUint32(buf[4:8], s.SensorsEnabled)
	binary.LittleEndian.PutUint32(buf[8:12], s.SensorsHealth)
	binary.LittleEndian.PutUint16(buf[12:14], s.Load)
	binary.LittleEndian.PutUint16(buf[14:16], s.VoltageBattery)
	binary.LittleEndian.PutUint16(buf[16:18], uint16(s.CurrentBattery))
	buf[18] = byte(s.BatteryRemaining)
	binary.LittleEndian.PutUint16(buf[19:21], s.DropRateComm)
	binary.LittleEndian.PutUint16(buf[21:23], s.ErrorsComm)
	binary.LittleEndian.PutUint16(buf[23:25], s.ErrorsCount1)
	binary.LittleEndian.PutUint16(buf[25:27], s.ErrorsCount2)
	binary.LittleEndian.PutUint16(buf[27:29], s.ErrorsCount3)
	binary.LittleEndian.PutUint16(buf[29:31], s.ErrorsCount4)
	return buf
}

// DecodeAttitude unpacks an Attitude message.
func DecodeAttitude(payload []byte) (*Attitude, error) {
	if len(payload) < 28 {
		return nil, errors.New("attitude payload too short")
	}
	return &Attitude{
		TimeBootMs: binary.LittleEndian.Uint32(payload[0:4]),
		Roll:       math.Float32frombits(binary.LittleEndian.Uint32(payload[4:8])),
		Pitch:      math.Float32frombits(binary.LittleEndian.Uint32(payload[8:12])),
		Yaw:        math.Float32frombits(binary.LittleEndian.Uint32(payload[12:16])),
		RollSpeed:  math.Float32frombits(binary.LittleEndian.Uint32(payload[16:20])),
		PitchSpeed: math.Float32frombits(binary.LittleEndian.Uint32(payload[20:24])),
		YawSpeed:   math.Float32frombits(binary.LittleEndian.Uint32(payload[24:28])),
	}, nil
}

// EncodeCommandLong packs a CommandLong message.
func EncodeCommandLong(c *CommandLong) []byte {
	buf := make([]byte, 33)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(c.Param1))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(c.Param2))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(c.Param3))
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(c.Param4))
	binary.LittleEndian.PutUint32(buf[16:20], math.Float32bits(c.Param5))
	binary.LittleEndian.PutUint32(buf[20:24], math.Float32bits(c.Param6))
	binary.LittleEndian.PutUint32(buf[24:28], math.Float32bits(c.Param7))
	binary.LittleEndian.PutUint16(buf[28:30], c.Command)
	buf[30] = c.TargetSystem
	buf[31] = c.TargetComponent
	buf[32] = c.Confirmation
	return buf
}
