# IEC 60870-5-104 (IEC104) Go Client Library

This project provides a Go implementation of the IEC 60870-5-104 (IEC104) protocol, commonly used for telecontrol in electrical engineering and power system automation. The library enables communication with IEC104 servers (RTUs, gateways, etc.) for SCADA and substation automation applications.

## Features

- Full IEC 60870-5-104 client implementation
- Support for connection management, heartbeat, and automatic reconnection
- General interrogation, measured value reading, and command sending (single, double, setpoint)
- Clock synchronization
- Thread-safe design with context cancellation
- Logging and statistics support

## Project Structure

- `client.go` — Main client implementation
- `apdu.go`, `asdu.go`, `frame.go`, `infoobject.go`, `util.go` — Protocol data structures and utilities
- `connection.go` — Connection state management
- `example/main.go` — Example usage
- `go.mod` — Go module definition

## Installation

```
go get github.com/eddielth/iec104
```

## Usage Example

See `example/main.go` for a complete example. Basic usage:

```go
package main

import (
    "log"
    "time"
    "github.com/eddielth/iec104"
)

func main() {
    client := iec104.NewClient("127.0.0.1:2404", 5*time.Second)
    client.EnableLog()
    err := client.Connect()
    if err != nil {
        log.Fatalf("Connect failed: %v", err)
    }
    defer client.Close()

    // General Interrogation
    err = client.GeneralInterrogation(1)
    if err != nil {
        log.Printf("General Interrogation failed: %v", err)
    }

    // Read measured value
    value, quality, err := client.ReadMeasuredValue(1, 10001)
    if err != nil {
        log.Printf("Read measured value failed: %v", err)
    } else {
        log.Printf("Measured value: %v, Quality: %v", value, quality)
    }

    // Send single command
    err = client.SendSingleCommand(1, 10001, true, false)
    if err != nil {
        log.Printf("Send single command failed: %v", err)
    }
}
```

## API Overview

- `NewClient(addr string, timeout time.Duration) *Client` — Create a new IEC104 client
- `Connect() error` — Connect to the IEC104 server
- `Close() error` — Close the connection
- `IsConnected() bool` — Check if client is connected
- `GeneralInterrogation(commonAddr uint16) error` — Execute general interrogation
- `ReadMeasuredValue(commonAddr uint16, objAddr uint32) (interface{}, byte, error)` — Read measured value
- `SendSingleCommand(addr uint16, objAddr uint32, value bool, selectExec bool) error` — Send single-point command
- `SendDoubleCommand(addr uint16, obj uint32, val byte) error` — Send double-point command
- `SendSetpointCommand(addr uint16, obj uint32, val float32) error` — Send setpoint command
- `ClockSynchronization(addr uint16) error` — Perform clock synchronization
- `EnableLog()` — Enable logging
- `DisableLog()` — Disable logging

## License

MIT License

## Disclaimer

This library is provided as-is and may require adaptation for production use. Please refer to the IEC 60870-5-104 standard for protocol details.
