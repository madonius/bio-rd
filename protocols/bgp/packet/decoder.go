package packet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/taktv6/tflow2/convert"
)

// Decode decodes a BGP message
func Decode(buf *bytes.Buffer) (*BGPMessage, error) {
	hdr, err := decodeHeader(buf)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode header: %v", err)
	}

	body, err := decodeMsgBody(buf, hdr.Type, hdr.Length-MinLen)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode message: %v", err)
	}

	return &BGPMessage{
		Header: hdr,
		Body:   body,
	}, nil
}

func decodeMsgBody(buf *bytes.Buffer, msgType uint8, l uint16) (interface{}, error) {
	switch msgType {
	case OpenMsg:
		return decodeOpenMsg(buf)
	case UpdateMsg:
		return decodeUpdateMsg(buf, l)
	case KeepaliveMsg:
		return nil, nil // Nothing to decode in Keepalive message
	case NotificationMsg:
		return decodeNotificationMsg(buf)
	}
	return nil, fmt.Errorf("Unknown message type: %d", msgType)
}

func decodeUpdateMsg(buf *bytes.Buffer, l uint16) (*BGPUpdate, error) {
	msg := &BGPUpdate{}

	err := decode(buf, []interface{}{&msg.WithdrawnRoutesLen})
	if err != nil {
		return msg, err
	}

	msg.WithdrawnRoutes, err = decodeNLRIs(buf, uint16(msg.WithdrawnRoutesLen))
	if err != nil {
		return msg, err
	}

	err = decode(buf, []interface{}{&msg.TotalPathAttrLen})
	if err != nil {
		return msg, err
	}

	msg.PathAttributes, err = decodePathAttrs(buf, msg.TotalPathAttrLen)
	if err != nil {
		return msg, err
	}

	nlriLen := uint16(l) - 4 - uint16(msg.TotalPathAttrLen) - uint16(msg.WithdrawnRoutesLen)
	if nlriLen > 0 {
		msg.NLRI, err = decodeNLRIs(buf, nlriLen)
		if err != nil {
			return msg, err
		}
	}

	return msg, nil
}

func decodeNotificationMsg(buf *bytes.Buffer) (*BGPNotification, error) {
	msg := &BGPNotification{}

	fields := []interface{}{
		&msg.ErrorCode,
		&msg.ErrorSubcode,
	}

	err := decode(buf, fields)
	if err != nil {
		return msg, err
	}

	if msg.ErrorCode > Cease {
		return msg, fmt.Errorf("Invalid error code: %d", msg.ErrorSubcode)
	}

	switch msg.ErrorCode {
	case MessageHeaderError:
		if msg.ErrorSubcode > BadMessageType || msg.ErrorSubcode == 0 {
			return invalidErrCode(msg)
		}
	case OpenMessageError:
		if msg.ErrorSubcode > UnacceptableHoldTime || msg.ErrorSubcode == 0 || msg.ErrorSubcode == DeprecatedOpenMsgError5 {
			return invalidErrCode(msg)
		}
	case UpdateMessageError:
		if msg.ErrorSubcode > MalformedASPath || msg.ErrorSubcode == 0 || msg.ErrorSubcode == DeprecatedUpdateMsgError7 {
			return invalidErrCode(msg)
		}
	case HoldTimeExpired:
		if msg.ErrorSubcode != 0 {
			return invalidErrCode(msg)
		}
	case FiniteStateMachineError:
		if msg.ErrorSubcode != 0 {
			return invalidErrCode(msg)
		}
	case Cease:
		if msg.ErrorSubcode != 0 {
			return invalidErrCode(msg)
		}
	default:
		return invalidErrCode(msg)
	}

	return msg, nil
}

func invalidErrCode(n *BGPNotification) (*BGPNotification, error) {
	return n, fmt.Errorf("Invalid error sub code: %d/%d", n.ErrorCode, n.ErrorSubcode)
}

func decodeOpenMsg(buf *bytes.Buffer) (*BGPOpen, error) {
	msg, err := _decodeOpenMsg(buf)
	return msg.(*BGPOpen), err
}

func _decodeOpenMsg(buf *bytes.Buffer) (interface{}, error) {
	msg := &BGPOpen{}

	fields := []interface{}{
		&msg.Version,
		&msg.AS,
		&msg.HoldTime,
		&msg.BGPIdentifier,
		&msg.OptParmLen,
	}

	err := decode(buf, fields)
	if err != nil {
		return msg, err
	}

	err = validateOpen(msg)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func validateOpen(msg *BGPOpen) error {
	if msg.Version != BGP4Version {
		return BGPError{
			ErrorCode:    OpenMessageError,
			ErrorSubCode: UnsupportedVersionNumber,
			ErrorStr:     fmt.Sprintf("Unsupported version number"),
		}
	}
	if !isValidIdentifier(msg.BGPIdentifier) {
		return BGPError{
			ErrorCode:    OpenMessageError,
			ErrorSubCode: BadBGPIdentifier,
			ErrorStr:     fmt.Sprintf("Invalid BGP identifier"),
		}
	}

	return nil
}

func isValidIdentifier(id uint32) bool {
	addr := net.IP(convert.Uint32Byte(id))
	if addr.IsLoopback() {
		return false
	}

	if addr.IsMulticast() {
		return false
	}

	if addr[0] == 0 {
		return false
	}

	if addr[0] == 255 && addr[1] == 255 && addr[2] == 255 && addr[3] == 255 {
		return false
	}

	return true
}

func decodeHeader(buf *bytes.Buffer) (*BGPHeader, error) {
	hdr := &BGPHeader{}

	marker := make([]byte, MarkerLen)
	n, err := buf.Read(marker)
	if err != nil {
		return hdr, BGPError{
			ErrorCode:    Cease,
			ErrorSubCode: 0,
			ErrorStr:     fmt.Sprintf("Failed to read from buffer: %v", err.Error()),
		}
	}

	if n != MarkerLen {
		return hdr, BGPError{
			ErrorCode:    Cease,
			ErrorSubCode: 0,
			ErrorStr:     fmt.Sprintf("Unable to read marker"),
		}
	}

	for i := range marker {
		if marker[i] != 255 {
			return nil, BGPError{
				ErrorCode:    MessageHeaderError,
				ErrorSubCode: ConnectionNotSync,
				ErrorStr:     fmt.Sprintf("Invalid marker: %v", marker),
			}
		}
	}

	fields := []interface{}{
		&hdr.Length,
		&hdr.Type,
	}

	err = decode(buf, fields)
	if err != nil {
		return hdr, BGPError{
			ErrorCode:    Cease,
			ErrorSubCode: 0,
			ErrorStr:     fmt.Sprintf("%v", err.Error()),
		}
	}

	if hdr.Length < MinLen || hdr.Length > MaxLen {
		return hdr, BGPError{
			ErrorCode:    MessageHeaderError,
			ErrorSubCode: BadMessageLength,
			ErrorStr:     fmt.Sprintf("Invalid length in BGP header: %v", hdr.Length),
		}
	}

	if hdr.Type > KeepaliveMsg || hdr.Type == 0 {
		return hdr, BGPError{
			ErrorCode:    MessageHeaderError,
			ErrorSubCode: BadMessageType,
			ErrorStr:     fmt.Sprintf("Invalid message type: %d", hdr.Type),
		}
	}

	return hdr, nil
}

func decode(buf *bytes.Buffer, fields []interface{}) error {
	var err error
	for _, field := range fields {
		err = binary.Read(buf, binary.BigEndian, field)
		if err != nil {
			return fmt.Errorf("Unable to read from buffer: %v", err)
		}
	}
	return nil
}
