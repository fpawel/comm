package modbus

import (
	"fmt"
	"testing"
)

func TestPutBCD6(t *testing.T) {
	b := make([]byte, 4)
	PutBCD6(b, 12.33)
	fmt.Printf("% X\n", b)
	fmt.Println(ParseBCD6(b))

	fmt.Printf("% X\n", BCD6(-12.33))
	fmt.Println(ParseBCD6(BCD6(-12.33)))
}
