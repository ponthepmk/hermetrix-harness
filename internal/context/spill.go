package context

import (
	stdcontext "context"
	"crypto/sha256"
	"encoding/hex"

	"hermetrix-harness/internal/blob"
)

type Spiller interface {
	Spill(ctx stdcontext.Context, mime string, data []byte) (SpillReceipt, error)
}

type BlobSpiller struct{ store *blob.Store }

func NewBlobSpiller(store *blob.Store) *BlobSpiller { return &BlobSpiller{store: store} }

func (s *BlobSpiller) Spill(_ stdcontext.Context, mime string, data []byte) (SpillReceipt, error) {
	ref, err := s.store.Put(data)
	if err != nil {
		return SpillReceipt{}, err
	}
	sum := sha256.Sum256(data)
	return SpillReceipt{Ref: ref, MIME: mime, Bytes: len(data), Checksum: hex.EncodeToString(sum[:])}, nil
}
