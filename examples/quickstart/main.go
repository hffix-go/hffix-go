// Command quickstart writes a FIX 4.4 NewOrderSingle message into a caller
// buffer and reads it back using hffix-go as an external dependency.
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
	if _, ok := r.Complete(); !ok {
		log.Fatal("incomplete message")
	}
	if !r.VerifyChecksum() {
		log.Fatal("bad checksum")
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
