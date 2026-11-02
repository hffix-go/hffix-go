package fix

import (
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	bodyLengthWidth = 6
	checksumWidth   = 3
)

// Writer serializes a FIX message into a caller-provided buffer. Finish
// patches BodyLength and appends CheckSum.
type Writer struct {
	buf                 []byte
	pos                 int
	bodyLengthDigitsPos int
	bodyStart           int
	messageEnd          int
	finished            bool
	failed              bool
}

// NewWriter starts a FIX message with BeginString and a reserved BodyLength.
func NewWriter(buf []byte, beginString string) (*Writer, error) {
	w := &Writer{buf: buf}
	if err := w.PushString(BeginString, beginString); err != nil {
		return nil, err
	}
	if err := w.pushTag(BodyLength); err != nil {
		return nil, err
	}
	w.bodyLengthDigitsPos = w.pos
	if err := w.pushRawBytes([]byte("000000")); err != nil {
		return nil, err
	}
	if err := w.pushRawBytes([]byte{soh}); err != nil {
		return nil, err
	}
	w.bodyStart = w.pos
	return w, nil
}

// Size returns the number of bytes written into the caller's buffer.
func (w *Writer) Size() int {
	if w.finished {
		return w.messageEnd
	}
	return w.pos
}

// Remaining returns the number of unused bytes in the caller's buffer.
func (w *Writer) Remaining() int {
	return len(w.buf) - w.pos
}

// Bytes returns the bytes written so far without copying them. Before Finish,
// the message header still contains its reserved BodyLength.
func (w *Writer) Bytes() []byte {
	if w.finished {
		return w.buf[:w.messageEnd]
	}
	return w.buf[:w.pos]
}

// PushString appends a UTF-8 string field.
func (w *Writer) PushString(tag Tag, value string) error {
	if !utf8.ValidString(value) {
		return ErrEncoding
	}
	return w.PushBytes(tag, []byte(value))
}

// PushInt appends a signed base-10 integer field.
func (w *Writer) PushInt(tag Tag, value int64) error {
	var tmp [20]byte
	data := strconv.AppendInt(tmp[:0], value, 10)
	return w.PushBytes(tag, data)
}

// PushUint appends an unsigned base-10 integer field.
func (w *Writer) PushUint(tag Tag, value uint64) error {
	var tmp [20]byte
	data := strconv.AppendUint(tmp[:0], value, 10)
	return w.PushBytes(tag, data)
}

// PushDecimal appends a decimal field without changing its precision or scale.
func (w *Writer) PushDecimal(tag Tag, value string) error {
	if w.finished || w.failed {
		return ErrWriterFinalized
	}
	if !validDecimal([]byte(value)) {
		return ErrParse
	}
	return w.PushBytes(tag, []byte(value))
}

// PushDecimalParts appends mantissa multiplied by 10 to the given exponent.
// For example, a mantissa of 12500 and exponent -3 writes "12.500".
func (w *Writer) PushDecimalParts(tag Tag, mantissa int64, exponent int) error {
	if w.finished || w.failed {
		return ErrWriterFinalized
	}
	var digitsBuffer [20]byte
	digits := strconv.AppendInt(digitsBuffer[:0], mantissa, 10)
	signLength := 0
	if digits[0] == '-' {
		signLength = 1
	}
	digitCount := len(digits) - signLength

	valueLength := 0
	if exponent >= 0 {
		if exponent > math.MaxInt-len(digits) {
			return &BufferTooSmallError{Needed: math.MaxInt, Available: len(w.buf)}
		}
		valueLength = len(digits) + exponent
	} else {
		scale := uint64(-(int64(exponent) + 1)) + 1
		if scale < uint64(digitCount) {
			valueLength = len(digits) + 1
		} else {
			zeroCount := scale - uint64(digitCount)
			if zeroCount > uint64(math.MaxInt-len(digits)-2) {
				return &BufferTooSmallError{Needed: math.MaxInt, Available: len(w.buf)}
			}
			valueLength = len(digits) + 2 + int(zeroCount)
		}
	}

	fixedSize := tagWidth(tag) + 2
	if valueLength > math.MaxInt-fixedSize {
		return &BufferTooSmallError{Needed: math.MaxInt, Available: len(w.buf)}
	}
	if err := w.ensureCapacity(fixedSize + valueLength); err != nil {
		return err
	}
	if err := w.pushTag(tag); err != nil {
		return err
	}

	if exponent >= 0 {
		if err := w.pushRawBytes(digits); err != nil {
			return err
		}
		for range exponent {
			w.buf[w.pos] = '0'
			w.pos++
		}
	} else {
		scale := uint64(-(int64(exponent) + 1)) + 1
		if scale < uint64(digitCount) {
			split := len(digits) - int(scale)
			if err := w.pushRawBytes(digits[:split]); err != nil {
				return err
			}
			if err := w.pushRawBytes([]byte{'.'}); err != nil {
				return err
			}
			if err := w.pushRawBytes(digits[split:]); err != nil {
				return err
			}
		} else {
			if signLength != 0 {
				if err := w.pushRawBytes(digits[:1]); err != nil {
					return err
				}
			}
			if err := w.pushRawBytes([]byte("0.")); err != nil {
				return err
			}
			for zeroCount := scale - uint64(digitCount); zeroCount > 0; zeroCount-- {
				w.buf[w.pos] = '0'
				w.pos++
			}
			if err := w.pushRawBytes(digits[signLength:]); err != nil {
				return err
			}
		}
	}
	return w.pushRawBytes([]byte{soh})
}

// PushFormatted appends the default fmt representation of value.
func (w *Writer) PushFormatted(tag Tag, value any) error {
	var tmp [64]byte
	return w.PushBytes(tag, fmt.Append(tmp[:0], value))
}

// PushByte appends a one-byte field value.
func (w *Writer) PushByte(tag Tag, value byte) error {
	return w.PushBytes(tag, []byte{value})
}

// PushDate appends a FIX LocalMktDate or UTCDate field in YYYYMMDD format.
func (w *Writer) PushDate(tag Tag, value time.Time) error {
	if value.Year() < 1 || value.Year() > 9999 {
		return ErrInvalidField
	}
	return w.PushString(tag, value.Format("20060102"))
}

// PushMonthYear appends a FIX MonthYear field in YYYYMM format.
func (w *Writer) PushMonthYear(tag Tag, value time.Time) error {
	if value.Year() < 1 || value.Year() > 9999 {
		return ErrInvalidField
	}
	return w.PushString(tag, value.Format("200601"))
}

// PushTime appends a FIX time-of-day value, retaining sub-second precision.
func (w *Writer) PushTime(tag Tag, value time.Duration) error {
	return w.pushTime(tag, value, -1)
}

// PushTimeMillis appends a FIX time-of-day value with exactly millisecond
// precision. value must be within one day and aligned to a millisecond.
func (w *Writer) PushTimeMillis(tag Tag, value time.Duration) error {
	return w.pushTime(tag, value, 3)
}

// PushTimeNano appends a FIX time-of-day value with exactly nanosecond
// precision. value must be within one day.
func (w *Writer) PushTimeNano(tag Tag, value time.Duration) error {
	return w.pushTime(tag, value, 9)
}

// PushTimestamp appends a UTC FIX timestamp, retaining sub-second precision.
func (w *Writer) PushTimestamp(tag Tag, value time.Time) error {
	return w.pushTimestamp(tag, value.UTC(), -1)
}

// PushTimestampMillis appends a UTC FIX timestamp with exactly millisecond
// precision.
func (w *Writer) PushTimestampMillis(tag Tag, value time.Time) error {
	value = value.UTC()
	if value.Nanosecond()%int(time.Millisecond) != 0 {
		return ErrInvalidField
	}
	return w.pushTimestamp(tag, value, 3)
}

// PushTimestampNano appends a UTC FIX timestamp with exactly nanosecond
// precision.
func (w *Writer) PushTimestampNano(tag Tag, value time.Time) error {
	return w.pushTimestamp(tag, value.UTC(), 9)
}

func (w *Writer) pushTimestamp(tag Tag, value time.Time, precision int) error {
	if value.Year() < 1 || value.Year() > 9999 {
		return ErrInvalidField
	}
	tod := time.Duration(value.Hour())*time.Hour +
		time.Duration(value.Minute())*time.Minute +
		time.Duration(value.Second())*time.Second +
		time.Duration(value.Nanosecond())
	date := value.Format("20060102") + "-"
	timeValue, err := formatTime(tod, precision)
	if err != nil {
		return err
	}
	valueBytes := make([]byte, 0, len(date)+len(timeValue))
	valueBytes = append(valueBytes, date...)
	valueBytes = append(valueBytes, timeValue...)
	return w.PushBytes(tag, valueBytes)
}

func (w *Writer) pushTime(tag Tag, value time.Duration, precision int) error {
	timeValue, err := formatTime(value, precision)
	if err != nil {
		return err
	}
	return w.PushBytes(tag, timeValue)
}

func formatTime(value time.Duration, precision int) ([]byte, error) {
	if value < 0 || value >= 24*time.Hour {
		return nil, ErrInvalidField
	}
	nanoseconds := int64(value % time.Second)
	if precision == -1 {
		switch {
		case nanoseconds == 0:
			precision = 0
		case nanoseconds%int64(time.Millisecond) == 0:
			precision = 3
		case nanoseconds%int64(time.Microsecond) == 0:
			precision = 6
		default:
			precision = 9
		}
	}
	if precision != 0 && precision != 3 && precision != 6 && precision != 9 {
		return nil, ErrInvalidField
	}
	if precision == 3 && nanoseconds%int64(time.Millisecond) != 0 ||
		precision == 6 && nanoseconds%int64(time.Microsecond) != 0 {
		return nil, ErrInvalidField
	}

	var result [18]byte
	hour := int(value / time.Hour)
	minute := int(value/time.Minute) % 60
	second := int(value/time.Second) % 60
	result[0], result[1] = '0'+byte(hour/10), '0'+byte(hour%10)
	result[2] = ':'
	result[3], result[4] = '0'+byte(minute/10), '0'+byte(minute%10)
	result[5] = ':'
	result[6], result[7] = '0'+byte(second/10), '0'+byte(second%10)

	length := 8
	if precision > 0 {
		result[8] = '.'
		frac := nanoseconds
		if precision == 3 {
			frac /= int64(time.Millisecond)
		} else if precision == 6 {
			frac /= int64(time.Microsecond)
		}
		for i := precision - 1; i >= 0; i-- {
			result[9+i] = '0' + byte(frac%10)
			frac /= 10
		}
		length += precision + 1
	}
	return result[:length], nil
}

// PushLengthPrefixedBytes appends a length field followed by raw data. The
// data may contain SOH and should be read with Reader.NextDataField.
func (w *Writer) PushLengthPrefixedBytes(lenTag, dataTag Tag, value []byte) error {
	if w.finished || w.failed {
		return ErrWriterFinalized
	}
	var lengthBuffer [20]byte
	length := strconv.AppendInt(lengthBuffer[:0], int64(len(value)), 10)
	fixedSize := tagWidth(lenTag) + 1 + len(length) + 1 + tagWidth(dataTag) + 2
	if len(value) > math.MaxInt-fixedSize {
		return &BufferTooSmallError{Needed: math.MaxInt, Available: len(w.buf)}
	}
	required := fixedSize + len(value)
	if err := w.ensureCapacity(required); err != nil {
		return err
	}
	if err := w.pushTag(lenTag); err != nil {
		return err
	}
	if err := w.pushRawBytes(length); err != nil {
		return err
	}
	if err := w.pushRawBytes([]byte{soh}); err != nil {
		return err
	}
	if err := w.pushTag(dataTag); err != nil {
		return err
	}
	if err := w.pushRawBytes(value); err != nil {
		return err
	}
	return w.pushRawBytes([]byte{soh})
}

// PushBytes appends a tag=value field terminated by SOH.
func (w *Writer) PushBytes(tag Tag, value []byte) error {
	if w.finished || w.failed {
		return ErrWriterFinalized
	}
	if bytesContainSOH(value) {
		return ErrInvalidField
	}
	fixedSize := tagWidth(tag) + 2
	if len(value) > math.MaxInt-fixedSize {
		return &BufferTooSmallError{Needed: math.MaxInt, Available: len(w.buf)}
	}
	if err := w.ensureCapacity(fixedSize + len(value)); err != nil {
		return err
	}
	if err := w.pushTag(tag); err != nil {
		return err
	}
	if err := w.pushRawBytes(value); err != nil {
		return err
	}
	return w.pushRawBytes([]byte{soh})
}

// Finish patches BodyLength, appends CheckSum, and returns the finished
// message. The returned bytes share the writer's buffer.
func (w *Writer) Finish() ([]byte, error) {
	return w.FinishWithChecksum(true)
}

// FinishWithChecksum patches BodyLength, appends CheckSum, and returns the
// finished message. When calculateChecksum is false, CheckSum is written as
// "000". The returned bytes share the writer's buffer.
func (w *Writer) FinishWithChecksum(calculateChecksum bool) ([]byte, error) {
	if w.finished {
		return w.buf[:w.messageEnd], nil
	}
	if w.failed {
		return nil, ErrWriterFinalized
	}
	bodyLength := w.pos - w.bodyStart
	if bodyLength < 0 || bodyLength >= 1_000_000 {
		w.failed = true
		return nil, &BufferTooSmallError{Needed: bodyLength, Available: 999_999}
	}
	if err := w.ensureCapacity(7); err != nil {
		return nil, err
	}
	writeZeroPadded(w.buf[w.bodyLengthDigitsPos:w.bodyLengthDigitsPos+bodyLengthWidth], bodyLength)

	var checksum uint8
	if calculateChecksum {
		for _, b := range w.buf[:w.pos] {
			checksum += b
		}
	}
	if err := w.pushTag(CheckSum); err != nil {
		w.failed = true
		return nil, err
	}
	var digits [checksumWidth]byte
	writeZeroPadded(digits[:], int(checksum))
	if err := w.pushRawBytes(digits[:]); err != nil {
		w.failed = true
		return nil, err
	}
	if err := w.pushRawBytes([]byte{soh}); err != nil {
		w.failed = true
		return nil, err
	}
	w.finished = true
	w.messageEnd = w.pos
	return w.buf[:w.pos], nil
}

func (w *Writer) pushTag(tag Tag) error {
	var tmp [10]byte
	data := strconv.AppendUint(tmp[:0], uint64(tag), 10)
	if err := w.pushRawBytes(data); err != nil {
		return err
	}
	return w.pushRawBytes([]byte{'='})
}

func (w *Writer) pushRawBytes(value []byte) error {
	if w.pos < 0 || w.pos > len(w.buf) || len(value) > len(w.buf)-w.pos {
		return bufferTooSmall(w.pos, len(value), len(w.buf))
	}
	copy(w.buf[w.pos:], value)
	w.pos += len(value)
	return nil
}

func (w *Writer) ensureCapacity(size int) error {
	if size < 0 || w.pos < 0 || w.pos > len(w.buf) || size > len(w.buf)-w.pos {
		return bufferTooSmall(w.pos, size, len(w.buf))
	}
	return nil
}

func tagWidth(tag Tag) int {
	var buffer [10]byte
	return len(strconv.AppendUint(buffer[:0], uint64(tag), 10))
}

func bytesContainSOH(value []byte) bool {
	for _, b := range value {
		if b == soh {
			return true
		}
	}
	return false
}

func bufferTooSmall(pos, size, available int) *BufferTooSmallError {
	needed := pos + size
	if needed < pos {
		needed = math.MaxInt
	}
	return &BufferTooSmallError{Needed: needed, Available: available}
}

func writeZeroPadded(dst []byte, value int) {
	for i := len(dst) - 1; i >= 0; i-- {
		dst[i] = byte('0' + value%10)
		value /= 10
	}
}
