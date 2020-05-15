package comport

import (
	"fmt"
	"github.com/fpawel/comm"
	"io"
	"time"
)

func NewMock(f RequestToResponseFunc) comm.T {
	return comm.New(NewMockPort(f), comm.Config{
		TimeoutGetResponse: 100 * time.Millisecond,
		TimeoutEndResponse: 0,
	})
}

func NewMockPort(f RequestToResponseFunc) io.ReadWriter {
	return &mockComport{f: f}
}

type RequestToResponseFunc = func(req []byte) []byte

type mockComport struct {
	req  []byte
	resp []byte
	f    RequestToResponseFunc
}

func (x *mockComport) Write(p []byte) (int, error) {
	x.req = p
	x.resp = x.f(x.req)
	return len(p), nil
}

func (x *mockComport) Read(p []byte) (int, error) {
	if len(x.resp) == 0 {
		return 0, fmt.Errorf("unsupported request %02X", x.req)
	}
	if len(p) < len(x.resp) {
		return len(x.resp), nil
	}
	return copy(p, x.resp), nil
}
