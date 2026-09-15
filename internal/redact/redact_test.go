package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	r := New([]string{"0123456789abcdef", "short", "komga-key-XYZ"})
	cases := map[string]string{
		`level=INFO msg=req key=0123456789abcdef`:                         `level=INFO msg=req key=***`,
		`url=http://admin:hunter22@komga:25600/api`:                       `url=http://***:***@komga:25600/api`,
		`GET /api/v1/series?apikey=abc123def&page=2`:                      `GET /api/v1/series?apikey=***&page=2`,
		`header X-Api-Key: zzzzzzzz`:                                      `header X-Api-Key: ***`,
		`Authorization: Bearer eyJhbGciOi.xx`:                             `Authorization: Bearer ***`,
		`{"password":"p4ss!word","user":"ann"}`:                           `{"password":"***","user":"ann"}`,
		`settings token=komga-key-XYZ done`:                               `settings token=*** done`,
		`module failed err="komga-key-XYZ rejected"`:                      `module failed err="*** rejected"`,
		`a short word stays; keep the keep_last_n=3 setting and tokenize`: `a short word stays; keep the keep_last_n=3 setting and tokenize`,
	}
	for in, want := range cases {
		if got := r.String(in); got != want {
			t.Errorf("\n in: %s\ngot: %s\nwant: %s", in, got, want)
		}
	}
}

func TestCopy(t *testing.T) {
	var out bytes.Buffer
	if err := New([]string{"supersecret"}).Copy(&out, strings.NewReader("a supersecret\nb\n")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a ***\nb\n" {
		t.Fatalf("%q", out.String())
	}
}
