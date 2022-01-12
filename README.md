# Certmagic Storage Backend for Google Cloud Storage 

This library allows you to use Google Cloud Storage as key/certificate storage backend for your [Certmagic](https://github.com/caddyserver/certmagic)-enabled HTTPS server. To protect your keys from unwanted attention, client-side encryption is possible.


### Development

In this section, we create an caddy confi using our GCS storage.

1. Create a `Caddyfile`

```
{
	storage gcs {
    bucket some-bucket
  }
}
localhost
acme_server
respond "Hello Caddy Storage GCS!"
```

2. Start GCS emulator

```bash
docker run -d \
  -p 9023:9023 \
  --name gcp-storage-emulator \
  oittaa/gcp-storage-emulator \
  start --default-bucket=some-bucket --port 9023 --in-memory


export STORAGE_EMULATOR_HOST=http://localhost:9023
```

3. Start caddy

```bash
xcaddy run
```

4. Check that it works

```bash
open https://localhost
```