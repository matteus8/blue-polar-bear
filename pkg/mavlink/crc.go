package mavlink

// X25CRC calculates the ITU X.25 / MAVLink CRC-16 checksum.
type X25CRC struct {
	crc uint16
}

// NewX25CRC initializes a new X.25 CRC accumulator seeded with 0xFFFF.
func NewX25CRC() *X25CRC {
	return &X25CRC{crc: 0xFFFF}
}

// Accumulate updates the running CRC with a single byte.
func (c *X25CRC) Accumulate(b byte) {
	ch := b ^ byte(c.crc&0xFF)
	ch = ch ^ (ch << 4)
	c.crc = (c.crc >> 8) ^ (uint16(ch) << 8) ^ (uint16(ch) << 3) ^ (uint16(ch) >> 4)
}

// AccumulateBytes updates the running CRC with a slice of bytes.
func (c *X25CRC) AccumulateBytes(data []byte) {
	for _, b := range data {
		c.Accumulate(b)
	}
}

// Value returns the accumulated 16-bit CRC value.
func (c *X25CRC) Value() uint16 {
	return c.crc
}

// CRC Extra seed bytes for supported MAVLink messages (derived from MAVLink XML definitions)
const (
	CRCExtraHeartbeat         byte = 50
	CRCExtraSysStatus         byte = 124
	CRCExtraAttitude          byte = 39
	CRCExtraGlobalPositionInt byte = 104
	CRCExtraCommandLong       byte = 152
	CRCExtraCommandAck        byte = 143
)

// GetCRCExtra returns the CRC seed byte for a supported message ID.
func GetCRCExtra(msgID uint32) (byte, bool) {
	switch msgID {
	case MsgIDHeartbeat:
		return CRCExtraHeartbeat, true
	case MsgIDSysStatus:
		return CRCExtraSysStatus, true
	case MsgIDAttitude:
		return CRCExtraAttitude, true
	case MsgIDGlobalPositionInt:
		return CRCExtraGlobalPositionInt, true
	case MsgIDCommandLong:
		return CRCExtraCommandLong, true
	case MsgIDCommandAck:
		return CRCExtraCommandAck, true
	default:
		return 0, false
	}
}
