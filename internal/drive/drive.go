package drive

import (
	"fmt"
	"io"
	"time"
)

const (
	lockFile    = "cloudstash.lock"
	lockTimeout = 5 * time.Minute
)

// Metadata contains name, size (in bytes), and content hash of a remote file
type Metadata struct {
	Name string
	Size uint64
	Hash string
}

// Drive is the interface every remote drive client should implement
type Drive interface {

	// GetProviderName returns name of the drive. i.e. dropbox
	GetProviderName() string

	// GetFile returns metadata and reader of the requested remote file
	// If file couldn't be found, it should return common.ErrNotFound
	GetFile(name string) (io.ReadCloser, error)

	// PutFile uploads specified file to the remote drive
	// It overwrites if the file exists
	PutFile(name string, content io.Reader) error

	// GetFileMetadata returns metadata of the remote file
	GetFileMetadata(name string) (*Metadata, error)

	// DeleteFile removes file from the remote drive
	DeleteFile(name string) error

	// MoveFile renames file
	MoveFile(name string, newName string) error

	// Lock create lock file on remote drive
	// If there is a lock file on remote drive already,
	// it should block until the remote lock is removed
	// If it takes longer than the lockTimeout, the lock should
	// be removed manually by Lock() and whole process should
	// start again from the beginning
	Lock() error

	// Unlock removes lock from remote drive
	Unlock() error

	// ComputeHash computes hash of file with drive's specific method.
	// This function is used as another thread, so return values should be
	// printed to channels.
	// r: reader of content to hash
	// hchan: hash channel. computed hash should be printed to this channel
	// echan: error channel. any error occurred should be printed to this channel
	ComputeHash(r io.Reader, hchan chan string, echan chan error)

	// GetAvailableSpace returns available space in bytes
	GetAvailableSpace() (int64, error)
}

// GetURL creates URL of remote file
// i.e. dropbox://filename.ext
func GetURL(drv Drive, name string) string {
	scheme := drv.GetProviderName()

	if name[0] == '/' {
		name = name[1:]
	}

	return fmt.Sprintf("%s://%s", scheme, name)
}
