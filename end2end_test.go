package certmagicgcs_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/caddyserver/certmagic"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/tink/go/aead"
	"github.com/google/tink/go/keyset"
	certmagicgcs "github.com/grafana/certmagic-gcs"
	"github.com/letsencrypt/pebble/ca"
	"github.com/letsencrypt/pebble/db"
	"github.com/letsencrypt/pebble/va"
	"github.com/letsencrypt/pebble/wfe"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/option"
)

const (
	testBucket = "some-bucket"
)

func testLogger(t *testing.T) *log.Logger {
	return log.New(testWriter{t}, "test", log.LstdFlags)
}

type testWriter struct {
	t *testing.T
}

func (tw testWriter) Write(p []byte) (n int, err error) {
	tw.t.Log(string(p))
	return len(p), nil
}

func pebbleHandler(t *testing.T) http.Handler {
	os.Setenv("PEBBLE_VA_ALWAYS_VALID", "1")
	os.Setenv("PEBBLE_VA_NOSLEEP", "1")
	t.Cleanup(func() {
		os.Unsetenv("PEBBLE_VA_ALWAYS_VALID")
		os.Unsetenv("PEBBLE_VA_NOSLEEP")
	})
	logger := testLogger(t)
	db := db.NewMemoryStore()
	ca := ca.New(logger, db, "", 0, 1, 100)
	va := va.New(logger, 80, 443, false, "")
	wfeImpl := wfe.New(logger, db, va, ca, false, false)
	return wfeImpl.Handler()
}
func TestGCSStorage(t *testing.T) {
	// start gcs fake server
	gcs := fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: testBucket,
				Name:       "some/object/",
			},
		},
	})
	defer gcs.Stop()

	// start let's encrypt
	pebble := httptest.NewTLSServer(pebbleHandler(t))
	defer pebble.Close()

	// Setup cert-magic
	certmagic.DefaultACME.CA = pebble.URL + "/dir"
	certmagic.DefaultACME.AltTLSALPNPort = 8443
	kh, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	assert.NoError(t, err)
	ks, err := aead.New(kh)
	assert.NoError(t, err)
	storage, err := certmagicgcs.NewStorage(context.Background(), certmagicgcs.StorageConfig{
		BucketName: testBucket,
		AEAD:       ks,
		ClientOptions: []option.ClientOption{
			option.WithHTTPClient(gcs.HTTPClient()),
			option.WithoutAuthentication(),
		},
	})
	assert.NoError(t, err)

	certmagic.Default.Storage = storage
	// Configure  cert pool
	pool := x509.NewCertPool()
	pool.AddCert(pebble.Certificate())
	certmagic.DefaultACME.TrustedRoots = pool

	// Create a test handler
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "Ok")
	})

	// Configure storage
	tlsConfig, err := certmagic.TLS([]string{"example.com"})
	assert.NoError(t, err)
	tlsConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsConfig.NextProtos...)

	// Start cert magic
	s := httptest.NewUnstartedServer(mux)
	s.TLS = tlsConfig
	s.StartTLS()
	defer s.Close()
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// Test request
	res, err := http.Get(s.URL)
	assert.NoError(t, err)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.NoError(t, err)
	assert.Equal(t, "Ok", string(body))
}
