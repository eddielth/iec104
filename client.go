package iec104

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const (
	StartByte       = 0x68             // Start character
	MinAPDULength   = 4                // Minimum APDU length (APCI only)
	MaxAPDULength   = 253              // Maximum APDU length
	MaxSeqNum       = 32767            // Maximum sequence number (15 bits)
	HeartbeatPeriod = 30 * time.Second // Heartbeat period
	MaxRetries      = 3                // Maximum retry attempts
)

// Client IEC104 client
type Client struct {
	conn         net.Conn
	addr         string
	timeout      time.Duration
	sendSeqNum   uint16     // Send sequence number
	recvSeqNum   uint16     // Receive sequence number
	responseChan chan *APDU // Response channel

	// State management
	mu     sync.RWMutex
	state  ConnectionState
	ctx    context.Context
	cancel context.CancelFunc

	// Heartbeat management
	lastActivity    time.Time
	heartbeatTicker *time.Ticker

	// Error handling
	errorChan chan error
	logger    *log.Logger

	// request-response mapping
	pending   map[string]chan *APDU // key: request ID
	pendingMu sync.Mutex

	enableLog bool
}

// NewClient creates a new IEC104 client
func NewClient(addr string, timeout time.Duration) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		addr:         addr,
		timeout:      timeout,
		responseChan: make(chan *APDU, 100),
		errorChan:    make(chan error, 10),
		state:        Disconnected,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log.New(log.Writer(), "[IEC104] ", log.LstdFlags),
		pending:      make(map[string]chan *APDU),
	}
}

// SetLogger sets the logger
func (c *Client) SetLogger(logger *log.Logger) {
	c.logger = logger
}

// GetState gets the connection state
func (c *Client) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// setState sets the connection state
func (c *Client) setState(state ConnectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != state {
		c.state = state
	}
}

// IsConnected checks if connected
func (c *Client) IsConnected() bool {
	return c.GetState() == Connected
}

// Connect connects to IEC104 server
func (c *Client) Connect() error {
	// Check current state
	currentState := c.GetState()
	if currentState != Disconnected {
		return fmt.Errorf("cannot connect: current state is %s", currentState)
	}

	c.setState(Connecting)

	// Create new response channel
	c.mu.Lock()
	if c.responseChan == nil {
		c.responseChan = make(chan *APDU, 100)
	}
	c.mu.Unlock()

	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		c.setState(Disconnected)
		return fmt.Errorf("failed to connect to server: %v", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.sendSeqNum = 0
	c.recvSeqNum = 0
	c.lastActivity = time.Now()
	c.mu.Unlock()

	// Start receive goroutine
	go c.receiveHandler()

	// Start heartbeat goroutine
	go c.heartbeatHandler()

	// Send startup frame
	if err := c.sendUFrame(UFrameStartDTAct); err != nil {
		c.Disconnect()
		return fmt.Errorf("failed to send startup frame: %v", err)
	}

	// Wait for confirmation
	select {
	case resp := <-c.responseChan:
		if resp.UControl != UFrameStartDTCfm {
			c.Disconnect()
			return errors.New("server did not confirm startup frame")
		}
		c.setState(Connected)
		c.logf("Successfully connected to server")

	case <-time.After(c.timeout):
		c.Disconnect()
		return errors.New("timeout waiting for startup confirmation")

	case <-c.ctx.Done():
		c.Disconnect()
		return errors.New("connection was cancelled")
	}

	return nil
}

// Disconnect disconnects from the server
func (c *Client) Disconnect() error {
	// Stop heartbeat
	if c.heartbeatTicker != nil {
		c.heartbeatTicker.Stop()
		c.heartbeatTicker = nil
	}

	// Try to send stop frame, but do not wait for response
	_ = c.sendUFrame(UFrameStopDTAct)

	// Wait a short while to allow stop frame to be sent
	time.Sleep(50 * time.Millisecond)

	var err error
	c.mu.Lock()
	if c.conn != nil {
		// Close connection
		err = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()

	// Wait for receive handler to finish
	time.Sleep(100 * time.Millisecond)

	c.setState(Disconnected)
	return err
}

// Close closes the client
func (c *Client) Close() error {
	// Cancel context
	c.cancel()

	// Ensure all goroutines receive the cancel signal
	time.Sleep(100 * time.Millisecond)

	// Disconnect
	return c.Disconnect()
}

// UFrameCommand U-frame command type
type UFrameCommand uint8

const (
	UFrameStartDTAct UFrameCommand = 0x07 // Start activation
	UFrameStartDTCfm UFrameCommand = 0x0B // Start confirmation
	UFrameStopDTAct  UFrameCommand = 0x13 // Stop activation
	UFrameStopDTCfm  UFrameCommand = 0x23 // Stop confirmation
	UFrameTestDTAct  UFrameCommand = 0x43 // Test activation
	UFrameTestDTCfm  UFrameCommand = 0x83 // Test confirmation
)

// sendUFrame sends UFrameCommand
func (c *Client) sendUFrame(cmd UFrameCommand) error {
	c.mu.Lock()
	state := c.state
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return errors.New("connection closed")
	}

	if state != Connected && cmd != UFrameStartDTAct {
		return errors.New("connection not established")
	}

	apdu := &APDU{
		Type:     UFrame,
		UControl: cmd,
	}

	data, err := c.encodeAPDU(apdu)
	if err != nil {
		return err
	}

	_, err = conn.Write(data)
	return err
}

func (c *Client) sendSFrame() error {
	if !c.IsConnected() {
		return errors.New("connection not established")
	}

	var recvSeq uint16
	c.mu.Lock()
	recvSeq = c.recvSeqNum
	c.mu.Unlock()

	apdu := &APDU{
		Type:    SFrame,
		RecvSeq: recvSeq,
	}

	data, err := c.encodeAPDU(apdu)
	if err != nil {
		return err
	}

	var conn net.Conn
	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()

	if conn == nil {
		return errors.New("connection closed")
	}

	_, err = conn.Write(data)
	if err == nil {
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	}
	return err
}

func (c *Client) sendIFrame(asdu *ASDU) error {
	if !c.IsConnected() {
		return errors.New("connection not established")
	}

	var sendSeq, recvSeq uint16
	c.mu.Lock()
	// Send using the current sequence number and increase it after sending
	sendSeq = c.sendSeqNum
	recvSeq = c.recvSeqNum
	c.sendSeqNum = (c.sendSeqNum + 1) & 0x7FFF
	c.mu.Unlock()

	apdu := &APDU{
		Type:    IFrame,
		SendSeq: sendSeq,
		RecvSeq: recvSeq,
		ASDU:    asdu,
	}

	data, err := c.encodeAPDU(apdu)
	if err != nil {
		return err
	}

	var conn net.Conn
	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()

	if conn == nil {
		return errors.New("connection closed")
	}
	_, err = conn.Write(data)
	if err == nil {
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	}
	return err
}

func (c *Client) sendIFrameWithResp(asdu *ASDU) (chan *APDU, string, error) {
	if !c.IsConnected() {
		return nil, "", errors.New("connection not established")
	}

	var sendSeq, recvSeq uint16
	c.mu.Lock()
	sendSeq = c.sendSeqNum
	recvSeq = c.recvSeqNum
	c.sendSeqNum = (c.sendSeqNum + 1) & 0x7FFF
	c.mu.Unlock()

	apdu := &APDU{
		Type:    IFrame,
		SendSeq: sendSeq,
		RecvSeq: recvSeq,
		ASDU:    asdu,
	}

	key := makeRequestKey(asdu.CommonAddr, asdu.InfoObjects[0].Address)
	respChan := make(chan *APDU, 2)
	c.pendingMu.Lock()
	c.pending[key] = respChan
	c.pendingMu.Unlock()

	data, err := c.encodeAPDU(apdu)
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
		return nil, key, err
	}

	var conn net.Conn
	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()

	if conn == nil {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
		return nil, key, errors.New("connection closed")
	}
	_, err = conn.Write(data)
	if err == nil {
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	} else {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}
	return respChan, key, err
}

// receiveHandler handles incoming messages
func (c *Client) receiveHandler() {
	defer func() {
		if r := recover(); r != nil {
			c.logf("Receive handler panic: %v", r)
		}
		c.setState(Disconnected)
	}()

	buf := make([]byte, 1024)
	recvBuf := make([]byte, 0, 1024)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Get connection using read lock
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		n, err := conn.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				c.logf("Read error: %v", err)
			}

			// Set state before closing connection
			c.setState(Disconnected)

			// Close connection using write lock
			c.mu.Lock()
			if c.conn != nil {
				c.conn.Close()
				c.conn = nil
			}
			c.mu.Unlock()

			// Try reconnect if auto-reconnect enabled
			if c.GetState() == Disconnected {
				go func() {
					if err := c.Reconnect(); err != nil {
						c.logf("Reconnect failed: %v", err)
					}
				}()
			}
			return
		}

		// Update last activity
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()

		recvBuf = append(recvBuf, buf[:n]...)

		for len(recvBuf) >= 2 {
			// Check startup characters
			if recvBuf[0] != StartByte {
				// Find next start character
				startIdx := bytes.IndexByte(recvBuf, StartByte)
				if startIdx == -1 {
					recvBuf = recvBuf[:0]
					break
				}
				recvBuf = recvBuf[startIdx:]
				continue
			}

			// Check length
			if len(recvBuf) < 2 {
				break
			}

			length := int(recvBuf[1])
			if len(recvBuf) < length+2 {
				break // Wait for more data
			}

			// Decode APDU
			apdu, err := c.decodeAPDU(recvBuf[:length+2])
			if err != nil {
				c.logf("APDU decode error: %v", err)
				recvBuf = recvBuf[length+2:]
				continue
			}

			// Priority: If it is an I-frame, try routing to pending
			pendingHandled := false
			if apdu.Type == IFrame && apdu.ASDU != nil && len(apdu.ASDU.InfoObjects) > 0 {
				key := makeRequestKey(apdu.ASDU.CommonAddr, apdu.ASDU.InfoObjects[0].Address)
				c.pendingMu.Lock()
				if ch, ok := c.pending[key]; ok {
					select {
					case ch <- apdu:
					default:
					}
					delete(c.pending, key)
					pendingHandled = true
				}
				c.pendingMu.Unlock()
			}
			// Only hand it over to handleAPDU when there is no pending
			if !pendingHandled {
				c.handleAPDU(apdu)
			}

			// Remove processed data
			recvBuf = recvBuf[length+2:]
		}
	}
}

// handleAPDU handles received APDU
func (c *Client) handleAPDU(apdu *APDU) {
	switch apdu.Type {
	case IFrame:
		if apdu.ASDU != nil {

			// Update expected sequence number
			c.mu.Lock()
			c.recvSeqNum = (apdu.SendSeq + 1) & 0x7FFF
			c.mu.Unlock()

			// Send S frame confirmation
			if err := c.sendSFrame(); err != nil {
				c.logf("Send S frame confirmation failed: %v", err)
				if err == io.EOF {
					return
				}
			}
		}

		select {
		case c.responseChan <- apdu:
		default:
			c.logf("Response channel is full, dropping message")
		}

	case SFrame:
		// Handle S frame
		c.logf("Received S frame: %d", apdu.RecvSeq)
		// Update confirmation status
		c.mu.Lock()
		if apdu.RecvSeq > c.sendSeqNum {
			c.sendSeqNum = apdu.RecvSeq
		}
		c.mu.Unlock()

	case UFrame:
		// Handle U frame
		switch apdu.UControl {
		case UFrameStartDTAct:
			// Received start activation, reply with start confirmation
			if err := c.sendUFrame(UFrameStartDTCfm); err != nil {
				c.logf("Send start confirmation failed: %v", err)
			}
		case UFrameStartDTCfm:
			c.logf("Received start confirmation")
			select {
			case c.responseChan <- apdu:
			default:
				c.logf("Response channel is full, dropping message")
			}
		case UFrameTestDTAct:
			if err := c.sendUFrame(UFrameTestDTCfm); err != nil {
				c.logf("Send test confirmation failed: %v", err)
				if err == io.EOF {
					return
				}
			}
			c.mu.Lock()
			c.lastActivity = time.Now()
			c.mu.Unlock()
		case UFrameTestDTCfm:
			c.logf("Received test confirmation")
			c.mu.Lock()
			c.lastActivity = time.Now()
			c.mu.Unlock()
		default:
			select {
			case c.responseChan <- apdu:
			default:
				c.logf("Response channel is full, dropping message")
			}
		}
	}
}

func (c *Client) heartbeatHandler() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !c.IsConnected() {
				continue
			}

			c.mu.RLock()
			lastActivity := c.lastActivity
			c.mu.RUnlock()

			if time.Since(lastActivity) > HeartbeatPeriod {
				c.logf("Heartbeat timeout, sending test frame")
				if err := c.sendUFrame(UFrameTestDTAct); err != nil {
					c.logf("Heartbeat timeout, sending test frame failed: %v", err)
					c.setState(Disconnected)

					go func() {
						for i := 0; i < MaxRetries; i++ {
							c.logf("Heartbeat timeout, trying to reconnect #%d", i+1)
							if err := c.Connect(); err != nil {
								c.logf("Heartbeat timeout, reconnect failed: %v", err)
								time.Sleep(time.Second * time.Duration(i+1))
								continue
							}
							c.logf("Heartbeat timeout, reconnect success")
							return
						}
						c.logf("Heartbeat timeout, reconnect failed times exceed max retries(%d)", MaxRetries)
					}()
				}
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// GeneralInterrogation general interrogation
func (c *Client) GeneralInterrogation(commonAddr uint16) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	asdu := &ASDU{
		TypeID:     TypeInterrogationCmd,
		Variable:   0x01, // SQ=0, count=1
		Cause:      CauseAct,
		CommonAddr: commonAddr,
		InfoObjects: []InfoObject{
			{
				Address: 0,                  // General interrogation address usually 0
				Value:   byte(CauseInrogen), // 20: General interrogation qualifier
			},
		},
	}

	if err := c.sendIFrame(asdu); err != nil {
		return fmt.Errorf("failed to send general interrogation command: %v", err)
	}

	c.logf("General interrogation command sent")

	// Wait for confirmation and completion
	confirmReceived := false
	terminationReceived := false

	for {
		select {
		case resp := <-c.responseChan:

			c.logf("General interrogation completed, data: %v", resp)

			if resp.ASDU == nil || resp.ASDU.TypeID != TypeInterrogationCmd {
				continue // Ignore non-interrogation responses
			}

			switch resp.ASDU.Cause {
			case CauseActCon:
				c.logf("General interrogation confirmed")
				confirmReceived = true
			case CauseActTerm:
				c.logf("General interrogation completed")
				terminationReceived = true
			case CauseInrogen:
				c.logf("Received general interrogation response data")
			}

			// If confirmation and termination received, interrogation complete
			if confirmReceived && terminationReceived {
				return nil
			}

		case <-time.After(c.timeout):
			if !confirmReceived {
				return errors.New("timeout waiting for general interrogation confirmation")
			}
			return errors.New("timeout waiting for general interrogation completion")

		case <-c.ctx.Done():
			return errors.New("operation cancelled")
		}
	}
}

// ReadMeasuredValue reads a measured value
func (c *Client) ReadMeasuredValue(commonAddr uint16, objAddr uint32) (interface{}, byte, error) {
	if !c.IsConnected() {
		return 0, 0, errors.New("not connected")
	}

	asdu := &ASDU{
		TypeID:     TypeReadCommand,
		Variable:   0x01,
		Cause:      CauseReq,
		CommonAddr: commonAddr,
		InfoObjects: []InfoObject{
			{
				Address: objAddr,
			},
		},
	}

	respChan, key, err := c.sendIFrameWithResp(asdu)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to send read command: %v", err)
	}

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()

	for {
		select {
		case resp := <-respChan:
			if resp.ASDU == nil || len(resp.ASDU.InfoObjects) == 0 {
				continue
			}
			if resp.ASDU.TypeID == TypeReadCommand && resp.ASDU.Cause == CauseActCon {
				continue // wait for next
			}
			for _, obj := range resp.ASDU.InfoObjects {
				if obj.Address == objAddr {
					return obj.Value, obj.Quality, nil
				}
			}
			return 0, 0, errors.New("no data was found for the corresponding address")
		case <-timer.C:
			return 0, 0, errors.New("timeout waiting for measured value response")
		case <-c.ctx.Done():
			return 0, 0, errors.New("operation cancelled")
		}
	}
}

func (c *Client) SendSingleCommand(addr uint16, objAddr uint32, value bool, selectExec bool) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	var cmdValue byte
	if value {
		cmdValue = 0x01
	} else {
		cmdValue = 0x00
	}

	// Add select/execute flag
	if selectExec {
		cmdValue |= 0x80 // Select
	}

	cause := CauseAct
	if selectExec {
		cause = CauseAct // Activate (select)
	}

	asdu := &ASDU{
		TypeID:     TypeSingleCommand,
		Variable:   0x01,
		Cause:      uint16(cause),
		CommonAddr: addr,
		InfoObjects: []InfoObject{
			{
				Address: objAddr,
				Value:   cmdValue,
			},
		},
	}

	if err := c.sendIFrame(asdu); err != nil {
		return fmt.Errorf("failed to send control command: %v", err)
	}

	// Wait for confirmation
	select {
	case resp := <-c.responseChan:
		if resp.ASDU == nil {
			return errors.New("invalid response")
		}

		expectedCause := CauseActCon
		if resp.ASDU.Cause != uint16(expectedCause) {
			return fmt.Errorf("control command not confirmed, cause: 0x%02X", resp.ASDU.Cause)
		}

		c.logf("control command confirmed: address=%d, value=%t", objAddr, value)
		return nil

	case <-time.After(c.timeout):
		return errors.New("control command confirmation timeout")

	case <-c.ctx.Done():
		return errors.New("operation cancelled")
	}
}

func (c *Client) SendDoubleCommand(addr uint16, obj uint32, val byte) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	asdu := &ASDU{
		TypeID:     TypeDoubleCommand,
		Variable:   0x01,
		Cause:      CauseAct,
		CommonAddr: addr,
		InfoObjects: []InfoObject{
			{
				Address: obj,
				Value:   val & 0x03, // only use lower 2 bits
			},
		},
	}

	if err := c.sendIFrame(asdu); err != nil {
		return fmt.Errorf("failed to send double point control command: %v", err)
	}

	// Wait for confirmation
	select {
	case resp := <-c.responseChan:
		if resp.ASDU == nil {
			return errors.New("invalid response")
		}

		if resp.ASDU.Cause != CauseActCon {
			return fmt.Errorf("double point control command not confirmed, cause: 0x%02X", resp.ASDU.Cause)
		}

		c.logf("double point control command confirmed: address=%d, value=%d", obj, val)
		return nil

	case <-time.After(c.timeout):
		return errors.New("double point control command confirmation timeout")

	case <-c.ctx.Done():
		return errors.New("operation cancelled")
	}
}

func (c *Client) SendSetpointCommand(addr uint16, obj uint32, val float32) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	asdu := &ASDU{
		TypeID:     TypeSetpointCommandSFA,
		Variable:   0x01,
		Cause:      CauseAct,
		CommonAddr: addr,
		InfoObjects: []InfoObject{
			{
				Address: obj,
				Value:   val,
			},
		},
	}

	if err := c.sendIFrame(asdu); err != nil {
		return fmt.Errorf("failed to send setpoint command: %v", err)
	}

	// Wait for confirmation
	select {
	case resp := <-c.responseChan:
		if resp.ASDU == nil {
			return errors.New("invalid response")
		}

		if resp.ASDU.Cause != CauseActCon {
			return fmt.Errorf("setpoint command not confirmed, cause: 0x%02X", resp.ASDU.Cause)
		}

		c.logf("setpoint command confirmed: address=%d, value=%.2f", obj, val)
		return nil

	case <-time.After(c.timeout):
		return errors.New("setpoint command confirmation timeout")

	case <-c.ctx.Done():
		return errors.New("operation cancelled")
	}
}

func (c *Client) ClockSynchronization(addr uint16) error {
	if !c.IsConnected() {
		return errors.New("not connected")
	}

	now := time.Now()

	asdu := &ASDU{
		TypeID:     TypeClockSyncCmd,
		Variable:   0x01,
		Cause:      CauseAct,
		CommonAddr: addr,
		InfoObjects: []InfoObject{
			{
				Address: 0,
				Time:    &now,
			},
		},
	}

	if err := c.sendIFrame(asdu); err != nil {
		return fmt.Errorf("failed to send clock sync command: %v", err)
	}

	// Wait for confirmation
	select {
	case resp := <-c.responseChan:
		if resp.ASDU == nil {
			return errors.New("invalid response")
		}

		if resp.ASDU.Cause != CauseActCon {
			return fmt.Errorf("clock sync command not confirmed, cause: 0x%02X", resp.ASDU.Cause)
		}

		c.logf("clock synchronization successful")
		return nil

	case <-time.After(c.timeout):
		return errors.New("clock sync confirmation timeout")

	case <-c.ctx.Done():
		return errors.New("operation cancelled")
	}
}

// GetResponseChannel returns the response channel
func (c *Client) GetResponseChannel() chan *APDU {
	return c.responseChan
}

// GetErrorChannel returns the error channel
func (c *Client) GetErrorChannel() chan error {
	return c.errorChan
}

// Reconnect implements reconnection functionality
func (c *Client) Reconnect() error {
	c.logf("starting reconnection...")

	// Disconnect existing connection
	c.Disconnect()

	// Wait before reconnecting
	time.Sleep(time.Second * 2)

	// Try to reconnect
	for i := 0; i < MaxRetries; i++ {
		if err := c.Connect(); err != nil {
			c.logf("reconnection failed (attempt %d/%d): %v", i+1, MaxRetries, err)
			if i < MaxRetries-1 {
				time.Sleep(time.Second * 5)
			}
			continue
		}

		c.logf("reconnection successful")
		return nil
	}

	return fmt.Errorf("reconnection failed after %d attempts", MaxRetries)
}

// StartAutoReconnect starts auto reconnection
func (c *Client) StartAutoReconnect() {
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			if c.GetState() == Disconnected {
				c.logf("connection lost, starting auto-reconnection...")
				if err := c.Reconnect(); err != nil {
					c.logf("auto-reconnection failed: %v", err)
				}
			}

			time.Sleep(time.Second * 10) // Check every 10 seconds
		}
	}()
}

// GetStatistics gets client statistics
func (c *Client) GetStatistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"state":          c.state.String(),
		"send_seq_num":   c.sendSeqNum,
		"recv_seq_num":   c.recvSeqNum,
		"last_activity":  c.lastActivity,
		"server_address": c.addr,
		"timeout":        c.timeout,
	}
}

// WaitForData waits for specific type of data
func (c *Client) WaitForData(typeID byte, timeout time.Duration) (*ASDU, error) {
	if !c.IsConnected() {
		return nil, errors.New("not connected")
	}

	deadline := time.Now().Add(timeout)

	for {
		select {
		case resp := <-c.responseChan:
			if resp.ASDU != nil && resp.ASDU.TypeID == typeID {
				return resp.ASDU, nil
			}

		case <-time.After(time.Until(deadline)):
			return nil, errors.New("timeout waiting for data")

		case <-c.ctx.Done():
			return nil, errors.New("operation cancelled")
		}

		if time.Now().After(deadline) {
			break
		}
	}

	return nil, errors.New("timeout waiting for data")
}

func (c *Client) EnableLog() {
	c.enableLog = true
}

func (c *Client) DisableLog() {
	c.enableLog = false
}

func (c *Client) logf(format string, v ...interface{}) {
	if c.enableLog && c.logger != nil {
		c.logger.Printf(format, v...)
	}
}
