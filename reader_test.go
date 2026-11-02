package fix

import (
	"bytes"
	"errors"
	"testing"
)

func TestReaderFieldsAndHint(t *testing.T) {
	input := []byte("35=A\x0155=BTC\x0155=ETH\x01")
	reader := NewReader(input)

	first, ok := reader.Find(Symbol)
	if !ok {
		t.Fatal("Find(Symbol) failed")
	}
	if got, err := first.Text(); err != nil || got != "BTC" {
		t.Fatalf("first symbol = %q, %v; want BTC", got, err)
	}

	position := reader.Position()
	second, ok := reader.FindWithHint(Symbol, position)
	if !ok {
		t.Fatal("FindWithHint(Symbol) failed")
	}
	if got, err := second.Text(); err != nil || got != "ETH" {
		t.Fatalf("second symbol = %q, %v; want ETH", got, err)
	}
	if reader.Position() != position {
		t.Fatal("FindWithHint changed the reader position")
	}
	if !bytes.Equal(reader.Bytes(), input) {
		t.Fatal("Bytes did not return the original buffer")
	}
}

func TestReaderMalformedTailAndConversions(t *testing.T) {
	reader := NewReader([]byte("35=A\x0138=-9223372036854775808\x0158=\x01XX=bad\x01"))
	if field, ok := reader.Next(); !ok || field.Tag != MsgType {
		t.Fatalf("first field = %+v, %v", field, ok)
	}
	quantity, ok := reader.Find(OrderQty)
	if !ok {
		t.Fatal("Find(OrderQty) failed")
	}
	if got, err := quantity.Int64(); err != nil || got != -1<<63 {
		t.Fatalf("Int64() = %d, %v; want MinInt64", got, err)
	}
	empty, ok := reader.Next()
	if !ok || empty.Tag != Text || len(empty.Bytes()) != 0 {
		t.Fatalf("empty field = %+v, %v", empty, ok)
	}
	if _, ok := reader.Next(); ok {
		t.Fatal("malformed tail should not produce a field")
	}
}

func TestFieldConversions(t *testing.T) {
	field := Field{Value: []byte("-12.500")}
	if got, err := field.Decimal(); err != nil || got != "-12.500" {
		t.Fatalf("Decimal() = %q, %v", got, err)
	}
	if got, err := (Field{Value: []byte{0x80}}).Text(); !errors.Is(err, ErrEncoding) || got != "" {
		t.Fatalf("Text() for invalid UTF-8 = %q, %v", got, err)
	}
	if _, err := (Field{Value: []byte("12x")}).Int64(); !errors.Is(err, ErrParse) {
		t.Fatalf("Int64() error = %v; want ErrParse", err)
	}
	if _, err := (Field{}).Byte(); !errors.Is(err, ErrParse) {
		t.Fatalf("Byte() error = %v; want ErrParse", err)
	}
}

func TestReaderRawDataAndMessageValidation(t *testing.T) {
	buf := make([]byte, 128)
	writer, err := NewWriter(buf, "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushString(MsgType, "A"); err != nil {
		t.Fatal(err)
	}
	raw := []byte("da\x01tum")
	if err := writer.PushLengthPrefixedBytes(RawDataLength, RawData, raw); err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	reader := NewReader(message)
	if end, ok := reader.Complete(); !ok || end != len(message) {
		t.Fatalf("Complete() = %d, %v; want %d, true", end, ok, len(message))
	}
	if !reader.Valid() {
		t.Fatal("Valid() rejected a complete message")
	}
	msgType, ok := reader.MessageType()
	if !ok || string(msgType.Value) != "A" {
		t.Fatalf("MessageType() = %+v, %v", msgType, ok)
	}

	lengthField, ok := reader.Find(RawDataLength)
	if !ok {
		t.Fatal("Find(RawDataLength) failed")
	}
	length, err := lengthField.Int64()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := reader.NextDataField(RawData, int(length))
	if !ok || !bytes.Equal(data.Value, raw) {
		t.Fatalf("NextDataField() = %q, %v; want %q", data.Value, ok, raw)
	}
	if _, ok := reader.Find(CheckSum); !ok {
		t.Fatal("reader did not resume after raw data")
	}
}

func TestReaderTruncationAndInvalidStructure(t *testing.T) {
	message := []byte("8=FIX.4.4\x019=5\x0135=A\x0110=000\x01")
	for cut := 0; cut < len(message); cut++ {
		reader := NewReader(message[:cut])
		if _, ok := reader.Complete(); ok {
			t.Fatalf("Complete() succeeded for %d-byte prefix", cut)
		}
		if !reader.Valid() {
			t.Fatalf("Valid() rejected incomplete %d-byte prefix", cut)
		}
	}
	if NewReader([]byte("9=5\x0135=A\x0110=000\x01")).Valid() {
		t.Fatal("Valid() accepted a message without BeginString")
	}
	if NewReader([]byte("8=FIX.4.4\x019=abc\x0135=A\x01")).Valid() {
		t.Fatal("Valid() accepted a non-numeric BodyLength")
	}
}

func TestReaderConcatenatedMessages(t *testing.T) {
	buf := make([]byte, 128)
	offset := 0
	for range 2 {
		writer, err := NewWriter(buf[offset:], "FIX.4.2")
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.PushString(MsgType, "A"); err != nil {
			t.Fatal(err)
		}
		message, err := writer.Finish()
		if err != nil {
			t.Fatal(err)
		}
		offset += len(message)
	}
	reader := NewReader(buf[:offset])
	count := 0
	for {
		field, ok := reader.Next()
		if !ok {
			break
		}
		if field.Tag == CheckSum {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("found %d checksum fields; want 2", count)
	}
}

func TestReaderChecksumAndNextMessage(t *testing.T) {
	buf := make([]byte, 128)
	first, err := NewWriter(buf, "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.PushString(MsgType, "A"); err != nil {
		t.Fatal(err)
	}
	message, err := first.Finish()
	if err != nil {
		t.Fatal(err)
	}

	second, err := NewWriter(buf[len(message):], "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.PushString(MsgType, "D"); err != nil {
		t.Fatal(err)
	}
	secondMessage, err := second.Finish()
	if err != nil {
		t.Fatal(err)
	}
	message = append(message, secondMessage...)

	reader := NewReader(message)
	if !reader.VerifyChecksum() {
		t.Fatal("VerifyChecksum rejected a valid message")
	}
	size, ok := reader.MessageSize()
	if !ok || size != len(first.Bytes()) {
		t.Fatalf("MessageSize() = %d, %v; want %d", size, ok, len(first.Bytes()))
	}
	messageBytes, ok := reader.MessageBytes()
	if !ok || !bytes.Equal(messageBytes, first.Bytes()) {
		t.Fatalf("MessageBytes() = %q, %v; want %q", messageBytes, ok, first.Bytes())
	}
	checksum, ok := reader.Checksum()
	calculated, calculatedOK := reader.CalculateChecksum()
	if !ok || !calculatedOK || checksum != calculated {
		t.Fatalf("checksum = %d, calculated = %d; want equal checksums", checksum, calculated)
	}
	next, ok := reader.NextMessage()
	if !ok {
		t.Fatal("NextMessage did not return the second message")
	}
	msgType, ok := next.MessageType()
	if !ok || string(msgType.Value) != "D" {
		t.Fatalf("next MsgType = %+v, %v; want D", msgType, ok)
	}

	corrupt := append([]byte(nil), message...)
	checksumStart := len(message) - len(secondMessage) - 7
	corrupt[checksumStart+3] = '9'
	corruptReader := NewReader(corrupt)
	if _, ok := corruptReader.Complete(); !ok {
		t.Fatal("Complete rejected a correctly framed message with a wrong checksum")
	}
	if corruptReader.VerifyChecksum() {
		t.Fatal("VerifyChecksum accepted a wrong checksum")
	}
}

func TestReaderRejectsMalformedChecksumTrailer(t *testing.T) {
	reader := NewReader([]byte("8=FIX.4.4\x019=5\x0135=A\x0110=0x0\x01"))
	if reader.Valid() {
		t.Fatal("Valid accepted a non-numeric checksum")
	}
	if _, ok := reader.Complete(); ok {
		t.Fatal("Complete accepted a non-numeric checksum")
	}
}
