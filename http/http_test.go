package http

import "testing"

func TestClient(t *testing.T) {
	var (
		err               error
		respGet, respPost []byte
		cli               = NewClient("")
	)
	if respGet, err = cli.Get("https://httpbin.org/get"); err != nil {
		t.Fatal(err)
	}
	t.Log(string(respGet))

	if respPost, err = cli.Post("https://httpbin.org/post", nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Log(string(respPost))
}
