package data

import (
	"encoding/base64"
	"net/url"
	"testing"
)

func TestJSONStringify(t *testing.T) {
	type testJSON struct {
		Project string `json:"project"`
	}
	const src = "{\"project\":\"superflare\"}"
	dest, err := jsonStringify(testJSON{Project: "superflare"})
	if err != nil {
		t.Fatalf("jsonStringify returned error: %v", err)
	}

	if src != dest {
		t.Fatal("JSON Stringify Error")
	}

	// mock incorrect data
	_, err = jsonStringify(make(chan int))
	if err == nil {
		t.Fatal("JSON Stringify Error")
	}
}

func TestRedirectEncodingSurvivesQueryParsing(t *testing.T) {
	for _, target := range []string{
		"https://example.com/~", "https://example.com/?q=a+b&next=%2Fpath", "http://[fd00::1]:5000/path",
	} {
		encoded := Base64EncodeUrl(target)
		query, err := url.ParseQuery("go=" + encoded)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []string{encoded, query.Get("go"), base64.StdEncoding.EncodeToString([]byte(target))} {
			decoded, err := Base64DecodeUrl(input)
			if err != nil || string(decoded) != target {
				t.Errorf("decode %q = %q, %v; want %q", input, decoded, err, target)
			}
		}
	}
}

func TestMaskTextWithStars(t *testing.T) {
	if MaskTextWithStars("1234") != "1**4" {
		t.Fatal("MaskTextWithStars Error")
	}

	if MaskTextWithStars("123") != "1*3" {
		t.Fatal("MaskTextWithStars Error")
	}

	if MaskTextWithStars("12") != "12" {
		t.Fatal("MaskTextWithStars Error")
	}

	if MaskTextWithStars("1") != "1" {
		t.Fatal("MaskTextWithStars Error")
	}

	if MaskTextWithStars("") != "" {
		t.Fatal("MaskTextWithStars Error")
	}
}

func TestGenerateRandomString(t *testing.T) {
	name1 := GenerateRandomString(8)
	if len(name1) != 8 {
		t.Fatal("GenerateRandomString length error")
	}
}

func TestBase64EncodeUrl(t *testing.T) {
	const input = "https://example.com/path"
	encoded := Base64EncodeUrl(input)
	if encoded == "" || encoded == input {
		t.Fatal("Base64EncodeUrl should encode and query-escape")
	}
	decoded, err := Base64DecodeUrl(encoded)
	if err != nil {
		t.Fatalf("Base64EncodeUrl result should be decodable: %v", err)
	}
	if string(decoded) != input {
		t.Fatalf("Base64EncodeUrl roundtrip: got %q", string(decoded))
	}
}

func TestBase64DecodeUrl(t *testing.T) {
	const input = "https://example.com"
	encoded := Base64EncodeUrl(input)
	decoded, err := Base64DecodeUrl(encoded)
	if err != nil {
		t.Fatalf("Base64DecodeUrl: %v", err)
	}
	if string(decoded) != input {
		t.Fatalf("Base64DecodeUrl: got %q", string(decoded))
	}

	_, err = Base64DecodeUrl("%")
	if err == nil {
		t.Fatal("Base64DecodeUrl invalid input should return error")
	}
}
