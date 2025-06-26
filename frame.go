package iec104

// FrameType frame type
type FrameType uint8

const (
	IFrame FrameType = iota // Information frame
	SFrame                  // Supervision frame
	UFrame                  // Control frame
)
