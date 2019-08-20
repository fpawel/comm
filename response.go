package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/gohelp"
	"github.com/fpawel/gohelp/myfmt"
	"github.com/hako/durafmt"
	"github.com/powerman/structlog"
	"os"
	"sync/atomic"
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

var Err = merry.New("ошибка проткола последовательной приёмопередачи")

func GetResponse(log *structlog.Logger, ctx context.Context, readWriter ReadWriter, request Request) ([]byte, error) {
	if request.Config.MaxAttemptsRead < 1 {
		request.Config.MaxAttemptsRead = 1
	}
	t := time.Now()

	respReader := responseReader{Request: request, ReadWriter: readWriter}

	response, strResult, attempt, err := respReader.getResponse(
		gohelp.LogAppendPrefixKeys(log, "request", fmt.Sprintf("`% X`", request.Bytes)), ctx)

	if isLogAnswersEnabled(log) {
		logAnswer(log, request.Bytes, response, strResult, attempt, time.Since(t), err)
	}

	if err == nil {
		return response, nil
	}
	if merry.Is(err, context.Canceled) {
		err = merry.Append(err, "прервано")
	} else if merry.Is(err, context.DeadlineExceeded) {
		err = merry.WithMessage(err, "нет ответа").WithCause(Err)
		if !merry.Is(err, Err) {
			panic("unexpected")
		}
	}
	return response, merry.
		Appendf(err, "запрорс % X", request.Bytes).
		Appendf("ответ % X", response).
		Appendf("попытка %d из %d", attempt, request.Config.MaxAttemptsRead).
		Appendf("продолжительность %s", myfmt.FormatDuration(time.Since(t))).
		Appendf("таймаут ответа %d мс", request.Config.ReadTimeoutMillis).
		Appendf("таймаут байта %d мс", request.Config.ReadByteTimeoutMillis)
}

func WithLogAnswers(fun func() error) error {
	v := atomic.LoadInt32(&enableLogAnswersInitAtomicFlag)
	defer atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, v)
	atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, 1)
	return fun()
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

		log := gohelp.LogPrependSuffixKeys(log, "attempt", attempt+1)

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

			strResult := ""
			if r.err == nil && x.ResponseParser != nil {
				strResult, r.err = x.ResponseParser(x.Bytes, r.response)
			}

			if r.err != nil {
				return r.response, "", attempt, r.err
			}

			if merry.Is(r.err, Err) {
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
			LogKeyDuration, myfmt.FormatDuration(time.Since(t)),
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

type PrintfFunc = func(msg interface{}, keyvals ...interface{})

func isLogAnswersEnabled(log *structlog.Logger) bool {
	flagValue := atomic.LoadInt32(&enableLogAnswersInitAtomicFlag)
	switch flagValue {
	case 0:
		return false
	case 1:
		return true
	default:
		const env = "COMM_LOG_ANSWERS"
		v := os.Getenv(env)
		if v != "true" {
			log.Info("посылки не будут выводиться в консоль", env, v)
			atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, 0)
			return false
		}
		log.Info("посылки будут выводиться в консоль", env, v)
		atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, 1)
		return true
	}
}

func logAnswer(log *structlog.Logger, request, response []byte, strResult string, attempt int, duration time.Duration, err error) {

	if len(strResult) > 0 {
		log = gohelp.LogPrependSuffixKeys(log, "result", strResult)
	}

	log = gohelp.LogPrependSuffixKeys(log, "duration", fmt.Sprintf("`%s`", durafmt.Parse(duration)))

	str := fmt.Sprintf("% X --> % X", request, response)
	if len(response) == 0 {
		str = fmt.Sprintf("% X", request)
	}
	logFunc := log.Info
	if err != nil {
		str = fmt.Sprintf("%s: %s", str, err)
		logFunc = log.PrintErr
		if !merry.Is(err, context.Canceled) {
			log = gohelp.LogPrependSuffixKeys(log, "stack", myfmt.FormatMerryStacktrace(err))
		}
	}
	logFunc(str)
}

var (
	enableLogAnswersInitAtomicFlag int32 = -1
)
