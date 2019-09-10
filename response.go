package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/gohelp"
	"github.com/fpawel/gohelp/myfmt"
	"github.com/powerman/structlog"
	"io"
	"sync"
	"time"
)

type ResponseReader struct {
	ReadWriter     io.ReadWriter
	Ctx            context.Context
	Config         Config
	ResponseParser ResponseParser
}

func NewResponseReader(ctx context.Context, readWriter io.ReadWriter, config Config, responseParser ResponseParser) ResponseReader {
	return ResponseReader{
		Ctx:            ctx,
		ReadWriter:     readWriter,
		Config:         config,
		ResponseParser: responseParser,
	}
}

type Logger = *structlog.Logger

type ResponseParser = func(request, response []byte) (string, error)

type Config struct {
	ReadTimeoutMillis     int `toml:"read_timeout" comment:"таймаут получения ответа, мс"`
	ReadByteTimeoutMillis int `toml:"read_byte_timeout" comment:"таймаут окончания ответа, мс"`
	MaxAttemptsRead       int `toml:"max_attempts_read" comment:"число попыток получения ответа"`
}

var Err = merry.New("ошибка проткола последовательной приёмопередачи")

const (
	LogKeyDeviceValue = "device_value"
	LogKeyDuration    = "comm_duration"
	LogKeyAttempt     = "comm_attempt"
)

func (x ResponseReader) GetResponse(request []byte, log Logger) ([]byte, error) {
	if x.Config.MaxAttemptsRead < 1 {
		x.Config.MaxAttemptsRead = 1
	}
	response, result, err := x.getResponse(request, log)
	if err == nil {
		return response, nil
	}
	if merry.Is(err, context.Canceled) {
		err = merry.Append(err, "прервано")
	} else if merry.Is(err, context.DeadlineExceeded) {
		err = merry.Append(err, "нет ответа").WithCause(Err)
		if !merry.Is(err, Err) {
			panic("unexpected")
		}
	}
	err = merry.Appendf(err, "запрорс=`% X`", request).Appendf("comm=%+v", x.Config)
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ=`% X`", response)
	}
	if len(result) > 0 {
		err = merry.Appendf(err, "результат=%s", result)
	}
	return response, err
}

type result struct {
	response []byte
	err      error
}

func (x ResponseReader) getResponse(request []byte, log Logger) ([]byte, string, error) {
	if x.Ctx == nil {
		x.Ctx = context.Background()
	}
	var lastError error
	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {
		if err := x.write(request); err != nil {
			return nil, "", err
		}
		ctx, _ := context.WithTimeout(x.Ctx, x.Config.ReadTimeout())
		c := make(chan result)
		startWaitResponseMoment := time.Now()
		go x.waitForResponse(ctx, c)

		log := gohelp.LogPrependSuffixKeys(log,
			LogKeyDuration, time.Since(startWaitResponseMoment),
			LogKeyAttempt, attempt)

		select {

		case r := <-c:
			strResult := ""
			if r.err == nil && x.ResponseParser != nil {
				strResult, r.err = x.ResponseParser(request, r.response)
			}
			if len(strResult) > 0 {
				log = gohelp.LogPrependSuffixKeys(log, LogKeyDeviceValue, strResult)
			}
			logAnswer(log, request, r.response, r.err)
			if merry.Is(r.err, Err) {
				lastError = r.err
				time.Sleep(x.Config.ReadByteTimeout())
				continue
			}
			if r.err != nil {
				return r.response, strResult, r.err
			}

			return r.response, strResult, nil

		case <-ctx.Done():

			logAnswer(log, request, nil, ctx.Err())

			switch ctx.Err() {

			case context.DeadlineExceeded:
				lastError = ctx.Err()
				continue

			default:
				return nil, "", ctx.Err()
			}
		}
	}
	return nil, "", lastError

}

func (x ResponseReader) write(request []byte) error {
	t := time.Now()
	writtenCount, err := x.ReadWriter.Write(request)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < x.Config.ReadTimeout(); writtenCount, err = x.ReadWriter.Write(request) {
		// COMPORT PENDING
		time.Sleep(x.Config.ReadByteTimeout())
	}
	if err != nil {
		return merry.Wrap(err)
	}
	if writtenCount != len(request) {
		return fmt.Errorf("записано %d байт из %d", writtenCount, len(request))
	}
	return err
}

func (x ResponseReader) waitForResponse(ctx context.Context, c chan result) {

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
			bytesToReadCount, err := x.ReadWriter.Read(nil)
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
				c <- result{response, merry.Wrap(err)}
				return
			}
			response = append(response, b...)
			ctx = context.Background()
			ctxReady, _ = context.WithTimeout(context.Background(), x.Config.ReadByteTimeout())
		}
	}
}

func (x ResponseReader) read(bytesToReadCount int) ([]byte, error) {

	b := make([]byte, bytesToReadCount)
	readCount, err := x.ReadWriter.Read(b)
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

//func SetEnableLog(enable bool){
//	mu.Lock()
//	defer mu.Unlock()
//	enableLog = enable
//}

func logAnswer(log Logger, request, response []byte, err error) {
	if !isLogEnabled() {
		return
	}
	str := fmt.Sprintf("% X --> % X", request, response)
	if len(response) == 0 {
		str = fmt.Sprintf("% X", request)
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
	log.PrintErr(str)
}

func isLogEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enableLog
}

var (
	mu        sync.Mutex
	enableLog = true
)
