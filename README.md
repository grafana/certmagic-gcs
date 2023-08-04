# Certmagic Storage Backend for Google Cloud Storage

This library allows you to use Google Cloud Storage as key/certificate storage backend for your [Certmagic](https://github.com/caddyserver/certmagic)-enabled HTTPS server. To protect your keys from unwanted attention, client-side encryption is possible.

## Usage

### Caddy

In this section, we create a caddy config using our GCS storage.

#### Getting started

1. Create a `Caddyfile`
    ```
    {
      storage gcs {
        bucket-name some-bucket
      }
    }
    localhost
    acme_server
    respond "Hello Caddy Storage GCS!"
    ```
2. Start GCS emulator
    ```console
    $ docker run -d \
        -p 9023:9023 \
        --name gcp-storage-emulator \
        oittaa/gcp-storage-emulator \
        start --default-bucket=some-bucket --port 9023 --in-memory
    $ export STORAGE_EMULATOR_HOST=http://localhost:9023
    ```
3. Start caddy
    ```console
    $ xcaddy run
    ```
4. Check that it works
    ```console
    $ open https://localhost
    ```

### Client Side Encryption

This module supports client side encryption using [google Tink](https://github.com/google/tink), thus providing a simple way to customize the encryption algorithm and handle key rotation. To get started: 

1. Install [tinkey](https://github.com/google/tink/blob/master/docs/TINKEY.md)
2. Create a key set
    ```console
    $ tinkey create-keyset --key-template AES128_GCM_RAW --out keyset.json
    ```
    Here is an example keyset.json:
    ```json
    {
      "primaryKeyId": 1818673287,
      "key": [
        {
          "keyData": {
            "typeUrl": "type.googleapis.com/google.crypto.tink.AesGcmKey",
            "value": "GhDEQ/4v72esAv3rbwZyS+ls",
            "keyMaterialType": "SYMMETRIC"
          },
          "status": "ENABLED",
          "keyId": 1818673287,
          "outputPrefixType": "RAW"
        }
      ]
    }
    ```
3. Start caddy with the following config
    ```
    {
      storage gcs {
        bucket-name some-bucket
        encryption-key-set ./keyset.json
      }
    }
    localhost
    acme_server
    respond "Hello Caddy Storage GCS!"
    ```
4. restart the fake gcs backend to start with an empty bucket
    ```console
    $ docker restart gcp-storage-emulator
    $ # start caddy
    $ xcaddy run
    $ # to rotate the key-set
    $ tinkey rotate-keyset --in keyset.json  --key-template AES128_GCM_RAW
    ```

### CertMagic

1. Add the package:

```console
go get github.com/grafana/certmagic-gcs
```

2. Create a `certmagicgcs.NewStorage` with a `certmagicgcs.StorageConfig`:

```golang
import certmagicgcs "github.com/grafana/certmagic-gcs/storage"

bucket := "my-example-bucket"

gcs, _ := certmagicgcs.NewStorage(
  context.Background(), 
  &certmagicgcs.StorageConfig{BucketName: bucket}
)
```

3. Optionally, [register as default storage](https://github.com/caddyserver/certmagic#storage).

```golang
certmagic.Default.Storage = gcs
```

## License

This module is distributed under [AGPL-3.0-only](LICENSE).
