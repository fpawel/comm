package comport

import (
	"github.com/ansel1/merry"
	"golang.org/x/sys/windows/registry"
)

func Ports() ([]string, error) {

	root, err := registry.OpenKey(registry.LOCAL_MACHINE, serialCommKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}

	ks, err := root.ReadValueNames(0)
	if err != nil {
		return nil, err
	}
	var ports []string
	for _, k := range ks {
		port, _, err := root.GetStringValue(k)
		if err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}
	return ports, nil
}

func CheckPortNameIsValid(portName string) error {
	if len(portName) == 0 {
		return merry.New("не задано имя СОМ порта")
	}
	ports, err := Ports()
	if err != nil {
		return merry.Append(err, "не удалось получить список СОМ портов, представленных в системе")
	}

	if len(ports) == 0 {
		return merry.New("СОМ порты отсутствуют")
	}
	for _, s := range ports {
		if portName == s {
			return nil
		}
	}
	return merry.Errorf("СОМ порт %q не доступен. Список доступных СОМ портов: %s", portName, ports)
}

const serialCommKey = `hardware\devicemap\serialcomm`
