package certmagicgcs

import (
	"context"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/certmagic"
)

// Interface guards
var (
	_ caddyfile.Unmarshaler  = (*CaddyStorageGCS)(nil)
	_ caddy.StorageConverter = (*CaddyStorageGCS)(nil)
)

// CaddyStorageGCS implements a caddy storage backend for Google Cloud Storage.
type CaddyStorageGCS struct {
	// BucketName is the name of the storage bucket.
	BucketName string `json:"string"`
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
	return NewStorage(context.TODO(), s.BucketName)
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
			if value != "" {
				s.BucketName = value
			}
		}
	}
	return nil
}
