//go:build darwin

package mavlink

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func openSerialFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0666)
}

func configureSerialPort(f *os.File, baud int) error {
	fd := f.Fd()
	var termios syscall.Termios

	// Retrieve current termios settings
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCGETA),
		uintptr(unsafe.Pointer(&termios)),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl TIOCGETA on %s: %w", f.Name(), errno)
	}

	// Raw mode: disable canonical mode, echo, signals, and extended input processing
	termios.Lflag &^= (syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN)

	// Disable input software flow control and carriage return translations
	termios.Iflag &^= (syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON)

	// Disable implementation-defined output processing
	termios.Oflag &^= syscall.OPOST

	// 8N1: 8 data bits, no parity, 1 stop bit, disable hardware RTS/CTS flow control (CCTS_OFLOW | CRTS_IFLOW)
	const darwinCRTSCTS uint64 = 0x00030000
	termios.Cflag &^= (syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | darwinCRTSCTS)
	termios.Cflag |= (syscall.CS8 | syscall.CREAD | syscall.CLOCAL)

	// Blocking read: return as soon as at least 1 byte is available
	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	// Set baud rate (macOS supports direct numeric baud rate values in Ispeed/Ospeed)
	var speed uint64
	switch baud {
	case 9600:
		speed = syscall.B9600
	case 19200:
		speed = syscall.B19200
	case 38400:
		speed = syscall.B38400
	case 57600:
		speed = syscall.B57600
	case 115200:
		speed = syscall.B115200
	case 230400:
		speed = syscall.B230400
	default:
		speed = uint64(baud)
	}
	termios.Ispeed = speed
	termios.Ospeed = speed

	// Apply configured termios settings immediately
	_, _, errno = syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCSETA),
		uintptr(unsafe.Pointer(&termios)),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl TIOCSETA on %s: %w", f.Name(), errno)
	}

	return nil
}
