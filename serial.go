package main

import (
	"bytes"
	"sync"
	"time"

	"go.bug.st/serial"
)

// SerialManager handles serial port connection and data reading.
type SerialManager struct {
	mu       sync.Mutex
	port     serial.Port
	portName string
	baudRate int
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{} // signals when the reader goroutine has exited
}

// SerialLine represents a single line received from the serial port.
type SerialLine struct {
	Timestamp time.Time
	Data      string
}

func NewSerialManager() *SerialManager {
	return &SerialManager{
		baudRate: 9600,
	}
}

// AvailablePorts returns a list of detected serial port names.
func (sm *SerialManager) AvailablePorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil || len(ports) == 0 {
		return []string{}
	}
	return ports
}

// stopReader signals the reader goroutine to stop and waits for it to exit.
// Must be called with sm.mu held. Releases and re-acquires the lock while waiting.
func (sm *SerialManager) stopReader() {
	if !sm.running {
		return
	}
	close(sm.stopCh)
	doneCh := sm.doneCh
	sm.running = false
	sm.mu.Unlock()
	// Wait for goroutine to finish outside the lock to avoid deadlock
	if doneCh != nil {
		<-doneCh
	}
	sm.mu.Lock()
}

// Connect opens the serial port with the configured settings.
func (sm *SerialManager) Connect(portName string, baudRate int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Stop any existing reader before closing the port
	sm.stopReader()

	if sm.port != nil {
		sm.port.Close()
		sm.port = nil
	}

	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	p, err := serial.Open(portName, mode)
	if err != nil {
		return err
	}

	p.SetReadTimeout(100 * time.Millisecond)
	sm.port = p
	sm.portName = portName
	sm.baudRate = baudRate
	return nil
}

// Disconnect closes the serial port and stops reading.
func (sm *SerialManager) Disconnect() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.stopReader()

	if sm.port != nil {
		sm.port.Close()
		sm.port = nil
	}
}

// IsConnected returns true if a port is currently open.
func (sm *SerialManager) IsConnected() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.port != nil
}

// StartReading begins reading lines from the serial port in a goroutine.
// Each complete line is sent to the returned channel. If an error occurs,
// a SerialLine with empty Data and non-zero Timestamp is NOT sent; instead
// the channel is closed and the error can be detected by the consumer.
func (sm *SerialManager) StartReading() (<-chan SerialLine, <-chan error) {
	ch := make(chan SerialLine, 256)
	errCh := make(chan error, 1)

	sm.mu.Lock()
	if sm.running {
		sm.mu.Unlock()
		close(ch)
		close(errCh)
		return ch, errCh
	}
	sm.stopCh = make(chan struct{})
	sm.doneCh = make(chan struct{})
	sm.running = true
	port := sm.port
	sm.mu.Unlock()

	go func() {
		defer close(ch)
		defer close(sm.doneCh)

		buf := make([]byte, 1024)
		var partial []byte

		for {
			select {
			case <-sm.stopCh:
				return
			default:
			}

			n, err := port.Read(buf)
			if n > 0 {
				partial = append(partial, buf[:n]...)
				// Extract complete lines
				for {
					idx := bytes.IndexByte(partial, '\n')
					if idx < 0 {
						break
					}
					lineData := string(partial[:idx])
					// Strip trailing \r if present
					if len(lineData) > 0 && lineData[len(lineData)-1] == '\r' {
						lineData = lineData[:len(lineData)-1]
					}
					partial = partial[idx+1:]

					line := SerialLine{
						Timestamp: time.Now(),
						Data:      lineData,
					}
					select {
					case ch <- line:
					case <-sm.stopCh:
						return
					}
				}
			}

			if err != nil {
				// Check if we were asked to stop (port closed by Disconnect)
				select {
				case <-sm.stopCh:
					return
				default:
				}
				// Real error — report it
				errCh <- err
				return
			}
		}
	}()

	return ch, errCh
}
