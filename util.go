package iec104

import (
	"fmt"
)

// IsSeqNumGreater determines whether s1 is greater than s2 (considering sequence number loop)
func IsSeqNumGreater(s1, s2, max uint16) bool {
	return (s1 > s2 && s1-s2 < max/2) || (s2 > s1 && s2-s1 > max/2)
}

// makeRequestKey generates a unique key for request-response matching using sequence number
func makeRequestKey(seqNum uint16) string {
	return fmt.Sprintf("seq:%d", seqNum)
}
