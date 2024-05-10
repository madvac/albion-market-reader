package photon_spectator

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	// Command types
	AcknowledgeType          = 1
	ConnectType              = 2
	VerifyConnectType        = 3
	DisconnectType           = 4
	PingType                 = 5
	SendReliableType         = 6
	SendUnreliableType       = 7
	SendReliableFragmentType = 8
	// Message types
	OperationRequest       = 2
	otherOperationResponse = 3
	EventDataType          = 4
	OperationResponse      = 7
)

type PhotonCommand struct {
	// Header
	Type                   uint8
	ChannelID              uint8
	Flags                  uint8
	ReservedByte           uint8
	Length                 int32
	ReliableSequenceNumber int32

	// Body
	Data []byte
}

type ReliableMessage struct {
	// Header
	Signature uint8
	Type      uint8

	// OperationRequest
	OperationCode uint8

	// EventData
	EventCode uint8

	// OperationResponse
	OperationResponseCode uint16
	OperationDebugString  string

	ParamaterCount int16
	Data           []byte
}

type ReliableFragment struct {
	SequenceNumber int32
	FragmentCount  int32
	FragmentNumber int32
	TotalLength    int32
	FragmentOffset int32

	Data []byte
}

// Returns a structure containing the fields of a reliable message.
// Errors if the type is not SendReliableType.
func (c PhotonCommand) ReliableMessage() (msg ReliableMessage, err error) {
	if c.Type != SendReliableType {
		return msg, fmt.Errorf("Command can't be converted")
	}

	buf := bytes.NewBuffer(c.Data)

	binary.Read(buf, binary.BigEndian, &msg.Signature)
	binary.Read(buf, binary.BigEndian, &msg.Type)

	if msg.Type > 128 {
		return msg, fmt.Errorf("Encryption not supported")
	}

	if msg.Type == otherOperationResponse {
		msg.Type = OperationResponse
	}

	switch msg.Type {
	case OperationRequest:
		binary.Read(buf, binary.BigEndian, &msg.OperationCode)
	case EventDataType:
		binary.Read(buf, binary.BigEndian, &msg.EventCode)
	case OperationResponse, otherOperationResponse:
		binary.Read(buf, binary.BigEndian, &msg.OperationCode)
		binary.Read(buf, binary.BigEndian, &msg.OperationResponseCode)
		var paramType uint8
		binary.Read(buf, binary.BigEndian, &paramType)
		paramValue := decodeType(buf, paramType)
		if paramValue != nil {
			msg.OperationDebugString = paramValue.(string)
		}
	}

	binary.Read(buf, binary.BigEndian, &msg.ParamaterCount)
	msg.Data = buf.Bytes()

	return
}

// Returns a structure containing the fields of a reliable fragment
// Errors if the type is not SendReliableFragmentType.
func (c PhotonCommand) ReliableFragment() (msg ReliableFragment, err error) {
	if c.Type != SendReliableFragmentType {
		return msg, fmt.Errorf("Command can't be converted")
	}

	buf := bytes.NewBuffer(c.Data)

	binary.Read(buf, binary.BigEndian, &msg.SequenceNumber)
	binary.Read(buf, binary.BigEndian, &msg.FragmentCount)
	binary.Read(buf, binary.BigEndian, &msg.FragmentNumber)
	binary.Read(buf, binary.BigEndian, &msg.TotalLength)
	binary.Read(buf, binary.BigEndian, &msg.FragmentOffset)

	msg.Data = buf.Bytes()

	return
}
