package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/hako/durafmt"
	"github.com/powerman/structlog"
	"io"
	"time"
)

type ReadWriter interface {
	io.ReadWriter
	BytesToReadCount() (int, error)
}

type Request struct {
	Bytes          []byte
	Config         Config
	ReadWriter     ReadWriter
	ResponseParser ResponseParser
	Logger         *structlog.Logger
}

type ResponseParser = func(request, response []byte) (string, error)

type Config struct {
	ReadTimeoutMillis     int `toml:"read_timeout" comment:"таймаут получения ответа, мс"`
	ReadByteTimeoutMillis int `toml:"read_byte_timeout" comment:"таймаут окончания ответа, мс"`
	MaxAttemptsRead       int `toml:"max_attempts_read" comment:"число попыток получения ответа"`
}

var ErrProtocol = merry.New("serial protocol failed")

func GetResponse(request Request, ctx context.Context) ([]byte, error) {
	if request.Config.MaxAttemptsRead < 1 {
		request.Config.MaxAttemptsRead = 1
	}

	t := time.Now()
	response, strResult, err := request.getResponse(ctx)

	if merry.Is(err, context.DeadlineExceeded) {
		err = merry.WithMessage(err, "нет ответа")
	} else if merry.Is(err, context.Canceled) {
		err = merry.WithMessage(err, "прервано")
	}

	logArgs := []interface{}{
		structlog.KeyTime, time.Now().Format("15:04:05"),
		"duration", durafmt.Parse(time.Since(t)),
	}

	if err == nil {
		if len(strResult) > 0 {
			logArgs = append(logArgs, "результат", strResult)
		}
		request.Logger.Debug(fmt.Sprintf("[% X] --> [% X]", request.Bytes, response), logArgs...)
	} else {
		logArgs = append(logArgs,
			"запрос", request.Bytes,
			"ответ", response,
			"config", request.Config,
		)
		request.Logger.PrintErr(err, logArgs...)
	}

	return response, err
}

type result struct {
	response []byte
	err      error
}

func (x Request) getResponse(mainContext context.Context) ([]byte, string, error) {

	var lastError error

	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {

		logArgs := []interface{}{
			"попытка", attempt + 1,
			"запрос", x.Bytes,
		}

		t := time.Now()

		if err := x.write(); err != nil {
			return nil, "", err
		}
		ctx, _ := context.WithTimeout(mainContext, x.Config.ReadTimeout())
		c := make(chan result)

		go x.waitForResponse(ctx, c)

		select {

		case r := <-c:

			logArgs = append(logArgs,
				structlog.KeyTime, time.Now().Format("15:04:05.000"),
				"duration", durafmt.Parse(time.Since(t)),
				"ответ", r.response)

			if r.err != nil {
				return nil, "", x.Logger.Err(r.err, logArgs...)
			}

			strResult := ""
			if x.ResponseParser != nil {
				strResult, r.err = x.ResponseParser(x.Bytes, r.response)
			}

			if merry.Is(r.err, ErrProtocol) {
				lastError = x.Logger.Err(r.err, logArgs...)
				time.Sleep(x.Config.ReadByteTimeout())
				continue
			}
			if r.err != nil {
				return r.response, strResult, x.Logger.Err(r.err, logArgs...)
			}

			return r.response, strResult, nil

		case <-ctx.Done():

			logArgs = append(logArgs,
				structlog.KeyTime, time.Now().Format("15:04:05"),
				"duration", durafmt.Parse(time.Since(t)))

			lastError = x.Logger.Err(ctx.Err(), logArgs...)

			switch ctx.Err() {

			case context.DeadlineExceeded:
				continue

			case context.Canceled:
				return nil, "", context.Canceled

			default:
				return nil, "", lastError
			}
		}
	}
	return nil, "", lastError

}

func (x Request) write() error {

	t := time.Now()
	writtenCount, err := x.ReadWriter.Write(x.Bytes)
	for ; err == nil && writtenCount == 0 && time.Since(t) < x.Config.ReadTimeout(); writtenCount, err = x.ReadWriter.Write(x.Bytes) {
		// COMPORT PENDING
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return err
	}

	if writtenCount != len(x.Bytes) {

		return structlog.New().Err(merry.New("не все байты были записаны"),
			structlog.KeyTime, time.Now().Format("15:04:05"),
			"число_записаных_байт", writtenCount,
			"общее_число_байт", len(x.Bytes),
			"продолжительность_записи", durafmt.Parse(time.Since(t)))
	}
	return err
}

func (x Request) waitForResponse(ctx context.Context, c chan result) {

	var response []byte
	ctxReady := context.Background()

	for {
		select {

		case <-ctx.Done():
			return

		case <-ctxReady.Done():
			c <- result{response, nil}
			return

		default:
			bytesToReadCount, err := x.ReadWriter.BytesToReadCount()
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}

			if bytesToReadCount == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			b, err := x.read(bytesToReadCount)
			if err != nil {
				c <- result{response, merry.WithMessagef(err, "[% X]", response)}
				return
			}
			response = append(response, b...)
			ctx = context.Background()
			ctxReady, _ = context.WithTimeout(context.Background(), x.Config.ReadByteTimeout())
		}
	}
}

func (x Request) read(bytesToReadCount int) ([]byte, error) {
	b := make([]byte, bytesToReadCount)
	readCount, err := x.ReadWriter.Read(b)
	if err != nil {
		return nil, merry.Wrap(err)
	}
	if readCount != bytesToReadCount {
		return nil, structlog.New().Err(merry.New("не все байты были считаны"),
			structlog.KeyTime, time.Now().Format("15:04:05"),
			structlog.KeyStack, structlog.Auto,
			"ожидаемое_число_байт", bytesToReadCount,
			"число_считаных_байт", readCount,
			"ответ", fmt.Sprintf("% X", b[:readCount]))
	}
	return b, nil
}

func (x Config) ReadTimeout() time.Duration {
	return time.Duration(x.ReadTimeoutMillis) * time.Millisecond
}

func (x Config) ReadByteTimeout() time.Duration {
	return time.Duration(x.ReadByteTimeoutMillis) * time.Millisecond
}
