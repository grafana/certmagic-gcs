module github.com/grafana/certmagic-gcs

go 1.16

require (
	cloud.google.com/go/iam v0.3.0 // indirect
	cloud.google.com/go/storage v1.21.1-0.20220301044418-a34503bc0f0b
	github.com/caddyserver/caddy/v2 v2.4.6
	github.com/caddyserver/certmagic v0.15.2
	github.com/fsouza/fake-gcs-server v1.37.9
	github.com/google/tink/go v1.6.1
	github.com/letsencrypt/pebble v1.0.2-0.20211028190950-4cce110cac5a
	github.com/stretchr/testify v1.7.1
	google.golang.org/api v0.74.0
)
