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

type Logger = *structlog.Logger

type ReadWriter interface {
	Read(ctx context.Context, p []byte) (n int, err error)
	Write(ctx context.Context, p []byte) (n int, err error)
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

const (
	LogKeyDeviceValue = "device_value"
)

func GetResponse(log Logger, ctx context.Context, readWriter ReadWriter, request Request) ([]byte, error) {
	if request.Config.MaxAttemptsRead < 1 {
		request.Config.MaxAttemptsRead = 1
	}
	t := time.Now()

	respReader := responseReader{Request: request, ReadWriter: readWriter}

	response, strResult, attempt, err := respReader.getResponse(ctx)
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
	err = merry.
		Appendf(err, "запрорс % X", request.Bytes).
		Appendf("попытка %d из %d", attempt, request.Config.MaxAttemptsRead).
		Appendf("таймаут ответа %d мс", request.Config.ReadTimeoutMillis).
		Appendf("таймаут байта %d мс", request.Config.ReadByteTimeoutMillis)

	if dur := time.Since(t); dur >= time.Second {
		err = merry.Append(err, durafmt.Parse(dur).String())
	}
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ % X", response)
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

func (x responseReader) getResponse(mainContext context.Context) ([]byte, string, int, error) {

	var lastError error
	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {

		if err := x.write(mainContext); err != nil {
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

func (x responseReader) write(ctx context.Context) error {
	t := time.Now()
	writtenCount, err := x.ReadWriter.Write(ctx, x.Bytes)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < x.Config.ReadTimeout(); writtenCount, err = x.ReadWriter.Write(ctx, x.Bytes) {
		// COMPORT PENDING
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return merry.Wrap(err)
	}
	if writtenCount != len(x.Bytes) {
		return fmt.Errorf("записано %d байт из %d", writtenCount, len(x.Bytes))
	}
	return err
}

func (x responseReader) waitForResponse(ctx context.Context, c chan result) {

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
			b, err := x.read(ctx, bytesToReadCount)
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

func (x responseReader) read(ctx context.Context, bytesToReadCount int) ([]byte, error) {

	b := make([]byte, bytesToReadCount)
	readCount, err := x.ReadWriter.Read(ctx, b)
	if err != nil {
		return nil, merry.Wrap(err)
	}
	if readCount != bytesToReadCount {
		return nil, fmt.Errorf("считано %d байт из %d: % X", readCount, bytesToReadCount, b[:readCount])
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

func isLogAnswersEnabled(log Logger) bool {
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
			log.Info("посылки не будут выводиться в консоль: " + env + "=" + v)
			atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, 0)
			return false
		}
		log.Info("посылки будут выводиться в консоль: " + env + "=" + v)
		atomic.StoreInt32(&enableLogAnswersInitAtomicFlag, 1)
		return true
	}
}

func logAnswer(log Logger, request, response []byte, strResult string, attempt int, duration time.Duration, err error) {
	str := fmt.Sprintf("% X --> % X %s", request, response, durafmt.Parse(duration))
	if len(response) == 0 {
		str = fmt.Sprintf("% X %s", request, durafmt.Parse(duration))
	}
	if len(strResult) > 0 {
		log = gohelp.LogPrependSuffixKeys(log, LogKeyDeviceValue, strResult)
	}
	if attempt > 0 {
		str += fmt.Sprintf(": попытка %d", attempt)
	}
	if err == nil {
		log.Info(str)
		return
	}
	if merry.Is(err, context.Canceled) {
		log.Warn(str + ": прервано")
		return
	}
	str += ": " + err.Error()
	stack := myfmt.FormatMerryStacktrace(err)
	if len(stack) > 0 {
		str += stack
	}
	log.PrintErr(str + ": " + err.Error())
}

var (
	enableLogAnswersInitAtomicFlag int32 = -1
)
