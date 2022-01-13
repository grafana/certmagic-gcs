package certmagicgcs

import (
	"context"
	"io/ioutil"
	"testing"

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
	s, err := NewStorage(context.Background(), StorageConfig{
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

	// Exists
	assert.False(t, s.Exists(key))

	// Create
	err := s.Store(key, []byte(content))
	assert.NoError(t, err)

	assert.True(t, s.Exists(key))

	out, err := s.Load(key)
	assert.NoError(t, err)
	assert.Equal(t, content, string(out))

	// Stat
	stat, err := s.Stat(key)
	assert.NoError(t, err)
	assert.Equal(t, key, stat.Key)
	assert.EqualValues(t, len(content), stat.Size)
	assert.True(t, stat.IsTerminal)

	// Delete
	err = s.Delete(key)
	assert.NoError(t, err)
	assert.False(t, s.Exists(key))
}

func TestDeleteOnlyIfKeyStillExists(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/1.txt"}},
	})
	err := s.Delete("/does/not/exists")
	assert.ErrorAs(t, err, &storage.ErrObjectNotExist)
}

func TestList(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/1.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c1/2.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c1/3.txt"}},
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c2/d/4.txt"}},
	})
	res, err := s.List("/a/b/", false)
	assert.NoError(t, err)
	assert.Equal(t, []string{"/a/b/1.txt"}, res)

	res, err = s.List("/a/b/c1/", true)
	assert.NoError(t, err)
	assert.Equal(t, []string{"/a/b/c1/2.txt", "/a/b/c1/3.txt"}, res)
}

func TestLock(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a/b/c"}},
	})
	ctx := context.Background()
	err := s.Lock(ctx, "a")
	assert.NoError(t, err)
	_, err = s.bucket.Object("a.lock").Attrs(ctx)
	assert.NoError(t, err)
}

func TestUnlock(t *testing.T) {
	s := setupTestStorage(t, []fakestorage.Object{
		{ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: "/a.lock"}},
	})
	err := s.Unlock("a")
	assert.NoError(t, err)
	_, err = s.bucket.Object("a.lock").Attrs(context.Background())
	assert.ErrorAs(t, err, &storage.ErrObjectNotExist)
}

func TestEncryption(t *testing.T) {
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
	s.aead = &cleartext{}
	key := "some/object/file.txt"
	content := "data"

	// store encrypted data
	err = s.Store(key, []byte(content))
	assert.NoError(t, err)

	// ensure the object is encrypted in storage
	rc, err := s.bucket.Object(key).NewReader(context.TODO())
	assert.NoError(t, err)
	defer rc.Close()
	encrypted, err := ioutil.ReadAll(rc)
	assert.NoError(t, err)
	decrypted, err := s.aead.Decrypt(encrypted, []byte(key))
	assert.NoError(t, err)
	assert.Equal(t, string(decrypted), content)

	// ensure load decrypts the object
	out, err := s.Load(key)
	assert.NoError(t, err)
	assert.Equal(t, content, string(out))
}
