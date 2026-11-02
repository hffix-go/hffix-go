# hffix-go

A Go port of the public parsing and writing functionality in
[hffix](https://github.com/jamesdbrock/hffix). It includes FIX tag constants,
zero-copy field scanning, incremental message validation, caller-buffer
serialization, and FIX date/time and numeric conversions.

```go
package main

import (
	"fmt"
	"log"

	fix "hffix-go"
)

func main() {
	buf := make([]byte, 256)
	w, err := fix.NewWriter(buf, "FIX.4.4")
	if err != nil {
		log.Fatal(err)
	}
	if err := w.PushString(fix.MsgType, "D"); err != nil {
		log.Fatal(err)
	}
	if err := w.PushDecimal(fix.Price, "101.25"); err != nil {
		log.Fatal(err)
	}
	msg, err := w.Finish()
	if err != nil {
		log.Fatal(err)
	}

	r := fix.NewReader(msg)
	for field, ok := r.Next(); ok; field, ok = r.Next() {
		fmt.Printf("%d = %s\n", field.Tag, field.Value)
	}
}
```

`Reader.Find` advances from its current position; `FindWithHint` scans from a
byte offset without changing it. `Reader.Complete` checks the message length
and checksum trailer, while `Reader.Valid` distinguishes malformed data from a
message that may still be arriving. `Reader.VerifyChecksum` verifies the
modulo-256 checksum, and `Reader.NextMessage` frames concatenated messages.

Raw-data values can contain SOH. Write them with
`Writer.PushLengthPrefixedBytes` and read them with `Reader.NextDataField`,
using the length from the paired length tag. `Reader.Next` is a flat
SOH-delimited field scanner; use `Complete` or `NextMessage` to frame messages.

The writer writes into the caller's buffer and never grows it. Decimal strings
retain their exact wire precision and scale; `PushDecimalParts` supports
mantissa/exponent values. `FinishWithChecksum(false)` writes a zero checksum
when a peer requires that behavior. The full tag set derives from upstream
`hffix_fields.hpp`; see [LICENSE](./LICENSE) for its license.
