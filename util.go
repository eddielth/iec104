package iec104

import (
	"fmt"
)

// IsSeqNumGreater checks if s1 is greater than s2, considering wraparound
func IsSeqNumGreater(s1, s2, max uint16) bool {
	return (s1 > s2 && s1-s2 < max/2) || (s2 > s1 && s2-s1 > max/2)
}

func makeRequestKey(commonAddr uint16, objAddr uint32) string {
	return fmt.Sprintf("%d-%d", commonAddr, objAddr)
}
