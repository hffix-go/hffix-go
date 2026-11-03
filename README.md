# hffix-go

A Go port of the public parsing and writing functionality in
[hffix](https://github.com/jamesdbrock/hffix). It includes FIX tag constants,
zero-copy field scanning, incremental message validation, caller-buffer
serialization, and FIX date/time and numeric conversions.

## Install

The module path is the repository path, so the package can be imported directly:

```sh
go get github.com/hffix-go/hffix-go
```

The default `main` branch may include experimental or in-progress features. For
a stable experience, please use specific tags rather than `@main`. For example,
to pin v1.2.3:

```sh
go get github.com/hffix-go/hffix-go@v1.2.3
```

which records the release in your `go.mod`:

```
require github.com/hffix-go/hffix-go v1.2.3
```

Tags are `v`-prefixed versions of [VERSION](./VERSION); use the
[tags](https://github.com/hffix-go/hffix-go/tags) page to choose one.

## Quickstart

In your own module, create `main.go` and run it with `go run .`:

```go
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	fix "github.com/hffix-go/hffix-go"
)

func main() {
	buf := make([]byte, 256)
	w, err := fix.NewWriter(buf, "FIX.4.4")
	if err != nil {
		log.Fatal(err)
	}

	push := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	push(w.PushString(fix.MsgType, "D"))
	push(w.PushString(fix.ClOrdID, "demo-1"))
	push(w.PushString(fix.Symbol, "AAPL"))
	push(w.PushByte(fix.Side, '1'))
	push(w.PushInt(fix.OrderQty, 100))
	push(w.PushByte(fix.OrdType, '2'))
	push(w.PushDecimal(fix.Price, "101.25"))
	push(w.PushTimestamp(fix.TransactTime,
		time.Date(2026, time.November, 3, 14, 30, 0, 0, time.UTC)))

	msg, err := w.Finish()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.ReplaceAll(string(msg), "\x01", "|"))

	r := fix.NewReader(msg)
	if _, ok := r.Complete(); !ok || !r.VerifyChecksum() {
		log.Fatal("incomplete or corrupt message")
	}

	msgType, _ := r.MessageType()
	fmt.Printf("MsgType = %s\n", msgType.Value)

	for field, ok := r.Next(); ok; field, ok = r.Next() {
		fmt.Printf("%d = %s\n", field.Tag, field.Value)
	}

	r = fix.NewReader(msg)
	if price, ok := r.Find(fix.Price); ok {
		value, err := price.Decimal()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Price as decimal = %s\n", value)
	}
}
```

`|` in the first line stands for the SOH byte (`0x01`) that delimits fields on
the wire:

```
8=FIX.4.4|9=000071|35=D|11=demo-1|55=AAPL|54=1|38=100|40=2|44=101.25|60=20261103-14:30:00|10=194|
MsgType = D
8 = FIX.4.4
9 = 000071
35 = D
11 = demo-1
55 = AAPL
54 = 1
38 = 100
40 = 2
44 = 101.25
60 = 20261103-14:30:00
10 = 194
Price as decimal = 101.25
```

The same program is in [examples/quickstart](./examples/quickstart/main.go)
and can be run from a checkout with `go run ./examples/quickstart`.

To compile and run that example as an external consumer against your current
working tree, run:

```sh
./scripts/consumer-check.sh
```

The script uses a temporary module and a local `replace`, so local library
changes are tested without publishing them. It also checks that the module path
matches the GitHub import path. It does not test fetching a published version
or branch from GitHub. CI runs it on Linux and macOS.

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

## Releasing

Releases are tagged from the contents of [VERSION](./VERSION) with a `v` prefix,
so a `VERSION` of `1.2.3` releases tag `v1.2.3`:

```sh
python3 scripts/release.py
```

The script only tags when the checkout is on `main`, has no staged, unstaged,
or untracked changes, and is in sync with `origin/main`. It then creates the
tag locally and pushes it. Use `--dry-run` to run every check without tagging,
or `--remote <name>` to target a remote other than `origin`.
