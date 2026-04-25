package freshness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileSHA256_KnownHash(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "x.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello"), 0600))

	hash, err := FileSHA256(file)
	require.NoError(t, err)
	// SHA-256 of "hello"
	require.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", hash)
	require.Len(t, hash, 64)
}

func TestFileSHA256_NonExistent(t *testing.T) {
	_, err := FileSHA256("/nonexistent/path/to/file.go")
	require.Error(t, err)
}
