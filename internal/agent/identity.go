package agent

import (
	"errors"
	"path/filepath"
)

// Identity is the agent's persisted worker identity. It is stored as JSON
// in identity.json inside the data directory.
type Identity struct {
	WorkerID string `json:"worker_id"`
	Secret   string `json:"secret"`
}

// ErrNoIdentity is returned by LoadIdentity when no identity file exists.
var ErrNoIdentity = errors.New("no saved identity")

// LoadIdentity reads the identity from dir/identity.json.
// Missing file -> ErrNoIdentity. Corrupt JSON -> a different error.
func LoadIdentity(dir string) (*Identity, error) {
	// TODO: implement per spec in docs/offline/step-1.6-identity.md
	return nil, ErrNoIdentity
}

// SaveIdentity writes id to dir/identity.json atomically (temp + rename),
// creating dir with mode 0700 if needed, final file mode 0600.
func SaveIdentity(dir string, id Identity) error {
	// TODO: implement per spec in docs/offline/step-1.6-identity.md
	return nil
}

// DeleteIdentity removes dir/identity.json. Missing file is NOT an error.
func DeleteIdentity(dir string) error {
	// TODO: implement per spec in docs/offline/step-1.6-identity.md
	return nil
}

// identityFilePath returns the path to the identity file inside dir.
func identityFilePath(dir string) string {
	return filepath.Join(dir, "identity.json")
}
