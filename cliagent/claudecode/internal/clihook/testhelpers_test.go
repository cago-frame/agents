package clihook_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
)

func postUnix(t *testing.T, sock, id string, body []byte) ([]byte, error) {
	t.Helper()
	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", sock)
		},
	}
	c := &http.Client{Transport: tr}
	req, err := http.NewRequest(http.MethodPost, "http://x/", bytesReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hook", id)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
