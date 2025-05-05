package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"cloud.google.com/go/storage"
	"github.com/caddyserver/certmagic"
	"github.com/google/tink/go/tink"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var (
	// LockExpiration is the duration before which a Lock is considered expired
	LockExpiration = 1 * time.Minute
	// LockPollInterval is the interval between each check of the lock state.
	LockPollInterval = 1 * time.Second
)

// Storage is a certmagic.Storage backed by a GCS bucket
type Storage struct {
	bucket *storage.BucketHandle
	aead   tink.AEAD
}

// Interface guards
var (
	_ certmagic.Storage = (*Storage)(nil)
	_ certmagic.Locker  = (*Storage)(nil)
)

type Config struct {
	// AEAD for Authenticated Encryption with Additional Data
	AEAD tink.AEAD
	// BucketName is the name of the GCS storage Bucket
	BucketName string
	// ClientOptions GCS storage client options
	ClientOptions []option.ClientOption
}

func NewStorage(ctx context.Context, config Config) (*Storage, error) {
	client, err := storage.NewClient(ctx, config.ClientOptions...)
	if err != nil {
		return nil, fmt.Errorf("could not initialize storage client: %w", err)
	}
	bucket := client.Bucket(config.BucketName)
	var kp tink.AEAD
	if config.AEAD != nil {
		kp = config.AEAD
	} else {
		kp = new(cleartext)
	}
	return &Storage{bucket: bucket, aead: kp}, nil
}

// Store puts value at key.
func (s *Storage) Store(ctx context.Context, key string, value []byte) error {
	w := s.bucket.Object(key).NewWriter(ctx)

	encrypted, err := s.aead.Encrypt(value, []byte(key))
	if err != nil {
		return fmt.Errorf("encrypting object %s: %w", key, err)
	}
	if _, err := w.Write(encrypted); err != nil {
		return fmt.Errorf("writing object %s: %w", key, err)
	}
	return w.Close()
}

// Load retrieves the value at key.
func (s *Storage) Load(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.bucket.Object(key).NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("loading object %s: %w", key, err)
	}
	defer rc.Close()

	encrypted, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading object %s: %w", key, err)
	}

	decrypted, err := s.aead.Decrypt(encrypted, []byte(key))
	if err != nil {
		return nil, fmt.Errorf("decrypting object %s: %w", key, err)
	}
	return decrypted, nil
}

// Delete deletes key. An error should be
// returned only if the key still exists
// when the method returns.
func (s *Storage) Delete(ctx context.Context, key string) error {
	err := s.bucket.Object(key).Delete(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return fs.ErrNotExist
	}
	if err != nil {
		return fmt.Errorf("deleting object %s: %w", key, err)
	}
	return nil
}

// Exists returns true if the key exists
// and there was no error checking.
func (s *Storage) Exists(ctx context.Context, key string) bool {
	_, err := s.bucket.Object(key).Attrs(ctx)
	return err == nil
}

// List returns all keys that match prefix.
// If recursive is true, non-terminal keys
// will be enumerated (i.e. "directories"
// should be walked); otherwise, only keys
// prefixed exactly by prefix will be listed.
func (s *Storage) List(ctx context.Context, prefix string, recursive bool) ([]string, error) {
	query := &storage.Query{Prefix: prefix}
	if !recursive {
		query.Delimiter = "/"
	}
	var names []string
	it := s.bucket.Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("listing objects: %w", err)
		}
		if attrs.Name != "" {
			names = append(names, attrs.Name)
		}
	}
	return names, nil
}

// Stat returns information about key.
func (s *Storage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {
	var keyInfo certmagic.KeyInfo
	attr, err := s.bucket.Object(key).Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return keyInfo, fs.ErrNotExist
	}
	if err != nil {
		return keyInfo, fmt.Errorf("loading attributes for %s: %w", key, err)
	}
	keyInfo.Key = key
	keyInfo.Modified = attr.Updated
	keyInfo.Size = attr.Size
	keyInfo.IsTerminal = true
	return keyInfo, nil
}

// Lock acquires the lock for key, blocking until the lock
// can be obtained or an error is returned. Note that, even
// after acquiring a lock, an idempotent operation may have
// already been performed by another process that acquired
// the lock before - so always check to make sure idempotent
// operations still need to be performed after acquiring the
// lock.
//
// The actual implementation of obtaining of a lock must be
// an atomic operation so that multiple Lock calls at the
// same time always results in only one caller receiving the
// lock at any given time.
//
// To prevent deadlocks, all implementations (where this concern
// is relevant) should put a reasonable expiration on the lock in
// case Unlock is unable to be called due to some sort of network
// failure or system crash. Additionally, implementations should
// honor context cancellation as much as possible (in case the
// caller wishes to give up and free resources before the lock
// can be obtained).
func (s *Storage) Lock(ctx context.Context, key string) error {
	lockKey := s.objLockName(key)
	obj := s.bucket.Object(lockKey)
	for {
		attrs, err := obj.Attrs(ctx)
		// create the lock if it doesn't exists
		if errors.Is(err, storage.ErrObjectNotExist) {
			w := obj.NewWriter(ctx)
			if _, err = w.Write([]byte{}); err != nil {
				return fmt.Errorf("creating %s: %w", lockKey, err)
			}
			if err = w.Close(); err != nil {
				return fmt.Errorf("closing %s: %w", lockKey, err)
			}
			continue
		} else if err != nil {
			return fmt.Errorf("loading attributes %s: %w", lockKey, err)
		}
		// Acquire the lock
		if !attrs.TemporaryHold {
			if _, err := obj.Update(ctx, storage.ObjectAttrsToUpdate{TemporaryHold: true}); err != nil {
				return fmt.Errorf("setting temporary hold on object %s: %w", lockKey, err)
			}
			return nil
		}
		// Unlock if the lock expired
		if attrs.Updated.Add(LockExpiration).Before(time.Now().UTC()) {
			if err := s.Unlock(ctx, key); err != nil {
				return fmt.Errorf("unlocking expired lock %s: %w", lockKey, err)
			}
			continue
		}
		// Wait and try again
		select {
		case <-time.After(LockPollInterval):
			continue // a no-op since it's at the end of the loop, but nice to be explicit
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Unlock releases the lock for key. This method must ONLY be
// called after a successful call to Lock, and only after the
// critical section is finished, even if it errored or timed
// out. Unlock cleans up any resources allocated during Lock.
func (s *Storage) Unlock(ctx context.Context, key string) error {
	lockKey := s.objLockName(key)
	obj := s.bucket.Object(lockKey)
	_, err := obj.Update(ctx, storage.ObjectAttrsToUpdate{TemporaryHold: false})
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not remove temporary hold for %s: %w", lockKey, err)
	}
	if err := s.Delete(ctx, s.objLockName(key)); err != nil {
		return fmt.Errorf("delting lock %s: %w", lockKey, err)
	}
	return nil
}

func (s *Storage) objLockName(key string) string {
	return key + ".lock"
}

// cleartext implements tink.AAED interface, but simply store the object in plaintext
type cleartext struct{}

// encrypt returns the unencrypted plaintext data.
func (cleartext) Encrypt(plaintext, _ []byte) ([]byte, error) {
	return plaintext, nil
}

// decrypt returns the ciphertext as plaintext
func (cleartext) Decrypt(ciphertext, _ []byte) ([]byte, error) {
	return ciphertext, nil
}
