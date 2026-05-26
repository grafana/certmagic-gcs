package storage

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/tink/go/aead"
	"github.com/google/tink/go/keyset"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/option"
)

const (
	testBucket = "some-bucket"
)

func setupTestStorage(t *testing.T, objects []fakestorage.Object) *Storage {
	server := fakestorage.NewServer(objects)
	defer server.Stop()
	s, err := NewStorage(t.Context(), Config{
		BucketName: testBucket,
		ClientOptions: []option.ClientOption{
			option.WithHTTPClient(server.HTTPClient()),
			option.WithoutAuthentication(),
		},
	})
	assert.NoError(t, err)
	return s
}

func TestSimpleOperations(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: testBucket,
				Name:       "some/object/",
			},
		},
	})
	key := "some/object/file.txt"
	content := "data"

	ctx := t.Context()

	// Exists
	assert.False(t, s.Exists(ctx, key))

	// Create
	err := s.Store(ctx, key, []byte(content))
	assert.NoError(t, err)

	assert.True(t, s.Exists(ctx, key))

	out, err := s.Load(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, content, string(out))

	// Stat
	stat, err := s.Stat(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, key, stat.Key)
	assert.EqualValues(t, len(content), stat.Size)
	assert.True(t, stat.IsTerminal)

	// Delete
	err = s.Delete(ctx, key)
	assert.NoError(t, err)
	assert.False(t, s.Exists(ctx, key))
}

func TestDeleteOnlyIfKeyStillExists(t *testing.T) {
	ctx := t.Context()
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/1.txt"}},
	})
	err := s.Delete(ctx, "/does/not/exists")
	assert.ErrorAs(t, err, &storage.ErrObjectNotExist)
}

func TestList(t *testing.T) {
	ctx := t.Context()
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/1.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c1/2.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c1/3.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c2/d/4.txt"}},
	})
	res, err := s.List(ctx, "/a/b/", false)
	assert.NoError(t, err)
	assert.Equal(t, []string{"/a/b/1.txt"}, res)

	res, err = s.List(ctx, "/a/b/c1/", true)
	assert.NoError(t, err)
	assert.Equal(t, []string{"/a/b/c1/2.txt", "/a/b/c1/3.txt"}, res)
}

func TestLock(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c"}},
	})
	ctx := t.Context()
	err := s.Lock(ctx, "a")
	assert.NoError(t, err)
	_, err = s.bucket.Object("a.lock").Attrs(ctx)
	assert.NoError(t, err)
}

func TestUnlock(t *testing.T) {
	ctx := t.Context()
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a.lock"}},
	})
	err := s.Unlock(ctx, "a")
	assert.NoError(t, err)
	_, err = s.bucket.Object("a.lock").Attrs(ctx)
	assert.ErrorAs(t, err, &storage.ErrObjectNotExist)
}

func TestEncryption(t *testing.T) {
	ctx := t.Context()
	s := setupTestStorage(t, []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: testBucket,
				Name:       "some/object/",
			},
		},
	})
	kh, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	assert.NoError(t, err)
	kp, err := aead.New(kh)
	assert.NoError(t, err)

	s.aead = kp
	key := "some/object/file.txt"
	content := "data"

	// store encrypted data
	err = s.Store(ctx, key, []byte(content))
	assert.NoError(t, err)

	// ensure the object is encrypted in storage
	rc, err := s.bucket.Object(key).NewReader(ctx)
	assert.NoError(t, err)
	defer rc.Close()
	encrypted, err := io.ReadAll(rc)
	assert.NoError(t, err)
	assert.NotEqual(t, string(encrypted), content)

	decrypted, err := s.aead.Decrypt(encrypted, []byte(key))
	assert.NoError(t, err)
	assert.Equal(t, string(decrypted), content)

	// ensure load decrypts the object
	out, err := s.Load(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, content, string(out))
}

func TestErrNotExist(t *testing.T) {
	ctx := t.Context()
	s := setupTestStorage(t, []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: testBucket,
				Name:       "some/object/",
			},
		},
	})
	key := "does/not/exists"
	_, err := s.Load(ctx, key)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	err = s.Delete(ctx, key)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, err = s.Stat(ctx, key)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func setupFuzzStorage(f *testing.F, objects []fakestorage.Object) *Storage {
	f.Helper()
	server := fakestorage.NewServer(objects)
	f.Cleanup(server.Stop)
	s, err := NewStorage(context.Background(), Config{
		BucketName: testBucket,
		ClientOptions: []option.ClientOption{
			option.WithHTTPClient(server.HTTPClient()),
			option.WithoutAuthentication(),
		},
	})
	if err != nil {
		f.Fatal(err)
	}
	return s
}

func FuzzStoreLoadRoundTrip(f *testing.F) {
	f.Add("certs/example.com/cert.pem", []byte("certificate data"))
	f.Add("keys/private.key", []byte("private key content"))
	f.Add("a", []byte(""))
	f.Add("path/with spaces/file.txt", []byte("\x00\xff\xfe"))
	f.Add("key\x00with\x00nulls", []byte("value"))

	s := setupFuzzStorage(f, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "seed/"}},
	})

	f.Fuzz(func(t *testing.T, key string, value []byte) {
		if key == "" || key == "." || key == ".." {
			t.Skip("invalid GCS object name")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		if err := s.Store(ctx, key, value); err != nil {
			t.Skip("store rejected key")
		}
		loaded, err := s.Load(ctx, key)
		if err != nil {
			t.Fatalf("Load(%q) failed after successful Store: %v", key, err)
		}
		if !bytes.Equal(value, loaded) {
			t.Fatalf("round-trip failed for key %q: got %d bytes, want %d bytes", key, len(loaded), len(value))
		}
	})
}

func FuzzList(f *testing.F) {
	f.Add("/a/b/", true)
	f.Add("/a/b/", false)
	f.Add("", true)
	f.Add("nonexistent/", false)
	f.Add("/", true)

	s := setupFuzzStorage(f, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/x/y/10.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/x/y/z1/20.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/x/y/z1/30.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/x/y/z2/w/40.txt"}},
	})

	f.Fuzz(func(t *testing.T, prefix string, recursive bool) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, _ = s.List(ctx, prefix, recursive)
	})
}

func FuzzObjLockName(f *testing.F) {
	f.Add("certs/example.com/cert.pem")
	f.Add("")
	f.Add("../../../etc/passwd")
	f.Add(strings.Repeat("a", 10000))
	f.Add("key\x00with\x00nulls")

	f.Fuzz(func(t *testing.T, key string) {
		s := &Storage{}
		result := s.objLockName(key)
		if !strings.HasSuffix(result, ".lock") {
			t.Fatalf("expected .lock suffix, got %q", result)
		}
	})
}
