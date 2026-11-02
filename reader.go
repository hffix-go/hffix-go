package fix

import (
	"bytes"
	"math"
	"strconv"
	"unicode/utf8"
)

const soh byte = 0x01

// Field is a tag and a view of its value in the input buffer.
type Field struct {
	Tag   Tag
	Value []byte
}

// Bytes returns the field's value without copying it.
func (f Field) Bytes() []byte {
	return f.Value
}

// Text returns the value as UTF-8 text.
func (f Field) Text() (string, error) {
	if !utf8.Valid(f.Value) {
		return "", ErrEncoding
	}
	return string(f.Value), nil
}

// Int64 parses the value as a signed base-10 integer.
func (f Field) Int64() (int64, error) {
	value, err := strconv.ParseInt(string(f.Value), 10, 64)
	if err != nil {
		return 0, ErrParse
	}
	return value, nil
}

// Uint64 parses the value as an unsigned base-10 integer.
func (f Field) Uint64() (uint64, error) {
	value, err := strconv.ParseUint(string(f.Value), 10, 64)
	if err != nil {
		return 0, ErrParse
	}
	return value, nil
}

// Float64 parses the value as a floating-point number.
func (f Field) Float64() (float64, error) {
	value, err := strconv.ParseFloat(string(f.Value), 64)
	if err != nil {
		return 0, ErrParse
	}
	return value, nil
}

// Bool parses a FIX Boolean field, whose values are Y and N.
func (f Field) Bool() (bool, error) {
	switch string(f.Value) {
	case "Y":
		return true, nil
	case "N":
		return false, nil
	default:
		return false, ErrParse
	}
}

// Decimal returns the original decimal representation when it is valid.
// Returning the wire representation preserves its precision and scale.
func (f Field) Decimal() (string, error) {
	if !utf8.Valid(f.Value) {
		return "", ErrEncoding
	}
	if !validDecimal(f.Value) {
		return "", ErrParse
	}
	return string(f.Value), nil
}

// Byte returns the first byte of the value.
func (f Field) Byte() (byte, error) {
	if len(f.Value) == 0 {
		return 0, ErrParse
	}
	return f.Value[0], nil
}

// Reader scans SOH-delimited tag=value fields without allocating or copying.
type Reader struct {
	buf []byte
	pos int
}

// NewReader creates a reader over buf.
func NewReader(buf []byte) *Reader {
	return &Reader{buf: buf}
}

// Position returns the byte offset where the next scan will begin.
func (r *Reader) Position() int {
	return r.pos
}

// Bytes returns the original input buffer.
func (r *Reader) Bytes() []byte {
	return r.buf
}

// BufferSize returns the size of the input buffer.
func (r *Reader) BufferSize() int {
	return len(r.buf)
}

// Next returns the next complete field, or false for an incomplete or
// malformed tail.
func (r *Reader) Next() (Field, bool) {
	field, next, ok := readField(r.buf, r.pos)
	if !ok {
		return Field{}, false
	}
	r.pos = next
	return field, true
}

// Find scans from the current position through the next field with tag.
// The reader advances through the field it finds.
func (r *Reader) Find(tag Tag) (Field, bool) {
	for {
		field, ok := r.Next()
		if !ok {
			return Field{}, false
		}
		if field.Tag == tag {
			return field, true
		}
	}
}

// FindWithHint scans from hint without changing the reader's position.
func (r *Reader) FindWithHint(tag Tag, hint int) (Field, bool) {
	pos := hint
	for {
		field, next, ok := readField(r.buf, pos)
		if !ok {
			return Field{}, false
		}
		if field.Tag == tag {
			return field, true
		}
		pos = next
	}
}

// NextDataField reads tag's value as exactly length bytes. This supports FIX
// raw-data fields whose contents may include SOH; length is usually read from
// the corresponding length tag first.
func (r *Reader) NextDataField(tag Tag, length int) (Field, bool) {
	if length < 0 || r.pos < 0 || r.pos > len(r.buf) {
		return Field{}, false
	}
	rest := r.buf[r.pos:]
	eq := bytes.IndexByte(rest, '=')
	if eq < 0 {
		return Field{}, false
	}
	parsedTag, ok := parseTag(rest[:eq])
	if !ok || parsedTag != tag {
		return Field{}, false
	}
	valueStart := eq + 1
	if length > len(rest)-valueStart {
		return Field{}, false
	}
	valueEnd := valueStart + length
	if valueEnd >= len(rest) || rest[valueEnd] != soh {
		return Field{}, false
	}
	field := Field{Tag: parsedTag, Value: rest[valueStart:valueEnd]}
	r.pos += valueEnd + 1
	return field, true
}

// Complete reports the end offset of a complete message beginning at the
// reader's current position. It validates BodyLength and the CheckSum trailer.
func (r *Reader) Complete() (int, bool) {
	state, end, _, _ := parseMessage(r.buf, r.pos)
	return end, state == messageComplete
}

// Valid reports whether the message is structurally valid so far. A partial
// buffer remains valid unless bytes already received disprove its structure.
func (r *Reader) Valid() bool {
	state, _, _, _ := parseMessage(r.buf, r.pos)
	return state != messageInvalid
}

// MessageType returns the MsgType field for a complete message at the current
// position.
func (r *Reader) MessageType() (Field, bool) {
	if _, ok := r.Complete(); !ok {
		return Field{}, false
	}
	pos := r.pos
	_, pos, ok := readField(r.buf, pos)
	if !ok {
		return Field{}, false
	}
	_, pos, ok = readField(r.buf, pos)
	if !ok {
		return Field{}, false
	}
	field, _, ok := readField(r.buf, pos)
	if !ok || field.Tag != MsgType {
		return Field{}, false
	}
	return field, true
}

// NextMessage returns a reader for the message following the current complete
// message. The returned reader shares the original input buffer.
func (r *Reader) NextMessage() (*Reader, bool) {
	end, ok := r.Complete()
	if !ok || end >= len(r.buf) {
		return nil, false
	}
	return NewReader(r.buf[end:]), true
}

// MessageSize returns the size of the complete message at the current
// position.
func (r *Reader) MessageSize() (int, bool) {
	end, ok := r.Complete()
	if !ok {
		return 0, false
	}
	return end - r.pos, true
}

// MessageBytes returns the complete message at the current position without
// copying it.
func (r *Reader) MessageBytes() ([]byte, bool) {
	end, ok := r.Complete()
	if !ok {
		return nil, false
	}
	return r.buf[r.pos:end], true
}

// Checksum returns the checksum byte carried by the current complete message.
func (r *Reader) Checksum() (byte, bool) {
	state, _, _, checksumStart := parseMessage(r.buf, r.pos)
	if state != messageComplete {
		return 0, false
	}
	value := r.buf[checksumStart+3 : checksumStart+6]
	checksum, ok := parseSize(value)
	return byte(checksum), ok
}

// CalculateChecksum returns the modulo-256 sum of all message bytes preceding
// tag 10.
func (r *Reader) CalculateChecksum() (byte, bool) {
	state, _, _, checksumStart := parseMessage(r.buf, r.pos)
	if state != messageComplete {
		return 0, false
	}
	var sum byte
	for _, value := range r.buf[r.pos:checksumStart] {
		sum += value
	}
	return sum, true
}

// VerifyChecksum reports whether the current complete message's checksum is
// the modulo-256 sum of all bytes preceding tag 10.
func (r *Reader) VerifyChecksum() bool {
	actual, ok := r.Checksum()
	if !ok {
		return false
	}
	calculated, ok := r.CalculateChecksum()
	return ok && calculated == actual
}

type messageState uint8

const (
	messageInvalid messageState = iota
	messagePartial
	messageComplete
)

func parseMessage(buf []byte, start int) (messageState, int, int, int) {
	if start < 0 || start > len(buf) {
		return messageInvalid, 0, 0, 0
	}

	beginString, pos, state := readExpectedField(buf, start, BeginString)
	if state != messageComplete {
		return state, 0, 0, 0
	}
	if !bytes.HasPrefix(beginString.Value, []byte("FIX")) {
		return messageInvalid, 0, 0, 0
	}

	bodyLengthField, bodyStart, state := readExpectedField(buf, pos, BodyLength)
	if state != messageComplete {
		return state, 0, 0, 0
	}
	bodyLength, ok := parseSize(bodyLengthField.Value)
	if !ok {
		return messageInvalid, 0, 0, 0
	}

	msgType, msgTypeEnd, state := readExpectedField(buf, bodyStart, MsgType)
	if state != messageComplete {
		return state, 0, 0, 0
	}
	if len(msgType.Value) == 0 || bodyLength < msgTypeEnd-bodyStart || bodyLength > len(buf)-bodyStart {
		if bodyLength > len(buf)-bodyStart {
			return messagePartial, 0, bodyStart, 0
		}
		return messageInvalid, 0, bodyStart, 0
	}

	checksumStart := bodyStart + bodyLength
	if checksumStart <= bodyStart || buf[checksumStart-1] != soh {
		return messageInvalid, 0, bodyStart, checksumStart
	}
	if len(buf)-checksumStart < 7 {
		available := buf[checksumStart:]
		prefix := []byte("10=")
		prefixLen := min(len(available), len(prefix))
		if !bytes.Equal(available[:prefixLen], prefix[:prefixLen]) {
			return messageInvalid, 0, bodyStart, checksumStart
		}
		for i := 3; i < len(available) && i < 6; i++ {
			if available[i] < '0' || available[i] > '9' {
				return messageInvalid, 0, bodyStart, checksumStart
			}
		}
		if len(available) >= 7 && available[6] != soh {
			return messageInvalid, 0, bodyStart, checksumStart
		}
		return messagePartial, 0, bodyStart, checksumStart
	}
	trailerEnd := checksumStart + 7
	trailer := buf[checksumStart:trailerEnd]
	if !bytes.Equal(trailer[:3], []byte("10=")) || trailer[6] != soh {
		return messageInvalid, 0, bodyStart, checksumStart
	}
	for _, digit := range trailer[3:6] {
		if digit < '0' || digit > '9' {
			return messageInvalid, 0, bodyStart, checksumStart
		}
	}
	return messageComplete, trailerEnd, bodyStart, checksumStart
}

func readExpectedField(buf []byte, pos int, tag Tag) (Field, int, messageState) {
	tagText := strconv.AppendUint(nil, uint64(tag), 10)
	for i, expected := range tagText {
		if pos+i >= len(buf) {
			return Field{}, 0, messagePartial
		}
		if buf[pos+i] != expected {
			return Field{}, 0, messageInvalid
		}
	}
	equalPos := pos + len(tagText)
	if equalPos >= len(buf) {
		return Field{}, 0, messagePartial
	}
	if buf[equalPos] != '=' {
		return Field{}, 0, messageInvalid
	}
	valueStart := equalPos + 1
	sohOffset := bytes.IndexByte(buf[valueStart:], soh)
	if sohOffset < 0 {
		return Field{}, 0, messagePartial
	}
	valueEnd := valueStart + sohOffset
	return Field{Tag: tag, Value: buf[valueStart:valueEnd]}, valueEnd + 1, messageComplete
}

func readField(buf []byte, pos int) (Field, int, bool) {
	if pos < 0 || pos > len(buf) {
		return Field{}, pos, false
	}
	rest := buf[pos:]
	eq := bytes.IndexByte(rest, '=')
	if eq < 0 {
		return Field{}, pos, false
	}
	sohOffset := bytes.IndexByte(rest[eq+1:], soh)
	if sohOffset < 0 {
		return Field{}, pos, false
	}
	tag, ok := parseTag(rest[:eq])
	if !ok {
		return Field{}, pos, false
	}
	valueStart := eq + 1
	valueEnd := valueStart + sohOffset
	return Field{Tag: tag, Value: rest[valueStart:valueEnd]}, pos + valueEnd + 1, true
}

func parseTag(value []byte) (Tag, bool) {
	if len(value) == 0 {
		return 0, false
	}
	var tag uint64
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
		digit := uint64(c - '0')
		if tag > (math.MaxUint32-digit)/10 {
			return 0, false
		}
		tag = tag*10 + digit
	}
	return Tag(tag), true
}

func parseSize(value []byte) (int, bool) {
	if len(value) == 0 {
		return 0, false
	}
	var n uint64
	maxInt := uint64(^uint(0) >> 1)
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
		digit := uint64(c - '0')
		if n > (maxInt-digit)/10 {
			return 0, false
		}
		n = n*10 + digit
	}
	return int(n), true
}

func validDecimal(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	i := 0
	if value[i] == '-' || value[i] == '+' {
		i++
	}
	digits := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
		digits++
	}
	if i < len(value) && value[i] == '.' {
		i++
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
			digits++
		}
	}
	return i == len(value) && digits > 0
}
