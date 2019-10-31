package comport

import (
	"time"
)

const DefaultSize = 8 // Default value for Config.Size

type StopBits byte
type Parity byte

const (
	Stop1     StopBits = 1
	Stop1Half StopBits = 15
	Stop2     StopBits = 2
)

const (
	ParityNone  Parity = 'N'
	ParityOdd   Parity = 'O'
	ParityEven  Parity = 'E'
	ParityMark  Parity = 'M' // parity bit is always 1
	ParitySpace Parity = 'S' // parity bit is always 0
)

// Config contains the information needed to open a serial port.
//
// Currently few options are implemented, but more may be added in the
// future (patches welcome), so it is recommended that you create a
// new config addressing the fields by name rather than by order.
//
// For example:
//
//    c0 := &serial.Config{Name: "COM45", Baud: 115200, ReadTimeout: time.Millisecond * 500}
// or
//    c1 := new(serial.Config)
//    c1.Name = "/dev/tty.usbserial"
//    c1.Baud = 115200
//    c1.ReadTimeout = time.Millisecond * 500
//
type Config struct {
	Name        string        `json:"name"`         // COM port name
	Baud        int           `json:"baud"`         // baud rate
	ReadTimeout time.Duration `json:"read_timeout"` // Total read timeout
	Size        byte          `json:"size"`         // The number of data bits. If 0, DefaultSize is used.
	Parity      Parity        `json:"parity"`       // The bit to use and defaults to ParityNone (no parity bit).
	StopBits    StopBits      `json:"stop_bits"`    // The number of stop bits to use. Default is 1 (1 stop bit)

	// RTSFlowControl bool
	// DTRFlowControl bool
	// XONFlowControl bool

	// CRLFTranslate bool
}

// CommStat contains information about a communications device. CommStat is filled by the ClearCommError function.
type CommStat struct {
	Flags, InQue, OutQue uint32
}

type structDCB struct {
	DCBlength, BaudRate                            uint32
	flags                                          [4]byte
	wReserved, XonLim, XoffLim                     uint16
	ByteSize, Parity, StopBits                     byte
	XonChar, XoffChar, ErrorChar, EofChar, EvtChar byte
	wReserved1                                     uint16
}

type structTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}
