package certmagicgcs

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func FuzzUnmarshalCaddyfile(f *testing.F) {
	f.Add("bucket-name my-bucket")
	f.Add("encryption-key-set /path/to/keyset.json")
	f.Add("bucket-name my-bucket\nencryption-key-set /path/to/keys.json")
	f.Add("")
	f.Add("unknown-key some-value")
	f.Add("bucket-name")

	f.Fuzz(func(t *testing.T, input string) {
		tokens, err := caddyfile.Tokenize([]byte(input), "fuzz")
		if err != nil {
			t.Skip("invalid caddyfile syntax")
		}
		d := caddyfile.NewDispenser(tokens)
		s := &CaddyStorageGCS{}
		_ = s.UnmarshalCaddyfile(d)
	})
}
