package iec104

import (
	"bytes"
	"errors"
	"fmt"
)

// APDU Application Protocol Data Unit
type APDU struct {
	Type     FrameType
	SendSeq  uint16        // Send sequence number (I-frame)
	RecvSeq  uint16        // Receive sequence number (I/S-frame)
	UControl UFrameCommand // U-frame control command
	ASDU     *ASDU         // Application Service Data Unit
}

// encodeAPDU encodes APDU
func (c *Client) encodeAPDU(apdu *APDU) ([]byte, error) {
	buf := new(bytes.Buffer)

	// Start character
	buf.WriteByte(StartByte)

	// Length placeholder (filled in later)
	buf.WriteByte(0)

	switch apdu.Type {
	case IFrame:
		// Control field (I-frame format) - Corrected sequence number encoding
		sendSeq := apdu.SendSeq & 0x7FFF // Ensure 15 bits
		recvSeq := apdu.RecvSeq & 0x7FFF // Ensure 15 bits

		ctrl1 := byte((sendSeq << 1) & 0xFE)
		ctrl2 := byte((sendSeq >> 7) & 0xFE)
		ctrl3 := byte((recvSeq << 1) & 0xFE)
		ctrl4 := byte((recvSeq >> 7) & 0xFE)
		buf.Write([]byte{ctrl1, ctrl2, ctrl3, ctrl4})

		// ASDU
		if apdu.ASDU != nil {
			if err := c.encodeASDU(buf, apdu.ASDU); err != nil {
				c.logf("unable to encode ASDU: %v", err)
				return nil, err
			}
		}

	case SFrame:
		// Control field (S-frame format)
		recvSeq := apdu.RecvSeq & 0x7FFF // Ensure 15 bits

		ctrl1 := byte(0x01)
		ctrl2 := byte(0x00)
		ctrl3 := byte((recvSeq << 1) & 0xFE)
		ctrl4 := byte((recvSeq >> 7) & 0xFE)
		buf.Write([]byte{ctrl1, ctrl2, ctrl3, ctrl4})

	case UFrame:
		// Control field (U-frame format)
		ctrl1 := byte(apdu.UControl)
		ctrl2 := byte(0x00)
		buf.Write([]byte{ctrl1, ctrl2, 0x00, 0x00})
	}

	// Fill length (excluding start character and length byte)
	data := buf.Bytes()
	data[1] = byte(len(data) - 2)

	c.logf("Encoded APDU: % X", data)
	return data, nil
}

// decodeAPDU decodes APDU
func (c *Client) decodeAPDU(data []byte) (*APDU, error) {
	c.logf("Decoding APDU: % X", data)
	if len(data) < 6 {
		return nil, errors.New("APDU length insufficient")
	}

	// Check start character
	if data[0] != StartByte {
		return nil, fmt.Errorf("invalid start character: 0x%02X", data[0])
	}

	// Check length
	length := int(data[1])
	if len(data) != length+2 {
		return nil, fmt.Errorf("length mismatch, declared: %d, actual: %d", length, len(data)-2)
	}

	// Parse control field
	ctrl1 := data[2]
	ctrl2 := data[3]
	ctrl3 := data[4]
	ctrl4 := data[5]

	apdu := &APDU{}

	// I-frame (lowest bit is 0)
	if (ctrl1 & 0x01) == 0 {
		apdu.Type = IFrame
		apdu.SendSeq = (uint16(ctrl1>>1) | (uint16(ctrl2&0x7F) << 7)) & 0x7FFF
		apdu.RecvSeq = (uint16(ctrl3>>1) | (uint16(ctrl4&0x7F) << 7)) & 0x7FFF

		// Parse ASDU (if present)
		if len(data) > 6 {
			var err error
			apdu.ASDU, err = c.decodeASDU(data[6:])
			if err != nil {
				return nil, fmt.Errorf("ASDU parsing failed: %v", err)
			}
		}

		// S-frame (lowest 2 bits are 01)
	} else if (ctrl1 & 0x03) == 0x01 {
		apdu.Type = SFrame
		apdu.RecvSeq = (uint16(ctrl3>>1) | (uint16(ctrl4&0x7F) << 7)) & 0x7FFF

		// U-frame (lowest 2 bits are 11)
	} else if (ctrl1 & 0x03) == 0x03 {
		apdu.Type = UFrame
		apdu.UControl = UFrameCommand(ctrl1)
	} else {
		return nil, fmt.Errorf("invalid frame type: 0x%02X", ctrl1)
	}

	return apdu, nil
}
