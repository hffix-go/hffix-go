package fix

import (
	"bytes"
	"errors"
	"testing"
)

type stringValue string

func (v stringValue) String() string {
	return string(v)
}

func TestWriterRoundTripAndChecksum(t *testing.T) {
	buf := make([]byte, 128)
	writer, err := NewWriter(buf, "FIX.4.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushString(MsgType, "D"); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushDecimal(Price, "101.250"); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushInt(OrderQty, 100); err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if writer.Size() != len(message) || writer.Remaining() != len(buf)-len(message) {
		t.Fatalf("writer sizes = %d written, %d remaining; want %d, %d",
			writer.Size(), writer.Remaining(), len(message), len(buf)-len(message))
	}
	if !bytes.Equal(writer.Bytes(), message) {
		t.Fatalf("Writer.Bytes() = %q; want %q", writer.Bytes(), message)
	}

	reader := NewReader(message)
	price, ok := reader.Find(Price)
	if !ok {
		t.Fatal("Find(Price) failed")
	}
	if got, err := price.Decimal(); err != nil || got != "101.250" {
		t.Fatalf("Decimal() = %q, %v; want 101.250", got, err)
	}
	quantity, ok := reader.Find(OrderQty)
	if !ok {
		t.Fatal("Find(OrderQty) failed")
	}
	if got, err := quantity.Int64(); err != nil || got != 100 {
		t.Fatalf("Int64() = %d, %v; want 100", got, err)
	}
	checksum, ok := reader.Find(CheckSum)
	if !ok || string(checksum.Value) != string(message[len(message)-4:len(message)-1]) {
		t.Fatalf("checksum field = %+v, %v", checksum, ok)
	}
}

func TestWriterFormattedAndUnsignedValues(t *testing.T) {
	writer, err := NewWriter(make([]byte, 128), "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushFormatted(Text, stringValue("formatted")); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushUint(9000, ^uint64(0)); err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	reader := NewReader(message)
	text, ok := reader.Find(Text)
	if !ok || string(text.Value) != "formatted" {
		t.Fatalf("formatted field = %+v, %v", text, ok)
	}
	number, ok := reader.Find(9000)
	if !ok || string(number.Value) != "18446744073709551615" {
		t.Fatalf("unsigned field = %+v, %v", number, ok)
	}
}

func TestWriterChecksumReference(t *testing.T) {
	buf := make([]byte, 50)
	writer, err := NewWriter(buf, "FIX.4.2")
	if err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(message[len(message)-4 : len(message)-1]); got != "182" {
		t.Fatalf("checksum = %s; want 182", got)
	}

	buf = make([]byte, 50)
	writer, err = NewWriter(buf, "FIX.4.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushByte(Text, 0x80); err != nil {
		t.Fatal(err)
	}
	message, err = writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(message[len(message)-4 : len(message)-1]); got != "230" {
		t.Fatalf("high-bit checksum = %s; want 230", got)
	}
}

func TestWriterBufferErrorsAndExactFit(t *testing.T) {
	_, err := NewWriter(make([]byte, 4), "FIX.4.4")
	var tooSmall *BufferTooSmallError
	if !errors.As(err, &tooSmall) || !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("NewWriter error = %v; want BufferTooSmallError", err)
	}
	if tooSmall.Needed <= tooSmall.Available {
		t.Fatalf("buffer error has Needed=%d Available=%d", tooSmall.Needed, tooSmall.Available)
	}

	expected := []byte("8=FIX.4.2\x019=000000\x0110=182\x01")
	buf := make([]byte, len(expected))
	writer, err := NewWriter(buf, "FIX.4.2")
	if err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if string(message) != string(expected) {
		t.Fatalf("message = %q; want %q", message, expected)
	}
	again, err := writer.Finish()
	if err != nil || string(again) != string(message) {
		t.Fatalf("second Finish() = %q, %v; want the same message", again, err)
	}
	if err := writer.PushString(Text, "late"); !errors.Is(err, ErrWriterFinalized) {
		t.Fatalf("PushString after Finish() error = %v; want ErrWriterFinalized", err)
	}
}

func TestWriterRejectsInvalidDecimalAndBodyLengthOverflow(t *testing.T) {
	writer, err := NewWriter(make([]byte, 1_100_100), "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushString(Text, string([]byte{0xff})); !errors.Is(err, ErrEncoding) {
		t.Fatalf("PushString invalid UTF-8 error = %v; want ErrEncoding", err)
	}
	if err := writer.PushDecimal(Price, "1e2"); !errors.Is(err, ErrParse) {
		t.Fatalf("PushDecimal invalid value error = %v; want ErrParse", err)
	}
	if err := writer.PushBytes(Text, make([]byte, 1_000_010)); err != nil {
		t.Fatal(err)
	}
	_, err = writer.Finish()
	var tooSmall *BufferTooSmallError
	if !errors.As(err, &tooSmall) || tooSmall.Available != 999_999 {
		t.Fatalf("Finish() error = %v; want body-length overflow", err)
	}
}

func TestWriterFieldErrorsDoNotPartiallyWrite(t *testing.T) {
	writer, err := NewWriter(make([]byte, 32), "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushBytes(Text, []byte("bad\x01value")); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("PushBytes with SOH error = %v; want ErrInvalidField", err)
	}
	if err := writer.PushString(999, "0123456789"); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("oversized PushString error = %v; want ErrBufferTooSmall", err)
	}
	if err := writer.PushString(100, "x"); err != nil {
		t.Fatalf("PushString after rejected writes = %v", err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(message, []byte("100=x\x01")) || bytes.Contains(message, []byte("999=")) {
		t.Fatalf("unexpected message after rejected writes: %q", message)
	}
}

func TestWriterDecimalPartsAndChecksumOption(t *testing.T) {
	writer, err := NewWriter(make([]byte, 128), "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PushDecimalParts(9001, 12500, -3); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushDecimalParts(9002, 0, -3); err != nil {
		t.Fatal(err)
	}
	message, err := writer.FinishWithChecksum(false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(message, []byte("9001=12.500\x01")) ||
		!bytes.Contains(message, []byte("9002=0.000\x01")) ||
		!bytes.HasSuffix(message, []byte("10=000\x01")) {
		t.Fatalf("unexpected decimal or checksum serialization: %q", message)
	}
	if again, err := writer.Finish(); err != nil || !bytes.Equal(again, message) {
		t.Fatalf("Finish after FinishWithChecksum = %q, %v; want same message", again, err)
	}
}
