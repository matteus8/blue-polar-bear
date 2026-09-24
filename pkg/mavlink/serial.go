package mavlink

import (
	"fmt"
	"io"
	"os"
)

// SerialPort wraps an underlying OS file descriptor representing a physical serial / UART port.
type SerialPort struct {
	file *os.File
	path string
	baud int
}

var _ io.ReadWriteCloser = (*SerialPort)(nil)

// OpenSerialPort opens a serial character device (e.g. /dev/ttyACM0 or /dev/tty.usbmodem1)
// in non-blocking raw 8N1 mode at the specified baud rate (e.g. 57600, 115200, 921600).
func OpenSerialPort(path string, baud int) (*SerialPort, error) {
	if path == "" {
		return nil, fmt.Errorf("serial port path cannot be empty")
	}
	if baud <= 0 {
		baud = 115200
	}

	// Open file with O_RDWR and O_NOCTTY
	f, err := openSerialFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening serial port %s: %w", path, err)
	}

	// Apply OS-specific termios configuration
	if err := configureSerialPort(f, baud); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("configuring serial port %s at %d baud: %w", path, baud, err)
	}

	return &SerialPort{
		file: f,
		path: path,
		baud: baud,
	}, nil
}

// Read reads raw bytes from the serial port.
func (p *SerialPort) Read(b []byte) (n int, err error) {
	return p.file.Read(b)
}

// Write writes raw bytes to the serial port.
func (p *SerialPort) Write(b []byte) (n int, err error) {
	return p.file.Write(b)
}

// Close closes the serial port device file.
func (p *SerialPort) Close() error {
	if p.file != nil {
		return p.file.Close()
	}
	return nil
}

// Path returns the filesystem path of the serial device.
func (p *SerialPort) Path() string {
	return p.path
}

// Baud returns the configured baud rate.
func (p *SerialPort) Baud() int {
	return p.baud
}
