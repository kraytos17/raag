package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ipfs/boxo/bitswap"
	bsnet "github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	chunk "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	ufsio "github.com/ipfs/boxo/ipld/unixfs/io"
	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	format "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/p-society/raag/internal/logger"
)

type Stack struct {
	bs       blockstore.Blockstore
	exchange *bitswap.Bitswap
	bsvc     blockservice.BlockService
	dag      format.DAGService
	dht      routing.ContentRouting
}

func NewStack(ctx context.Context, h host.Host, dht routing.ContentRouting, bsDS ds.Batching) (*Stack, error) {
	if bsDS == nil {
		return nil, fmt.Errorf("transfer.NewStack: bsDS (blockstore datastore) must not be nil")
	}

	bs := blockstore.NewBlockstore(bsDS)
	bs = blockstore.NewIdStore(bs)
	bsNet := bsnet.NewFromIpfsHost(h, nil)
	exchange := bitswap.New(ctx, bsNet, dht, bs)
	bsvc := blockservice.New(bs, exchange)
	dag := merkledag.NewDAGService(bsvc)
	return &Stack{
		bs:       bs,
		exchange: exchange,
		bsvc:     bsvc,
		dag:      dag,
		dht:      dht,
	}, nil
}

func (s *Stack) AddFile(ctx context.Context, path string) (cid.Cid, error) {
	f, err := os.Open(path)
	if err != nil {
		return cid.MustParse(""), fmt.Errorf("AddFile open %q: %w", path, err)
	}
	defer f.Close()

	spl := chunk.NewSizeSplitter(f, chunk.DefaultBlockSize)
	dbp := helpers.DagBuilderParams{
		Dagserv:   s.dag,
		RawLeaves: true,
		Maxlinks:  helpers.DefaultLinksPerBlock,
		NoCopy:    false,
	}

	db, err := dbp.New(spl)
	if err != nil {
		return cid.MustParse(""), fmt.Errorf("AddFile dag builder %q: %w", path, err)
	}

	nd, err := balanced.Layout(db)
	if err != nil {
		return cid.MustParse(""), fmt.Errorf("AddFile layout %q: %w", path, err)
	}

	rootCID := nd.Cid()
	if s.dht != nil {
		if err := s.dht.Provide(ctx, rootCID, true); err != nil {
			logger.Warnf("AddFile: DHT Provide %s failed (non-fatal): %v", rootCID, err)
		}
	}

	logger.Infof("AddFile: ingested file path=%s root_cid=%s", path, rootCID)
	return rootCID, nil
}

func (s *Stack) FetchFile(ctx context.Context, c cid.Cid, destPath string) error {
	nd, err := s.dag.Get(ctx, c)
	if err != nil {
		return fmt.Errorf("FetchFile get root %s: %w", c, err)
	}

	dr, err := ufsio.NewDagReader(ctx, nd, s.dag)
	if err != nil {
		return fmt.Errorf("FetchFile dag reader %s: %w", c, err)
	}
	defer dr.Close()

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("FetchFile mkdir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".raag-fetch-*.part")
	if err != nil {
		return fmt.Errorf("FetchFile create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmp, dr); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("FetchFile copy %s: %w", c, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("FetchFile close temp: %w", err)
	}

	logger.Infof("FetchFile: complete cid=%s dest=%s", c, destPath)
	return os.Rename(tmpPath, destPath)
}

func (s *Stack) OpenStream(ctx context.Context, c cid.Cid) (ufsio.DagReader, error) {
	sessionDag := merkledag.NewDAGService(s.bsvc)

	nd, err := sessionDag.Get(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("OpenStream get root %s: %w", c, err)
	}

	dr, err := ufsio.NewDagReader(ctx, nd, sessionDag)
	if err != nil {
		return nil, fmt.Errorf("OpenStream dag reader %s: %w", c, err)
	}
	return dr, nil
}

func (s *Stack) HasBlock(ctx context.Context, c cid.Cid) (bool, error) {
	return s.bs.Has(ctx, c)
}

func (s *Stack) Blockstore() blockstore.Blockstore {
	return s.bs
}

func (s *Stack) Close() error {
	return s.exchange.Close()
}
