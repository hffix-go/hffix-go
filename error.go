package fix

import (
	"errors"
	"strconv"
)

var (
	// ErrEncoding indicates that a field value is not valid UTF-8.
	ErrEncoding = errors.New("invalid UTF-8 in field value")
	// ErrParse indicates that a field value could not be parsed.
	ErrParse = errors.New("failed to parse field value")
	// ErrBufferTooSmall marks writes that do not fit in the caller's buffer.
	ErrBufferTooSmall = errors.New("buffer too small")
	// ErrWriterFinalized marks attempts to modify or refinish a finalized writer.
	ErrWriterFinalized = errors.New("writer is finalized")
	// ErrInvalidField marks a field value that cannot be represented safely.
	ErrInvalidField = errors.New("invalid FIX field value")
)

// BufferTooSmallError describes a write that exceeded the caller's buffer.
type BufferTooSmallError struct {
	Needed    int
	Available int
}

func (e *BufferTooSmallError) Error() string {
	return "buffer too small: need " + strconv.Itoa(e.Needed) +
		" bytes, have " + strconv.Itoa(e.Available)
}

func (e *BufferTooSmallError) Unwrap() error {
	return ErrBufferTooSmall
}
