package fix

import (
	"strconv"
	"time"
)

// Date parses a FIX date field in YYYYMMDD format.
func (f Field) Date() (time.Time, error) {
	value := f.Value
	if len(value) != 8 || !allDigits(value) {
		return time.Time{}, ErrParse
	}
	date, err := time.Parse("20060102", string(value))
	if err != nil {
		return time.Time{}, ErrParse
	}
	return date.UTC(), nil
}

// MonthYear parses a FIX MonthYear field in YYYYMM format.
func (f Field) MonthYear() (time.Time, error) {
	value := f.Value
	if len(value) != 6 || !allDigits(value) {
		return time.Time{}, ErrParse
	}
	date, err := time.Parse("200601", string(value))
	if err != nil {
		return time.Time{}, ErrParse
	}
	return date.UTC(), nil
}

// Time parses a FIX time-of-day value with millisecond precision.
func (f Field) Time() (time.Duration, error) {
	return parseFIXTime(f.Value, false)
}

// TimeNano parses a FIX time-of-day value with up to nanosecond precision.
func (f Field) TimeNano() (time.Duration, error) {
	return parseFIXTime(f.Value, true)
}

// Timestamp parses a FIX timestamp with millisecond precision.
func (f Field) Timestamp() (time.Time, error) {
	return parseFIXTimestamp(f.Value, false)
}

// TimestampNano parses a FIX timestamp with up to nanosecond precision.
func (f Field) TimestampNano() (time.Time, error) {
	return parseFIXTimestamp(f.Value, true)
}

// DecimalParts returns a decimal as an integer mantissa and base-10 exponent.
// For example, "12.500" is returned as 12500 and -3.
func (f Field) DecimalParts() (int64, int, error) {
	if !validDecimal(f.Value) {
		return 0, 0, ErrParse
	}
	digits := make([]byte, 0, len(f.Value))
	exponent := 0
	afterDecimal := false
	for _, value := range f.Value {
		switch value {
		case '.':
			afterDecimal = true
		default:
			digits = append(digits, value)
			if afterDecimal {
				exponent--
			}
		}
	}
	mantissa, err := strconv.ParseInt(string(digits), 10, 64)
	if err != nil {
		return 0, 0, ErrParse
	}
	return mantissa, exponent, nil
}

func parseFIXTimestamp(value []byte, nanosecondPrecision bool) (time.Time, error) {
	if len(value) < 17 || value[8] != '-' || !allDigits(value[:8]) {
		return time.Time{}, ErrParse
	}
	date, err := time.Parse("20060102", string(value[:8]))
	if err != nil {
		return time.Time{}, ErrParse
	}
	tod, err := parseFIXTime(value[9:], nanosecondPrecision)
	if err != nil {
		return time.Time{}, err
	}
	return date.Add(tod).UTC(), nil
}

func parseFIXTime(value []byte, nanosecondPrecision bool) (time.Duration, error) {
	if len(value) < 8 || value[2] != ':' || value[5] != ':' {
		return 0, ErrParse
	}
	hour, ok := twoDigits(value[0:2])
	if !ok {
		return 0, ErrParse
	}
	minute, ok := twoDigits(value[3:5])
	if !ok {
		return 0, ErrParse
	}
	second, ok := twoDigits(value[6:8])
	if !ok || hour > 23 || minute > 59 || second > 59 {
		return 0, ErrParse
	}

	nanoseconds := 0
	if len(value) > 8 {
		if value[8] != '.' {
			return 0, ErrParse
		}
		digits := value[9:]
		if (!nanosecondPrecision && len(digits) != 3) ||
			(nanosecondPrecision && len(digits) != 3 && len(digits) != 6 && len(digits) != 9) ||
			!allDigits(digits) {
			return 0, ErrParse
		}
		fraction, _ := strconv.Atoi(string(digits))
		switch len(digits) {
		case 3:
			nanoseconds = fraction * 1_000_000
		case 6:
			nanoseconds = fraction * 1_000
		case 9:
			nanoseconds = fraction
		}
	}
	return time.Duration(hour)*time.Hour +
		time.Duration(minute)*time.Minute +
		time.Duration(second)*time.Second +
		time.Duration(nanoseconds), nil
}

func twoDigits(value []byte) (int, bool) {
	if len(value) != 2 || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' {
		return 0, false
	}
	return int(value[0]-'0')*10 + int(value[1]-'0'), true
}

func allDigits(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
