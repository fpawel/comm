package comport

import (
	"github.com/ansel1/merry"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type winComport struct {
	f  *os.File
	fd syscall.Handle
	rl sync.Mutex
	wl sync.Mutex
	ro *syscall.Overlapped
	wo *syscall.Overlapped
}

func (p *winComport) Close() error {
	return p.f.Close()
}

func (p *winComport) Write(buf []byte) (int, error) {
	n, err := p.write(buf)
	if err != nil {
		return n, merry.Appendf(err, "written count: %d", n)
	}
	return n, nil
}

func (p *winComport) Read(buf []byte) (int, error) {
	n, err := p.read(buf)
	if err != nil {
		return n, merry.Appendf(err, "read count: %d", n)
	}
	return n, nil
}

// Discards data written to the port but not transmitted,
// or data received but not read
func (p *winComport) Flush() error {
	return purgeComm(p.fd)
}

// Retrieves information about a communications error and reports the current status of a communications device.
// The function is called when a communications error occurs,
// and it clears the device's error flag to enable additional input and output (I/O) operations.
func (p *winComport) ClearCommError(errors *uint32, commStat *CommStat) error {
	return clearCommError(p.fd, errors, commStat)
}

func (p *winComport) BytesToReadCount() (int, error) {
	var (
		errors   uint32
		commStat CommStat
	)
	if err := p.ClearCommError(&errors, &commStat); err != nil {
		return 0, merry.Appendf(err, "unable to get bytes to read count")
	}
	return int(commStat.InQue), nil
}

// openPort opens a serial port with the specified configuration
func openWinComport(c *Config) (*winComport, error) {
	size, par, stop := c.Size, c.Parity, c.StopBits
	if size == 0 {
		size = DefaultSize
	}
	if par == 0 {
		par = ParityNone
	}
	if stop == 0 {
		stop = Stop1
	}
	return openFile(c.Name, c.Baud, size, par, stop, c.ReadTimeout)
}

//func openWinComportDebounce(config *Config, timeout time.Duration, ctx context.Context) (port *winComport, err error) {
//	ctx, _ = context.WithTimeout(ctx, timeout)
//	var wg sync.WaitGroup
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		for {
//			select {
//			case <-ctx.Done():
//				if err == nil {
//					err = ctx.Err()
//				}
//				return
//			default:
//				if port, err = openWinComport(config); err == nil {
//					return
//				}
//			}
//		}
//	}()
//	wg.Wait()
//	return
//}

var (
	nSetCommState,
	nSetCommTimeouts,
	nSetCommMask,
	nSetupComm,
	nGetOverlappedResult,
	nCreateEvent,
	nResetEvent,
	nPurgeComm,
	//nFlushFileBuffers,
	nClearCommError uintptr
)

func openFile(name string, baud int, databits byte, parity Parity, stopbits StopBits, readTimeout time.Duration) (*winComport, error) {
	if err := CheckPortNameIsValid(name); err != nil {
		return nil, err
	}
	if len(name) > 0 && name[0] != '\\' {
		name = "\\\\.\\" + name
	}
	utf16ptrName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		panic(err)
	}
	h, err := syscall.CreateFile(utf16ptrName,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OVERLAPPED,
		0)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "the system cannot find the file specified") {
			err = merry.New("нет СОМ порта с таким именем")
		}
		if strings.Contains(strings.ToLower(err.Error()), "access is denied") {
			err = merry.New("СОМ порт занят")
		}
		return nil, err
	}

	f := os.NewFile(uintptr(h), name)

	if err = setCommState(h, baud, databits, parity, stopbits); err != nil {
		return nil, err
	}
	if err = setupComm(h, 64, 64); err != nil {
		return nil, err
	}
	if err = setCommTimeouts(h, readTimeout); err != nil {
		return nil, err
	}
	if err = setCommMask(h); err != nil {
		return nil, err
	}

	ro, err := newOverlapped()
	if err != nil {
		return nil, err
	}
	wo, err := newOverlapped()
	if err != nil {
		return nil, err
	}
	port := new(winComport)
	port.f = f
	port.fd = h
	port.ro = ro
	port.wo = wo

	return port, nil
}

func getProcAddr(lib syscall.Handle, name string) uintptr {
	addr, err := syscall.GetProcAddress(lib, name)
	if err != nil {
		panic(name + " " + err.Error())
	}
	return addr
}

func (p *winComport) write(buf []byte) (int, error) {
	p.wl.Lock()
	defer p.wl.Unlock()

	if err := p.Flush(); err != nil {
		return 0, merry.Appendf(err, "attempt to write: % X", buf)
	}

	if err := resetEvent(p.wo.HEvent); err != nil {
		return 0, merry.Appendf(err, "attempt to write: % X", buf)
	}
	var n uint32
	err := syscall.WriteFile(p.fd, buf, &n, p.wo)
	if err != nil && err != syscall.ERROR_IO_PENDING {
		return int(n), merry.Appendf(err, "attempt to write: % X", buf)
	}
	return getOverlappedResult(p.fd, p.wo)
}

func (p *winComport) read(buf []byte) (int, error) {
	if p == nil || p.f == nil {
		return 0, merry.New("invalid port on read")
	}

	p.rl.Lock()
	defer p.rl.Unlock()

	if err := resetEvent(p.ro.HEvent); err != nil {
		return 0, err
	}
	var done uint32
	err := syscall.ReadFile(p.fd, buf, &done, p.ro)
	if err != nil && err != syscall.ERROR_IO_PENDING {
		return int(done), err
	}
	return getOverlappedResult(p.fd, p.ro)
}

func setCommState(h syscall.Handle, baud int, databits byte, parity Parity, stopbits StopBits) error {
	var params structDCB
	params.DCBlength = uint32(unsafe.Sizeof(params))

	params.flags[0] = 0x01  // fBinary
	params.flags[0] |= 0x10 // Assert DSR

	params.BaudRate = uint32(baud)

	params.ByteSize = databits

	switch parity {
	case ParityNone:
		params.Parity = 0
	case ParityOdd:
		params.Parity = 1
	case ParityEven:
		params.Parity = 2
	case ParityMark:
		params.Parity = 3
	case ParitySpace:
		params.Parity = 4
	default:
		return merry.New("unsupported parity setting")
	}

	switch stopbits {
	case Stop1:
		params.StopBits = 0
	case Stop1Half:
		params.StopBits = 1
	case Stop2:
		params.StopBits = 2
	default:
		return merry.New("unsupported stop bit setting")
	}

	r, _, err := syscall.Syscall(nSetCommState, 2, uintptr(h), uintptr(unsafe.Pointer(&params)), 0)
	if r == 0 {
		return err
	}
	return nil
}

func setCommTimeouts(h syscall.Handle, readTimeout time.Duration) error {
	var timeouts structTimeouts
	const MAXDWORD = 1<<32 - 1

	// blocking read by default
	var timeoutMs int64 = MAXDWORD - 1

	if readTimeout > 0 {
		// non-blocking read
		timeoutMs = readTimeout.Nanoseconds() / 1e6
		if timeoutMs < 1 {
			timeoutMs = 1
		} else if timeoutMs > MAXDWORD-1 {
			timeoutMs = MAXDWORD - 1
		}
	}

	/* From http://msdn.microsoft.com/en-us/library/aa363190(v=VS.85).aspx

		 For blocking I/O see below:

		 Remarks:

		 If an application sets ReadIntervalTimeout and
		 ReadTotalTimeoutMultiplier to MAXDWORD and sets
		 ReadTotalTimeoutConstant to a value greater than zero and
		 less than MAXDWORD, one of the following occurs when the
		 ReadFile function is called:

		 If there are any bytes in the input buffer, ReadFile returns
		       immediately with the bytes in the buffer.

		 If there are no bytes in the input buffer, ReadFile waits
	               until a byte arrives and then returns immediately.

		 If no bytes arrive within the time specified by
		       ReadTotalTimeoutConstant, ReadFile times out.
	*/

	timeouts.ReadIntervalTimeout = MAXDWORD
	timeouts.ReadTotalTimeoutMultiplier = MAXDWORD
	timeouts.ReadTotalTimeoutConstant = uint32(timeoutMs)

	r, _, err := syscall.Syscall(nSetCommTimeouts, 2, uintptr(h), uintptr(unsafe.Pointer(&timeouts)), 0)
	if r == 0 {
		return err
	}
	return nil
}

func setupComm(h syscall.Handle, in, out int) error {
	r, _, err := syscall.Syscall(nSetupComm, 3, uintptr(h), uintptr(in), uintptr(out))
	if r == 0 {
		return err
	}
	return nil
}

func setCommMask(h syscall.Handle) error {
	const EV_RXCHAR = 0x0001
	r, _, err := syscall.Syscall(nSetCommMask, 2, uintptr(h), EV_RXCHAR, 0)
	if r == 0 {
		return err
	}
	return nil
}

func resetEvent(h syscall.Handle) error {
	r, _, err := syscall.Syscall(nResetEvent, 1, uintptr(h), 0, 0)
	if r == 0 {
		return err
	}
	return nil
}

func purgeComm(h syscall.Handle) error {
	const PURGE_TXABORT = 0x0001
	const PURGE_RXABORT = 0x0002
	const PURGE_TXCLEAR = 0x0004
	const PURGE_RXCLEAR = 0x0008
	r, _, err := syscall.Syscall(nPurgeComm, 2, uintptr(h),
		PURGE_TXABORT|PURGE_RXABORT|PURGE_TXCLEAR|PURGE_RXCLEAR, 0)
	if r == 0 {
		return err
	}
	return nil
}

func newOverlapped() (*syscall.Overlapped, error) {
	var overlapped syscall.Overlapped
	r, _, err := syscall.Syscall6(nCreateEvent, 4, 0, 1, 0, 0, 0, 0)
	if r == 0 {
		return nil, err
	}
	overlapped.HEvent = syscall.Handle(r)
	return &overlapped, nil
}

func getOverlappedResult(h syscall.Handle, overlapped *syscall.Overlapped) (int, error) {
	var n int
	r, _, err := syscall.Syscall6(nGetOverlappedResult, 4,
		uintptr(h),
		uintptr(unsafe.Pointer(overlapped)),
		uintptr(unsafe.Pointer(&n)), 1, 0, 0)
	if r == 0 {
		return n, err
	}

	return n, nil
}

func clearCommError(h syscall.Handle, errors *uint32, commStat *CommStat) error {
	r, _, err := syscall.Syscall6(nClearCommError, 3,
		uintptr(h),
		uintptr(unsafe.Pointer(errors)),
		uintptr(unsafe.Pointer(commStat)), 0, 0, 0)
	if r == 0 {
		return err
	}
	return nil
}

func init() {
	k32, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		panic("LoadLibrary " + err.Error())
	}
	defer func() {
		_ = syscall.FreeLibrary(k32)
	}()

	nSetCommState = getProcAddr(k32, "SetCommState")
	nSetCommTimeouts = getProcAddr(k32, "SetCommTimeouts")
	nSetCommMask = getProcAddr(k32, "SetCommMask")
	nSetupComm = getProcAddr(k32, "SetupComm")
	nGetOverlappedResult = getProcAddr(k32, "GetOverlappedResult")
	nCreateEvent = getProcAddr(k32, "CreateEventW")
	nResetEvent = getProcAddr(k32, "ResetEvent")
	nPurgeComm = getProcAddr(k32, "PurgeComm")
	//nFlushFileBuffers = getProcAddr(k32, "FlushFileBuffers")
	nClearCommError = getProcAddr(k32, "ClearCommError")
}