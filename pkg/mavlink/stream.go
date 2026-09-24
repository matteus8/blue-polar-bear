package mavlink

import (
	"bufio"
	"errors"
	"io"
)

// StreamDecoder reads and decodes MAVLink v2 frames sequentially from an io.Reader.
// It handles frame synchronization (hunting for 0xFD magic byte), packet fragmentation,
// and CRC-16 validation, automatically discarding any noise or corrupted bytes on the line.
type StreamDecoder struct {
	reader *bufio.Reader
}

// NewStreamDecoder creates a decoder with an internal 4KB buffer for reading MAVLink v2 frames.
func NewStreamDecoder(r io.Reader) *StreamDecoder {
	return &StreamDecoder{
		reader: bufio.NewReaderSize(r, 4096),
	}
}

// NextFrame blocks until a valid MAVLink v2 Frame is parsed from the stream,
// or returns an error (such as io.EOF or context cancellation).
func (s *StreamDecoder) NextFrame() (*Frame, error) {
	for {
		// 1. Scan forward until we encounter MagicV2 (0xFD)
		b, err := s.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != MagicV2 {
			continue // discard noise byte
		}

		// 2. We found 0xFD. Peek next 9 bytes to read the rest of the 10-byte header
		headerRest, err := s.reader.Peek(HeaderLenV2 - 1)
		if err != nil {
			return nil, err
		}

		payloadLen := int(headerRest[0]) // headerRest[0] is data[1] (PayloadLength)
		totalPacketLen := HeaderLenV2 + payloadLen + ChecksumLen

		// 3. Peek the entire packet (10 + payloadLen + 2) - 1 byte (since we already consumed MagicV2)
		needed := totalPacketLen - 1
		packetRest, err := s.reader.Peek(needed)
		if err != nil {
			return nil, err
		}

		// Reassemble the full packet bytes: [MagicV2, packetRest...]
		fullPacket := make([]byte, totalPacketLen)
		fullPacket[0] = MagicV2
		copy(fullPacket[1:], packetRest)

		// 4. Validate and decode frame (including CRC check)
		frame, err := DecodeFrame(fullPacket)
		if err != nil {
			// False magic byte or corrupted frame; discard this 0xFD and keep hunting
			continue
		}

		// 5. Successfully parsed! Advance the bufio.Reader past the packet
		_, _ = s.reader.Discard(needed)
		return frame, nil
	}
}

// ReadSingleFrame is a convenience helper that reads one MAVLink v2 frame from an io.Reader.
func ReadSingleFrame(r io.Reader) (*Frame, error) {
	decoder := NewStreamDecoder(r)
	frame, err := decoder.NextFrame()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	return frame, nil
}
