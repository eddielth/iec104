package iec104

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Common Type Identifications
const (
	TypeSinglePoint        = 0x01 // Single point indication
	TypeDoublePoint        = 0x03 // Double point indication
	TypeStepPosition       = 0x05 // Step position
	TypeBitString32        = 0x07 // 32-bit bitstring
	TypeMeasuredValueNVA   = 0x09 // Measured value, normalized value with quality
	TypeMeasuredValueSVA   = 0x0B // Measured value, scaled value with quality
	TypeMeasuredValueSFA   = 0x0D // Measured value, floating point with quality
	TypeIntegratedTotals   = 0x0F // Integrated totals
	TypeSingleCommand      = 0x2D // Single command
	TypeDoubleCommand      = 0x2E // Double command
	TypeStepCommand        = 0x2F // Step regulation command
	TypeSetpointCommandNVA = 0x30 // Setpoint command, normalized value
	TypeSetpointCommandSVA = 0x31 // Setpoint command, scaled value
	TypeSetpointCommandSFA = 0x32 // Setpoint command, floating point
	TypeInterrogationCmd   = 0x64 // General interrogation command
	TypeCounterCmd         = 0x65 // Counter interrogation command
	TypeReadCommand        = 0x66 // Read command
	TypeClockSyncCmd       = 0x67 // Clock synchronization command
	TypeTestCommand        = 0x68 // Test command
	TypeResetProcess       = 0x69 // Reset process command
	TypeDelayAcquisition   = 0x6A // Delay acquisition command
)

// Cause of Transmission Definitions
const (
	CausePerCyc      = 0x01 // Per/cyclic
	CauseBackground  = 0x02 // Background scan
	CauseSpontaneous = 0x03 // Spontaneous
	CauseInit        = 0x04 // Initialization
	CauseReq         = 0x05 // Request
	CauseAct         = 0x06 // Activation
	CauseActCon      = 0x07 // Activation confirmation
	CauseDeact       = 0x08 // Deactivation
	CauseDeactCon    = 0x09 // Deactivation confirmation
	CauseActTerm     = 0x0A // Activation termination
	CauseRetRem      = 0x0B // Return information caused by remote command
	CauseRetLoc      = 0x0C // Return information caused by local command
	CauseFile        = 0x0D // File transfer
	CauseInrogen     = 0x14 // General interrogation termination
	CauseInro1       = 0x15 // Group 1 interrogation termination
	CauseInro16      = 0x24 // Group 16 interrogation termination
	CauseReqcogen    = 0x25 // Counter general interrogation termination
	CauseReqco1      = 0x26 // Counter group 1 interrogation termination
	CauseReqco4      = 0x29 // Counter group 4 interrogation termination
)

// ASDU Application Service Data Unit
type ASDU struct {
	TypeID      byte         // Type identifier
	Variable    byte         // Variable structure qualifier
	Cause       uint16       // Cause of transmission
	CommonAddr  uint16       // Common address
	InfoObjects []InfoObject // Information objects
}

func (a *ASDU) String() string {
	return fmt.Sprintf("TypeID: 0x%02X, Variable: 0x%02X, Cause: 0x%04X, CommonAddr: 0x%04X, InfoObjects: %v", a.TypeID, a.Variable, a.Cause, a.CommonAddr, a.InfoObjects)
}

// encodeASDU encodes ASDU
func (c *Client) encodeASDU(buf *bytes.Buffer, asdu *ASDU) error {
	// Type identification
	buf.WriteByte(asdu.TypeID)

	// Variable structure qualifier
	buf.WriteByte(asdu.Variable)

	// Cause of transmission (little-endian)
	binary.Write(buf, binary.LittleEndian, asdu.Cause)

	// Common address (little-endian)
	binary.Write(buf, binary.LittleEndian, asdu.CommonAddr)

	// Information objects
	for _, obj := range asdu.InfoObjects {
		// Address (3 bytes, little-endian)
		addrBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(addrBytes, obj.Address)
		buf.Write(addrBytes[:3])

		// Value (process according to type identification)
		if err := c.encodeInfoObjectValue(buf, asdu.TypeID, &obj); err != nil {
			return err
		}
	}

	return nil
}

// decodeASDU decodes ASDU
func (c *Client) decodeASDU(data []byte) (*ASDU, error) {
	if len(data) < 6 {
		return nil, errors.New("ASDU length insufficient")
	}

	asdu := &ASDU{
		TypeID:     data[0],
		Variable:   data[1],
		Cause:      binary.LittleEndian.Uint16(data[2:4]),
		CommonAddr: binary.LittleEndian.Uint16(data[4:6]),
	}

	// Information objects
	objCount := int(asdu.Variable & 0x7F) // When SQ=0, take the lower 7 bits
	isSequence := (asdu.Variable & 0x80) != 0

	pos := 6
	var baseAddr uint32

	for i := 0; i < objCount; i++ {
		obj := InfoObject{}

		if !isSequence || i == 0 {
			// Read address (3-byte little endian)
			if pos+3 > len(data) {
				return nil, errors.New("ASDU length insufficient")
			}
			// addrBytes := append(data[pos:pos+3], 0)
			addrBytes := append([]byte{}, data[pos:pos+3]...)
			addrBytes = append(addrBytes, 0)
			obj.Address = binary.LittleEndian.Uint32(addrBytes)
			pos += 3
			baseAddr = obj.Address
		} else {
			obj.Address = baseAddr + uint32(i)
		}

		// Reading value and quality description
		valueSize, err := c.decodeInfoObjectValue(data[pos:], asdu.TypeID, &obj)
		if err != nil {
			return nil, err
		}
		pos += valueSize

		asdu.InfoObjects = append(asdu.InfoObjects, obj)
	}

	return asdu, nil
}
