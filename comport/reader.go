package comport

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"time"
)

type Reader struct {
	config Config
	port   *Port
	logger *structlog.Logger
}

func NewReader(config Config, device string) *Reader {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = time.Millisecond
	}
	return &Reader{
		config: config,
		logger: structlog.New("device", device).AppendPrefixKeys("device"),
	}
}

func (x *Reader) Config() Config {
	return x.config
}

func (x *Reader) Opened() bool {
	return x.port != nil
}

func (x *Reader) Open(name string) error {
	if x.Opened() {
		return merry.New("already opened")
	}
	x.config.Name = name
	port, err := OpenPort(&x.config)
	if err == nil {
		x.port = port
		x.logger.Info("порт открыт", "config", x.config)
	} else {
		err = merry.Append(err, name)
	}

	return err
}

func (x *Reader) Close() error {
	if x.port == nil {
		return nil
	}
	err := x.port.Close()
	x.port = nil
	return err
}

func (x *Reader) OpenDebounce(name string, bounceTimeout time.Duration, ctx context.Context) error {
	if x.Opened() {
		return merry.New("already opened")
	}
	x.config.Name = name
	port, err := OpenPortDebounce(&x.config, bounceTimeout, ctx)
	if err == nil {
		x.port = port
	}
	return err
}

func (x *Reader) GetResponse(request []byte, commConfig comm.Config, ctx context.Context, what string, prs comm.ResponseParser) ([]byte, error) {
	strPort := fmt.Sprintf("%s,%d", x.config.Name, x.config.Baud)
	logger := x.logger.New("port", strPort, "what", what).
		SetSuffixKeys("port", "what")
	commRequest := comm.Request{
		Bytes:          request,
		Config:         commConfig,
		ReadWriter:     x.port,
		ResponseParser: prs,
		Logger:         logger,
	}
	return comm.GetResponse(commRequest, ctx)
}

func (x *Reader) Write(buf []byte) (int, error) {
	return x.port.Write(buf)
}
