package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/gohelp"
	"github.com/fpawel/gohelp/helpstr"
	"github.com/powerman/structlog"
	"time"
)

const (
	LogKeyDuration = "продолжительность"
)

type ReadWriter interface {
	Read(log *structlog.Logger, ctx context.Context, p []byte) (n int, err error)
	Write(log *structlog.Logger, ctx context.Context, p []byte) (n int, err error)
	BytesToReadCount() (int, error)
}

type Request struct {
	Bytes          []byte
	Config         Config
	ResponseParser ResponseParser
}

type ResponseParser = func(request, response []byte) (string, error)

type Config struct {
	ReadTimeoutMillis     int `toml:"read_timeout" comment:"таймаут получения ответа, мс"`
	ReadByteTimeoutMillis int `toml:"read_byte_timeout" comment:"таймаут окончания ответа, мс"`
	MaxAttemptsRead       int `toml:"max_attempts_read" comment:"число попыток получения ответа"`
}

var ErrProtocol = merry.New("serial protocol failed")

func GetResponse(log *structlog.Logger, ctx context.Context, readWriter ReadWriter, request Request) ([]byte, error) {
	if request.Config.MaxAttemptsRead < 1 {
		request.Config.MaxAttemptsRead = 1
	}

	log = gohelp.LogPrependSuffixKeys(log, "request", fmt.Sprintf("% X", request.Bytes))

	t := time.Now()

	respReader := responseReader{Request: request, ReadWriter: readWriter}

	response, strResult, attempt, err := respReader.getResponse(log, ctx)

	log = gohelp.LogPrependSuffixKeys(log,
		"response", fmt.Sprintf("% X", response),
		"attempt", attempt+1,
		LogKeyDuration, helpstr.FormatDuration(time.Since(t)))
	if len(strResult) > 0 {
		log = gohelp.LogPrependSuffixKeys(log, "result", strResult)
	}

	switch err {
	case context.DeadlineExceeded:
		err = ErrProtocol.Here().WithMessage("нет ответа")
	case context.Canceled:
		err = merry.WithMessage(err, "прервано")
	}
	if err == nil {
		if gohelp.GetEnvWithLog("COMM_LOG_ANSWERS") == "true" {
			log.Debug("answer")
		}
	}
	return response, err
}

type responseReader struct {
	Request
	ReadWriter
}

type result struct {
	response []byte
	err      error
}

func (x responseReader) getResponse(log *structlog.Logger, mainContext context.Context) ([]byte, string, int, error) {

	var lastError error

	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {

		log = gohelp.LogPrependSuffixKeys(log,
			"attempt", attempt+1,
			"request", x.Bytes)

		if err := x.write(log, mainContext); err != nil {
			return nil, "", attempt, err
		}

		if mainContext == nil {
			mainContext = context.Background()
		}

		ctx, _ := context.WithTimeout(mainContext, x.Config.ReadTimeout())
		c := make(chan result)

		go x.waitForResponse(log, ctx, c)

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
				lastError = r.err
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
				lastError = ctx.Err()
				continue

			default:
				return nil, "", attempt, ctx.Err()

			}
		}
	}
	return nil, "", x.Config.MaxAttemptsRead, lastError

}

func (x responseReader) write(log *structlog.Logger, ctx context.Context) error {

	log = gohelp.LogPrependSuffixKeys(log,
		"total_bytes_to_write", len(x.Bytes),
		structlog.KeyStack, structlog.Auto,
	)

	t := time.Now()
	writtenCount, err := x.ReadWriter.Write(log, ctx, x.Bytes)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < x.Config.ReadTimeout(); writtenCount, err = x.ReadWriter.Write(log, ctx, x.Bytes) {
		// COMPORT PENDING
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return merry.Wrap(err)
	}

	if writtenCount != len(x.Bytes) {

		return log.Err(merry.New("не все байты были записаны"),
			"written_bytes_count", writtenCount,
			LogKeyDuration, helpstr.FormatDuration(time.Since(t)),
			structlog.KeyStack, structlog.Auto)
	}
	return err
}

func (x responseReader) waitForResponse(log *structlog.Logger, ctx context.Context, c chan result) {

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
			b, err := x.read(log, ctx, bytesToReadCount)
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}
			response = append(response, b...)
			ctx = context.Background()
			ctxReady, _ = context.WithTimeout(context.Background(), x.Config.ReadByteTimeout())
		}
	}
}

func (x responseReader) read(log *structlog.Logger, ctx context.Context, bytesToReadCount int) ([]byte, error) {

	log = gohelp.LogPrependSuffixKeys(log,
		"total_bytes_to_read", bytesToReadCount,
		structlog.KeyStack, structlog.Auto,
	)

	b := make([]byte, bytesToReadCount)
	readCount, err := x.ReadWriter.Read(log, ctx, b)
	if err != nil {
		return nil, merry.Wrap(err)
	}
	if readCount != bytesToReadCount {
		return nil, log.Err(merry.New("не все байты были считаны"),
			"read_bytes_count", readCount,
			"response", fmt.Sprintf("% X", b[:readCount]),
			structlog.KeyStack, structlog.Auto)
	}
	return b, nil
}

func (x Config) ReadTimeout() time.Duration {
	return time.Duration(x.ReadTimeoutMillis) * time.Millisecond
}

func (x Config) ReadByteTimeout() time.Duration {
	return time.Duration(x.ReadByteTimeoutMillis) * time.Millisecond
}
