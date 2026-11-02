package fix

import (
	"errors"
	"testing"
	"time"
)

func TestFIXDateTimeFieldConversions(t *testing.T) {
	date, err := (Field{Value: []byte("20240229")}).Date()
	if err != nil || !date.Equal(time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Date() = %v, %v", date, err)
	}

	monthYear, err := (Field{Value: []byte("202412")}).MonthYear()
	if err != nil || monthYear.Year() != 2024 || monthYear.Month() != time.December {
		t.Fatalf("MonthYear() = %v, %v", monthYear, err)
	}

	timeValue, err := (Field{Value: []byte("09:08:07.123")}).Time()
	wantTime := 9*time.Hour + 8*time.Minute + 7*time.Second + 123*time.Millisecond
	if err != nil || timeValue != wantTime {
		t.Fatalf("Time() = %v, %v; want %v", timeValue, err, wantTime)
	}
	nanoTime, err := (Field{Value: []byte("09:08:07.123456789")}).TimeNano()
	wantNanoTime := 9*time.Hour + 8*time.Minute + 7*time.Second + 123456789*time.Nanosecond
	if err != nil || nanoTime != wantNanoTime {
		t.Fatalf("TimeNano() = %v, %v; want %v", nanoTime, err, wantNanoTime)
	}

	timestamp, err := (Field{Value: []byte("20240229-09:08:07.123456789")}).TimestampNano()
	wantTimestamp := time.Date(2024, time.February, 29, 9, 8, 7, 123456789, time.UTC)
	if err != nil || !timestamp.Equal(wantTimestamp) {
		t.Fatalf("TimestampNano() = %v, %v; want %v", timestamp, err, wantTimestamp)
	}

	mantissa, exponent, err := (Field{Value: []byte("-12.500")}).DecimalParts()
	if err != nil || mantissa != -12500 || exponent != -3 {
		t.Fatalf("DecimalParts() = %d, %d, %v; want -12500, -3", mantissa, exponent, err)
	}
}

func TestFIXDateTimeRejectsMalformedValues(t *testing.T) {
	for _, test := range []struct {
		value []byte
		parse func(Field) error
	}{
		{[]byte("20230229"), func(field Field) error { _, err := field.Date(); return err }},
		{[]byte("202413"), func(field Field) error { _, err := field.MonthYear(); return err }},
		{[]byte("25:00:00"), func(field Field) error { _, err := field.TimeNano(); return err }},
		{[]byte("12:00:00.1234"), func(field Field) error { _, err := field.TimeNano(); return err }},
		{[]byte("20240229 12:00:00"), func(field Field) error { _, err := field.TimestampNano(); return err }},
	} {
		if err := test.parse(Field{Value: test.value}); !errors.Is(err, ErrParse) {
			t.Errorf("parser accepted %q: err = %v", test.value, err)
		}
	}

	if _, _, err := (Field{Value: []byte("9223372036854775808")}).DecimalParts(); !errors.Is(err, ErrParse) {
		t.Fatalf("DecimalParts() overflow error = %v; want ErrParse", err)
	}
}

func TestWriterDateTimeRoundTrip(t *testing.T) {
	writer, err := NewWriter(make([]byte, 128), "FIX.4.4")
	if err != nil {
		t.Fatal(err)
	}
	wantDate := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	wantTimestamp := time.Date(2024, time.February, 29, 9, 8, 7, 123456789, time.FixedZone("offset", -5*60*60))
	if err := writer.PushDate(64, wantDate); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushMonthYear(200, wantDate); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushTimeNano(9000, 9*time.Hour+8*time.Minute+7*time.Second+123456789*time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if err := writer.PushTimestamp(TransactTime, wantTimestamp); err != nil {
		t.Fatal(err)
	}
	message, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	reader := NewReader(message)
	date, ok := reader.Find(64)
	if !ok {
		t.Fatal("missing date")
	}
	gotDate, err := date.Date()
	if err != nil || !gotDate.Equal(wantDate) {
		t.Fatalf("Date() = %v, %v; want %v", gotDate, err, wantDate)
	}
	monthYear, ok := reader.Find(200)
	if !ok {
		t.Fatal("missing month-year")
	}
	if got, err := monthYear.MonthYear(); err != nil || got.Year() != 2024 || got.Month() != time.February {
		t.Fatalf("MonthYear() = %v, %v", got, err)
	}
	timeField, ok := reader.Find(9000)
	if !ok {
		t.Fatal("missing time")
	}
	if got, err := timeField.TimeNano(); err != nil || got != 9*time.Hour+8*time.Minute+7*time.Second+123456789*time.Nanosecond {
		t.Fatalf("TimeNano() = %v, %v", got, err)
	}
	timestamp, ok := reader.Find(TransactTime)
	if !ok {
		t.Fatal("missing timestamp")
	}
	gotTimestamp, err := timestamp.TimestampNano()
	if err != nil || !gotTimestamp.Equal(wantTimestamp.UTC()) {
		t.Fatalf("TimestampNano() = %v, %v; want %v", gotTimestamp, err, wantTimestamp.UTC())
	}
}
