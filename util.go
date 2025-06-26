package iec104

import "fmt"

func makeRequestKey(commonAddr uint16, objAddr uint32) string {
	return fmt.Sprintf("%04X-%08X", commonAddr, objAddr)
}
