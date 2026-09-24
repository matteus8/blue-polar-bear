//go:build !darwin && !linux

package mavlink

import (
	"fmt"
	"os"
)

func openSerialFile(path string) (*os.File, error) {
	return nil, fmt.Errorf("serial port is not supported on this OS")
}

func configureSerialPort(f *os.File, baud int) error {
	return fmt.Errorf("serial port configuration is not supported on this OS")
}
