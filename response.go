package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/gohelp/helpstr"
	"github.com/powerman/structlog"
	"io"
	"time"
)

const (
	LogKeyDuration = "продолжительность"
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
	response, strResult, attempt, err := request.getResponse(ctx)

	logArgs := []interface{}{
		LogKeyDuration, helpstr.FormatDuration(time.Since(t)),
	}

	switch err {
	case context.DeadlineExceeded:
		err = merry.WithMessage(err, "нет ответа")
	case context.Canceled:
		err = merry.WithMessage(err, "прервано")
	}

	if err == nil {
		if len(strResult) > 0 {
			logArgs = append(logArgs, "результат", strResult)
		}
		request.Logger.Debug(fmt.Sprintf("[% X] --> [% X]", request.Bytes, response), logArgs...)
	} else {
		logArgs = append(logArgs,
			"запрос", request.Bytes,
			"таймаут_получения_ответа_мс", request.Config.ReadTimeoutMillis,
			"таймаут_окончания_ответа_мс", request.Config.ReadByteTimeoutMillis,
			"максимальное_количество_попыток_получить_ответ", request.Config.MaxAttemptsRead,
			"попытка", attempt+1,
		)
		if len(response) > 0 {
			logArgs = append(logArgs, "ответ", response)
		}

		request.Logger.PrintErr(err, logArgs...)
	}

	return response, err
}

type result struct {
	response []byte
	err      error
}

func (x Request) getResponse(mainContext context.Context) ([]byte, string, int, error) {

	var lastError error

	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {

		t := time.Now()

		if err := x.write(); err != nil {
			return nil, "", attempt, err
		}

		if mainContext == nil {
			mainContext = context.Background()
		}

		ctx, _ := context.WithTimeout(mainContext, x.Config.ReadTimeout())
		c := make(chan result)

		go x.waitForResponse(ctx, c)

		select {

		case r := <-c:

			if r.err != nil {
				return nil, "", attempt, r.err
			}

			strResult := ""
			if x.ResponseParser != nil {
				strResult, r.err = x.ResponseParser(x.Bytes, r.response)
			}

			if merry.Is(r.err, ErrProtocol) {

				logArgs := []interface{}{
					"попытка", attempt + 1,
					"запрос", x.Bytes,
					LogKeyDuration, helpstr.FormatDuration(time.Since(t)),
				}
				if len(r.response) > 0 {
					logArgs = append(logArgs, "ответ", r.response)
				}

				lastError = x.Logger.Err(r.err, logArgs...)
				x.Logger.Debug(r.err, logArgs...)
				time.Sleep(x.Config.ReadByteTimeout())
				continue
			}
			if r.err != nil {
				return r.response, strResult, attempt, r.err
			}

			return r.response, strResult, attempt, nil

		case <-ctx.Done():

			switch ctx.Err() {

			case context.DeadlineExceeded:

				logArgs := []interface{}{
					"попытка", attempt + 1,
					"запрос", x.Bytes,
					LogKeyDuration, helpstr.FormatDuration(time.Since(t)),
				}

				lastError = ctx.Err()
				x.Logger.Debug(ctx.Err(), logArgs...)

				continue

			default:
				return nil, "", attempt, ctx.Err()

			}
		}
	}
	return nil, "", x.Config.MaxAttemptsRead, lastError

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
			"число_записаных_байт", writtenCount,
			"общее_число_байт", len(x.Bytes),
			LogKeyDuration, helpstr.FormatDuration(time.Since(t)))
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
