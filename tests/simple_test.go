package tests

import (
	"bytes"
	"github.com/wneessen/go-mail"
	"testing"
)

func TestSerialize(t *testing.T) {
	msg := mail.NewMsg()
	msg.From("huangjin@irs.gov")
	msg.To("poorman@gmail.com")
	msg.SetBodyString(mail.TypeTextPlain, "We need all your money. Now.")

	var buf bytes.Buffer
	_, err := msg.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}

	println(buf.String())
}
