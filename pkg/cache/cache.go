package cache

import (
	"crypto/sha256"
	"github.com/farmersedgeinc/yaml-crypt/pkg/config"
	"github.com/prologic/bitcask"
	"os"
	"path/filepath"
)

const (
	// Length to hash plaintext and ciphertext keys.
	hashLength = 16
	// Prefix for keys containing a hashed plaintext, used to look up ciphertext.
	plaintextKeyPrefix = 'p'
	// Prefix for keys containing a hashed ciphertext, used to look up plaintext.
	ciphertextKeyPrefix = 'c'
	// Name of the directory to store the caches in
	cacheDirName = ".yamlcrypt.cache"
)

// Max young cache size: 100MiB by default (can be shrunk for tests)
var youngCacheSize int64 = (1024 ^ 2) * 100

// A quick and dirty "LRU-ish" cache.
// Maintains a read/write "young" cache, and a read-only "old" cache.
// New values are added to the "young" cache.
// When looking up a value, if it's present in the "young" cache, retrieve it from there. If it's present in the "old" cache, retrieve it from there, copying it into the "young" cache.
// When the "young" cache gets too big, the current "old" cache is removed and the current "young" cache takes its place.
type Cache struct {
	young     *bitcask.Bitcask
	youngPath string
	old       *bitcask.Bitcask
	oldPath   string
}

// Initialize the cache.
func Setup(config config.Config) (Cache, error) {
	cache := Cache{
		youngPath: filepath.Join(config.Root, cacheDirName, "young"),
		oldPath:   filepath.Join(config.Root, cacheDirName, "old"),
	}
	var err error
	cache.young, err = bitcask.Open(
		cache.youngPath,
		bitcask.WithAutoRecovery(true),
	)
	if err != nil {
		return cache, err
	}
	cache.old, err = bitcask.Open(
		cache.oldPath,
		bitcask.WithAutoRecovery(true),
	)
	return cache, err
}

// Close the cache, doing some cleanup as well. Must be called before exiting
func (c *Cache) Close() error {
	// we only need to merge young, because old is read-only
	mergeErr := c.young.Merge()
	stats, statsErr := c.young.Stats()
	// we want to close if at all possible, so we'll handle merge/stats errors later
	err := c.young.Close()
	if err != nil {
		return err
	}
	err = c.old.Close()
	if err != nil {
		return err
	}
	if mergeErr != nil {
		return mergeErr
	}
	if statsErr != nil {
		return statsErr
	}
	// if the young cache size is too big, get rid of the old cache and make the young cache take its place.
	if stats.Size > youngCacheSize {
		err := os.RemoveAll(c.oldPath)
		if err != nil {
			return err
		}
		return os.Rename(c.oldPath, c.youngPath)
	}
	return nil
}

// Look up the ciphertext for a given plaintext.
func (c *Cache) Encrypt(plaintext string) ([]byte, bool, error) {
	return []byte{}, false, nil
	key := plaintextToKey(plaintext)
	if c.young.Has(key) {
		value, err := c.young.Get(key)
		return value, true, err
	} else if c.old.Has(key) {
		ciphertext, err := c.old.Get(key)
		if err != nil {
			return []byte{}, false, err
		}
		err = c.Add(plaintext, ciphertext)
		return ciphertext, true, err
	}
	return []byte{}, false, nil
}

// Look up the plaintext for a given ciphertext.
func (c *Cache) Decrypt(ciphertext []byte) (string, bool, error) {
	key := ciphertextToKey(ciphertext)
	if c.young.Has(key) {
		value, err := c.young.Get(key)
		return string(value), true, err
	} else if c.old.Has(key) {
		value, err := c.old.Get(key)
		if err != nil {
			return "", false, err
		}
		plaintext := string(value)
		err = c.Add(plaintext, ciphertext)
		return plaintext, true, err
	}
	return "", false, nil
}

// Add a (plaintext, ciphertext) pair to the young cache.
func (c *Cache) Add(plaintext string, ciphertext []byte) error {
	err := c.young.Put(plaintextToKey(plaintext), ciphertext)
	if err != nil {
		return err
	}
	err = c.young.Put(ciphertextToKey(ciphertext), []byte(plaintext))
	if err != nil {
		return err
	}
	return c.young.Sync()
}

// Convert a ciphertext to the key used to lookup its plaintext.
func ciphertextToKey(data []byte) []byte {
	key := make([]byte, 1, hashLength+1)
	key[0] = ciphertextKeyPrefix
	key = append(key, hash(data)...)
	return key
}

// Convert a plaintext to the key used to lookup its ciphertext.
func plaintextToKey(data string) []byte {
	key := make([]byte, 1, hashLength+1)
	key[0] = plaintextKeyPrefix
	key = append(key, hash([]byte(data))...)
	return key
}

// Hash some bytes, truncating the length to the hashLength constant.
func hash(data []byte) []byte {
	result := sha256.Sum256(data)
	return result[:hashLength]
}
