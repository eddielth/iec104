package iec104

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// InfoObject Information object
type InfoObject struct {
	Address uint32      // Information object address
	Value   interface{} // Value (varies by type identifier)
	Quality byte        // Quality descriptor
	Time    *time.Time  // Time tag (optional)
}

func (i InfoObject) String() string {
	return fmt.Sprintf("Address: 0x%04X, Value: %v, Quality: 0x%02X, Time: %v", i.Address, i.Value, i.Quality, i.Time)
}

// encodeInfoObjectValue encodes value according to typeID
func (c *Client) encodeInfoObjectValue(buf *bytes.Buffer, typeID byte, obj *InfoObject) error {
	switch typeID {
	case TypeSinglePoint: // Single point
		val, ok := obj.Value.(bool)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		if val {
			buf.WriteByte(0x01)
		} else {
			buf.WriteByte(0x00)
		}

	case TypeDoublePoint: // Double point
		val, ok := obj.Value.(byte)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		buf.WriteByte(val)

	case TypeMeasuredValueNVA: // Measured value NVA
		val, ok := obj.Value.(int16)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		binary.Write(buf, binary.LittleEndian, val)
		buf.WriteByte(obj.Quality)

	case TypeMeasuredValueSVA: // Measured value SVA
		val, ok := obj.Value.(int16)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		binary.Write(buf, binary.LittleEndian, val)
		buf.WriteByte(obj.Quality)

	case TypeMeasuredValueSFA: // Measured value SFA
		val, ok := obj.Value.(float32)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		binary.Write(buf, binary.LittleEndian, val)
		buf.WriteByte(obj.Quality)

	case TypeSingleCommand: // Single command
		val, ok := obj.Value.(byte)
		if !ok {
			return fmt.Errorf("type 0x%02X value type mismatch", typeID)
		}
		buf.WriteByte(val)

	case TypeInterrogationCmd: // Interrogation command
		buf.WriteByte(CauseInrogen) // General Summoning Qualifier

	case TypeReadCommand: // Read command
		if val, ok := obj.Value.(byte); ok {
			buf.WriteByte(val) // Reading group number
		} else {
			buf.WriteByte(0x00) // Default group number
		}

	default:
		return fmt.Errorf("type 0x%02X not supported", typeID)
	}

	return nil
}

// decodeInfoObjectValue decodes value according to typeID
func (c *Client) decodeInfoObjectValue(data []byte, typeID byte, obj *InfoObject) (int, error) {
	switch typeID {
	case TypeSinglePoint: // Single point
		if len(data) < 1 {
			return 0, errors.New("data insufficient")
		}
		obj.Value = (data[0] & 0x01) != 0
		return 1, nil

	case TypeDoublePoint: // Double point
		if len(data) < 1 {
			return 0, errors.New("data insufficient")
		}
		obj.Value = data[0] & 0x03
		return 1, nil

	case TypeMeasuredValueNVA: // Measured value NVA
		if len(data) < 3 {
			return 0, errors.New("data insufficient")
		}
		raw := int16(binary.LittleEndian.Uint16(data[0:2]))
		obj.Value = float64(raw) / 32767.0
		obj.Quality = data[2]
		return 3, nil

	case TypeMeasuredValueSVA: // Measured value SVA
		if len(data) < 3 {
			return 0, errors.New("data insufficient")
		}
		obj.Value = int16(binary.LittleEndian.Uint16(data[0:2]))
		obj.Quality = data[2]
		return 3, nil

	case TypeMeasuredValueSFA: // Measured value SFA
		if len(data) < 5 {
			return 0, errors.New("data insufficient")
		}
		obj.Value = math.Float32frombits(binary.LittleEndian.Uint32(data[0:4]))
		obj.Quality = data[4]
		return 5, nil

	case TypeIntegratedTotals: // Accumulated energy/electric energy 0x0F
		if len(data) < 5 {
			return 0, errors.New("data insufficient for integrated totals")
		}
		obj.Value = int32(binary.LittleEndian.Uint32(data[0:4]))
		obj.Quality = data[4]
		return 5, nil

	case TypeReadCommand: // Read command
		return 0, nil

	case TypeInterrogationCmd: // Interrogation command
		if len(data) < 1 {
			return 0, errors.New("data insufficient")
		}
		obj.Value = data[0] // Read the general summon qualifier
		return 1, nil

	default:
		return 0, fmt.Errorf("type 0x%02X not supported", typeID)
	}
}
