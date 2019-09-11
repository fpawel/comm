package main

// программа для простой проверки modbus
// -port=COM2 -addr=17 -data="00 02 00 02" -cmd=3 -rand=0 -cycle=5

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/comport"
	"github.com/fpawel/comm/modbus"
	"github.com/powerman/structlog"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

func main() {

	var (
		strData     string
		repeatCount int
		req         modbus.Request
		cComm       comm.Config
		cPort       comport.Config
	)

	cPort.Name = pStr(flag.String("port", "COM1", "имя компорта"))
	cPort.Baud = pInt(flag.Int("boud", 9600, "скорость передачи, бод"))

	flag.IntVar(&repeatCount, "cycle", 1, "количество повторений")

	cComm.ReadByteTimeoutMillis = pInt(flag.Int("timeout.byte", 1000, "таймаут ответа, мс"))
	cComm.ReadTimeoutMillis = pInt(flag.Int("timeout", 50, "таймаут байта, мс"))
	cComm.MaxAttemptsRead = pInt(flag.Int("attempts", 1, "предел чесла попыток получения ответа"))
	randBytesCount := flag.Int("rand", 0, "количество сгенерированных байт данных со случайным значением")
	flag.StringVar(&strData, "data", "", "байты данных запроса в формате hex")

	req.ProtoCmd = modbus.ProtoCmd(pInt(flag.Int("cmd", 0, "код команды")))
	req.Addr = modbus.Addr(pInt(flag.Int("addr", 1, "адрес модбас")))
	logLevel := flag.String("log.level", "debug", "уровень логгирования (debug|info|warn|err)")

	flag.Parse()

	structlog.DefaultLogger.
		SetPrefixKeys(
			structlog.KeyApp, structlog.KeyPID, structlog.KeyLevel, structlog.KeyUnit, structlog.KeyTime,
		).
		SetDefaultKeyvals(
			structlog.KeyApp, filepath.Base(os.Args[0]),
			structlog.KeySource, structlog.Auto,
		).
		SetSuffixKeys(
			structlog.KeyStack,
		).
		SetSuffixKeys(structlog.KeySource).
		SetKeysFormat(map[string]string{
			structlog.KeyTime:   " %[2]s",
			structlog.KeySource: " %6[2]s",
			structlog.KeyUnit:   " %6[2]s",
		}).SetTimeFormat("15:04:05").
		SetLogLevel(structlog.ParseLevel(*logLevel))

	log := structlog.New()

	if req.ProtoCmd == 0 {
		log.Fatal("не задан код команды")
		return
	}

	if len(strData) > 0 {
		r := regexp.MustCompile(`[\dABCDEFabcdef]{2}`)
		tokens := r.FindAllString(strData, -1)
		if len(tokens) == 0 {
			log.Fatal("не верный формат строки данных запроса: " + strData)
			return
		}
		for i, str := range r.FindAllString(strData, -1) {
			v, err := strconv.ParseInt(str, 16, 9)
			if err != nil {
				log.Fatal(fmt.Sprintf("не верный формат строки данных запроса: %q: байт в позиции %d: %q: %v", strData, i, str, err))
				return
			}
			req.Data = append(req.Data, byte(v))
		}
	}
	if *randBytesCount > 0 {
		rndSrc := rand.NewSource(time.Now().UnixNano())
		rnd := rand.New(rndSrc)
		xs := make([]byte, *randBytesCount)
		rnd.Read(xs)
		req.Data = append(req.Data, xs...)
	}

	port := comport.NewPort(func() comport.Config { return cPort })
	defer log.ErrIfFail(port.Close)

	portReader := port.NewResponseReader(context.Background(), cComm)

	for i := 0; repeatCount == 0 || i < repeatCount; i++ {
		t := time.Now()
		bytes, err := req.GetResponse(log, portReader, func(_, _ []byte) (string, error) {
			return "", nil
		})
		if err != nil {
			fmt.Printf("[%d] %v %v\n", i+1, err, time.Since(t))
		} else {
			fmt.Printf("[%d] %v\n%v", i+1, time.Since(t), hex.Dump(bytes))
		}
	}
}

func pStr(p *string) string {
	return *p
}
func pInt(p *int) int {
	return *p
}
