package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	"github.com/multiformats/go-multihash"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/routing"

	"github.com/p-society/raag/internal/logger"
)

type Stack struct {
	host       host.Host
	blockstore blockstore.Blockstore
	dht        routing.ContentRouting
}

func NewStack(ctx context.Context, h host.Host, dht routing.ContentRouting, bsDS ds.Batching) (*Stack, error) {
	bs := blockstore.NewBlockstore(bsDS)
	bs = blockstore.NewIdStore(bs)
	return &Stack{
		host:       h,
		blockstore: bs,
		dht:        dht,
	}, nil
}

func (s *Stack) AddFile(ctx context.Context, path string) (cid.Cid, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cid.MustParse(""), fmt.Errorf("read file: %w", err)
	}

	h, _ := multihash.Sum(data, multihash.SHA2_256, -1)
	c := cid.NewCidV1(cid.Raw, h)

	b, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		return cid.MustParse(""), fmt.Errorf("create block: %w", err)
	}

	if err := s.blockstore.Put(ctx, b); err != nil {
		return cid.MustParse(""), fmt.Errorf("put block: %w", err)
	}

	logger.Infof("Added file to blockstore cid=%s path=%s", c, path)
	return c, nil
}

func (s *Stack) FetchFile(ctx context.Context, c cid.Cid, destPath string) error {
	b, err := s.blockstore.Get(ctx, c)
	if err != nil {
		return fmt.Errorf("get block: %w", err)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".raag-fetch-*.part")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}

	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(b.RawData()); err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	logger.Infof("Fetched file from network cid=%s dest=%s", c, destPath)
	return os.Rename(tmpPath, destPath)
}

func (s *Stack) HasBlock(ctx context.Context, c cid.Cid) (bool, error) {
	return s.blockstore.Has(ctx, c)
}

func (s *Stack) Close() error {
	return nil
}
