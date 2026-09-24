//go:build linux

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

	// Retrieve current termios settings on Linux
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&termios)),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl TCGETS on %s: %w", f.Name(), errno)
	}

	// Raw mode: disable canonical mode, echo, signals, and extended input processing
	termios.Lflag &^= (syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN)

	// Disable input software flow control and carriage return translations
	termios.Iflag &^= (syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON)

	// Disable implementation-defined output processing
	termios.Oflag &^= syscall.OPOST

	// 8N1: 8 data bits, no parity, 1 stop bit
	termios.Cflag &^= (syscall.CSIZE | syscall.PARENB | syscall.CSTOPB)
	termios.Cflag |= (syscall.CS8 | syscall.CREAD | syscall.CLOCAL)

	// Blocking read: return as soon as at least 1 byte is available
	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	// Set baud rate using standard Linux CBAUD mask (0010017 octal)
	const cbaudMask uint32 = 0010017
	var baudCode uint32
	switch baud {
	case 9600:
		baudCode = syscall.B9600
	case 19200:
		baudCode = syscall.B19200
	case 38400:
		baudCode = syscall.B38400
	case 57600:
		baudCode = syscall.B57600
	case 115200:
		baudCode = syscall.B115200
	case 230400:
		baudCode = syscall.B230400
	case 460800:
		baudCode = syscall.B460800
	case 921600:
		baudCode = syscall.B921600
	default:
		baudCode = syscall.B115200
	}

	termios.Cflag = (termios.Cflag &^ cbaudMask) | baudCode

	// Apply configured termios settings immediately
	_, _, errno = syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCSETS),
		uintptr(unsafe.Pointer(&termios)),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl TCSETS on %s: %w", f.Name(), errno)
	}

	return nil
}
