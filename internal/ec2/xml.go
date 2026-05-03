package ec2

import (
	"bytes"
	"encoding/xml"
	"net/http"
)

const ec2XMLNS = "http://ec2.amazonaws.com/doc/2016-11-15/"
const ec2QueryVersion = "2016-11-15"

type ec2ErrResponse struct {
	XMLName   xml.Name    `xml:"Response"`
	Errors    ec2ErrBlock `xml:"Errors"`
	RequestID string      `xml:"RequestID"`
}

type ec2ErrBlock struct {
	Error ec2ErrItem `xml:"Error"`
}

type ec2ErrItem struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message, rid string) {
	t := ec2ErrResponse{
		Errors:    ec2ErrBlock{Error: ec2ErrItem{Code: code, Message: message}},
		RequestID: rid,
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	_ = enc.Encode(&t)
	_ = enc.Flush()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("x-amzn-RequestId", rid)
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeXML(w http.ResponseWriter, status int, rid string, v any) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	_ = enc.Encode(v)
	_ = enc.Flush()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("x-amzn-RequestId", rid)
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
