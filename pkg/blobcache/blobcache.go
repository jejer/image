package blobcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/internal/image"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/types"
	digest "github.com/opencontainers/go-digest"
	perrors "github.com/pkg/errors"
)

var (
	_ types.ImageReference   = &BlobCache{}
	_ types.ImageSource      = &blobCacheSource{}
	_ types.ImageDestination = &blobCacheDestination{}
)

const (
	compressedNote   = ".compressed"
	decompressedNote = ".decompressed"
)

// BlobCache is an object which saves copies of blobs that are written to it while passing them
// through to some real destination, and which can be queried directly in order to read them
// back.
//
// Implements types.ImageReference.
type BlobCache struct {
	reference types.ImageReference
	// WARNING: The contents of this directory may be accessed concurrently,
	// both within this process and by multiple different processes
	directory string
	compress  types.LayerCompression
}

func makeFilename(blobSum digest.Digest, isConfig bool) string {
	if isConfig {
		return blobSum.String() + ".config"
	}
	return blobSum.String()
}

// NewBlobCache creates a new blob cache that wraps an image reference.  Any blobs which are
// written to the destination image created from the resulting reference will also be stored
// as-is to the specified directory or a temporary directory.
// The compress argument controls whether or not the cache will try to substitute a compressed
// or different version of a blob when preparing the list of layers when reading an image.
func NewBlobCache(ref types.ImageReference, directory string, compress types.LayerCompression) (*BlobCache, error) {
	if directory == "" {
		return nil, fmt.Errorf("error creating cache around reference %q: no directory specified", transports.ImageName(ref))
	}
	switch compress {
	case types.Compress, types.Decompress, types.PreserveOriginal:
		// valid value, accept it
	default:
		return nil, fmt.Errorf("unhandled LayerCompression value %v", compress)
	}
	return &BlobCache{
		reference: ref,
		directory: directory,
		compress:  compress,
	}, nil
}

func (b *BlobCache) Transport() types.ImageTransport {
	return b.reference.Transport()
}

func (b *BlobCache) StringWithinTransport() string {
	return b.reference.StringWithinTransport()
}

func (b *BlobCache) DockerReference() reference.Named {
	return b.reference.DockerReference()
}

func (b *BlobCache) PolicyConfigurationIdentity() string {
	return b.reference.PolicyConfigurationIdentity()
}

func (b *BlobCache) PolicyConfigurationNamespaces() []string {
	return b.reference.PolicyConfigurationNamespaces()
}

func (b *BlobCache) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return b.reference.DeleteImage(ctx, sys)
}

func (b *BlobCache) HasBlob(blobinfo types.BlobInfo) (bool, int64, error) {
	if blobinfo.Digest == "" {
		return false, -1, nil
	}

	for _, isConfig := range []bool{false, true} {
		filename := filepath.Join(b.directory, makeFilename(blobinfo.Digest, isConfig))
		fileInfo, err := os.Stat(filename)
		if err == nil && (blobinfo.Size == -1 || blobinfo.Size == fileInfo.Size()) {
			return true, fileInfo.Size(), nil
		}
		if !os.IsNotExist(err) {
			return false, -1, perrors.Wrap(err, "checking size")
		}
	}

	return false, -1, nil
}

func (b *BlobCache) Directory() string {
	return b.directory
}

func (b *BlobCache) ClearCache() error {
	f, err := os.Open(b.directory)
	if err != nil {
		return err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return perrors.Wrapf(err, "error reading directory %q", b.directory)
	}
	for _, name := range names {
		pathname := filepath.Join(b.directory, name)
		if err = os.RemoveAll(pathname); err != nil {
			return perrors.Wrapf(err, "clearing cache for %q", transports.ImageName(b))
		}
	}
	return nil
}

func (b *BlobCache) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return image.FromReference(ctx, sys, b)
}
