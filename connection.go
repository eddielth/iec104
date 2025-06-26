package iec104

// ConnectionState represents the connection state
type ConnectionState uint8

const (
	Disconnected ConnectionState = iota
	Connecting
	Connected
	Disconnecting
)

func (s ConnectionState) String() string {
	switch s {
	case Disconnected:
		return "Disconnected"
	case Connecting:
		return "Connecting"
	case Connected:
		return "Connected"
	case Disconnecting:
		return "Disconnecting"
	default:
		return "Unknown"
	}
}
