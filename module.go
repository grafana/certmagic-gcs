package certmagicgcs

import (
	"context"
	"fmt"
	"os"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/certmagic"
	"github.com/google/tink/go/aead"
	"github.com/google/tink/go/insecurecleartextkeyset"
	"github.com/google/tink/go/keyset"
	"github.com/grafana/certmagic-gcs/storage"
)

// Interface guards
var (
	_ caddyfile.Unmarshaler  = (*CaddyStorageGCS)(nil)
	_ caddy.StorageConverter = (*CaddyStorageGCS)(nil)
)

// CaddyStorageGCS implements a caddy storage backend for Google Cloud Storage.
type CaddyStorageGCS struct {
	// BucketName is the name of the storage bucket.
	BucketName string `json:"bucket-name"`
	// EncryptionKeySet is the path of a json tink encryption keyset
	EncryptionKeySet string `json:"encryption-key-set"`
}

func init() {
	caddy.RegisterModule(CaddyStorageGCS{})
}

// CaddyModule returns the Caddy module information.
func (CaddyStorageGCS) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "caddy.storage.gcs",
		New: func() caddy.Module {
			return new(CaddyStorageGCS)
		},
	}
}

// CertMagicStorage returns a cert-magic storage.
func (s *CaddyStorageGCS) CertMagicStorage() (certmagic.Storage, error) {
	config := storage.Config{
		BucketName: s.BucketName,
	}

	if len(s.EncryptionKeySet) > 0 {
		f, err := os.Open(s.EncryptionKeySet)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		r := keyset.NewJSONReader(f)
		// TODO: Add the ability to read an encrypted keyset / or envelope encryption
		// see https://github.com/google/tink/blob/e5c9356ed471be08a63eb5ea3ad0e892544e5a1c/go/keyset/handle_test.go#L84-L86
		// or https://github.com/google/tink/blob/master/docs/GOLANG-HOWTO.md
		kh, err := insecurecleartextkeyset.Read(r)
		if err != nil {
			return nil, err
		}
		kp, err := aead.New(kh)
		if err != nil {
			return nil, err
		}
		config.AEAD = kp
	}
	return storage.NewStorage(context.Background(), config)
}

// Validate caddy gcs storage configuration.
func (s *CaddyStorageGCS) Validate() error {
	if s.BucketName == "" {
		return fmt.Errorf("bucket name must be defined")
	}
	return nil
}

// UnmarshalCaddyfile unmarshall caddy file.
func (s *CaddyStorageGCS) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		key := d.Val()
		var value string

		if !d.Args(&value) {
			continue
		}

		switch key {
		case "bucket-name":
			s.BucketName = value
		case "encryption-key-set":
			s.EncryptionKeySet = value
		}
	}
	return nil
}
