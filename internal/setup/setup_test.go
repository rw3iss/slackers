package setup

import "testing"

func fixture() Config {
	return Config{
		ClientID:     "123.456",
		ClientSecret: "deadbeefcafefeed",
		AppToken:     "xapp-1-ABC-789-zyx",
		UserToken:    "xoxp-1-USER-XYZ",
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := fixture()
	hash, err := Encode(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if hash == "" {
		t.Fatal("encoded hash is empty")
	}
	got, err := Decode(hash)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestEncodeOmitsEmptyFields(t *testing.T) {
	c := Config{ClientID: "abc"}
	hash, err := Encode(c)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(hash)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ClientID != "abc" {
		t.Errorf("client id: got %q", got.ClientID)
	}
	if got.ClientSecret != "" || got.AppToken != "" || got.UserToken != "" {
		t.Errorf("empty fields leaked: %+v", got)
	}
}

func TestDecodeGarbage(t *testing.T) {
	cases := []string{"", "not base64!!!", "aGVsbG8", "{not json}"}
	for _, s := range cases {
		if _, err := Decode(s); err == nil {
			t.Errorf("decode %q: expected error, got nil", s)
		}
	}
}

func TestParseAnyJSON(t *testing.T) {
	js := `{"client_id":"abc","client_secret":"s","app_token":"xapp-1","user_token":"xoxp-1"}`
	got, err := ParseAny(js)
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if got != fixtureJSON() {
		t.Errorf("json parse mismatch: %+v", got)
	}
}

func fixtureJSON() Config {
	return Config{
		ClientID:     "abc",
		ClientSecret: "s",
		AppToken:     "xapp-1",
		UserToken:    "xoxp-1",
	}
}

func TestParseAnyFlags(t *testing.T) {
	cases := []string{
		`--client-id abc --client-secret s --app-token xapp-1 --user-token xoxp-1`,
		`--client_id=abc --client_secret=s --app_token=xapp-1 --user_token=xoxp-1`,
	}
	for _, in := range cases {
		got, err := ParseAny(in)
		if err != nil {
			t.Errorf("parse %q: %v", in, err)
			continue
		}
		if got != fixtureJSON() {
			t.Errorf("flags parse mismatch for %q: %+v", in, got)
		}
	}
}

func TestParseAnyHash(t *testing.T) {
	want := fixture()
	hash, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAny(hash)
	if err != nil {
		t.Fatalf("parse hash: %v", err)
	}
	if got != want {
		t.Errorf("hash parse mismatch")
	}
}

func TestParseAnyEmpty(t *testing.T) {
	if _, err := ParseAny(""); err == nil {
		t.Error("empty input should error")
	}
	if _, err := ParseAny("   "); err == nil {
		t.Error("whitespace input should error")
	}
}

func TestParseFlagsNoFields(t *testing.T) {
	if _, err := ParseFlags([]string{"--unknown", "foo"}); err == nil {
		t.Error("unknown flags only should error")
	}
}

func TestMaskSecret(t *testing.T) {
	c := Config{
		ClientID:     "public_id",
		ClientSecret: "deadbeefcafefeed",
		AppToken:     "xapp-1-ABC",
	}
	m := c.Mask()
	if m.ClientID != c.ClientID {
		t.Errorf("client id should not be masked")
	}
	if m.ClientSecret == c.ClientSecret {
		t.Errorf("secret should be masked")
	}
	if !contains(m.ClientSecret, "…") {
		t.Errorf("mask should include ellipsis, got %q", m.ClientSecret)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsEmpty(t *testing.T) {
	if !(Config{}).IsEmpty() {
		t.Error("zero value should be empty")
	}
	if (Config{ClientID: "a"}).IsEmpty() {
		t.Error("non-zero should not be empty")
	}
}
