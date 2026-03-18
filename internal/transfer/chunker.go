package transfer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"os"

	"github.com/p-society/raag/internal/logger"
)

const (
	DefaultBlockSize = 512 * 1024
	MaxBlockSize     = 4 * 1024 * 1024
	MinBlockSize     = 64 * 1024
)

var (
	ErrInvalidBlockSize = errors.New("invalid block size")
	ErrBlockVerifyFail  = errors.New("block hash verification failed")
)

type Block struct {
	Index  int
	Data   []byte
	Hash   []byte
	Size   int
	Offset int64
	CID    []byte
}

type ChunkInfo struct {
	Index       int
	Size        int
	Hash        []byte
	Offset      int64
	BlockSize   int
	TotalSize   int64
	TotalBlocks int
}

type Chunker struct {
	blockSize int
	hasher    func() hash.Hash
}

func NewChunker(blockSize int) *Chunker {
	if blockSize == 0 {
		blockSize = DefaultBlockSize
	}
	if blockSize < MinBlockSize {
		blockSize = MinBlockSize
	}
	if blockSize > MaxBlockSize {
		blockSize = MaxBlockSize
	}
	return &Chunker{
		blockSize: blockSize,
		hasher:    sha256.New,
	}
}

func (c *Chunker) BlockSize() int {
	return c.blockSize
}

func (c *Chunker) CalculateBlocks(fileSize int64) int {
	if fileSize == 0 {
		return 0
	}
	return int((fileSize + int64(c.blockSize) - 1) / int64(c.blockSize))
}

func (c *Chunker) ChunkInfo(fileSize int64) []ChunkInfo {
	numBlocks := c.CalculateBlocks(fileSize)
	info := make([]ChunkInfo, numBlocks)

	remaining := fileSize
	offset := int64(0)
	for i := range numBlocks {
		blockSize := c.blockSize
		if int64(blockSize) > remaining {
			blockSize = int(remaining)
		}
		info[i] = ChunkInfo{
			Index:       i,
			Size:        blockSize,
			Offset:      offset,
			BlockSize:   c.blockSize,
			TotalSize:   fileSize,
			TotalBlocks: numBlocks,
		}

		remaining -= int64(blockSize)
		offset += int64(blockSize)
	}
	return info
}

func (c *Chunker) HashBlock(data []byte) []byte {
	h := c.hasher()
	h.Write(data)
	return h.Sum(nil)
}

func (c *Chunker) HashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := c.hasher()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func (c *Chunker) ReadBlock(f *os.File, index int) (*Block, error) {
	offset := int64(index * c.blockSize)
	_, err := f.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	data := make([]byte, c.blockSize)
	n, err := f.Read(data)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n == 0 {
		return nil, io.EOF
	}

	data = data[:n]
	hash := c.HashBlock(data)
	return &Block{
		Index:  index,
		Data:   data,
		Hash:   hash,
		Size:   n,
		Offset: offset,
	}, nil
}

func (c *Chunker) VerifyBlock(block *Block) bool {
	computed := c.HashBlock(block.Data)
	if len(computed) != len(block.Hash) {
		return false
	}
	for i := range computed {
		if computed[i] != block.Hash[i] {
			return false
		}
	}
	return true
}

func (c *Chunker) WriteBlock(f *os.File, block *Block) error {
	_, err := f.Seek(block.Offset, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = f.Write(block.Data)
	if err != nil {
		return err
	}
	return f.Sync()
}

func (c *Chunker) WriteBlocksToFile(blocks []*Block, outputPath string, totalSize int64) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, block := range blocks {
		if err := c.WriteBlock(f, block); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != totalSize {
		logger.Warnf("File size mismatch: expected %d, got %d", totalSize, info.Size())
	}
	return nil
}

func (c *Chunker) AssembleFromBlocks(blockPath string, blocks map[int][]byte, order []int, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, idx := range order {
		data, ok := blocks[idx]
		if !ok {
			logger.Warnf("Missing block %d during assembly", idx)
			continue
		}

		offset := int64(idx * c.blockSize)
		_, err := f.WriteAt(data, offset)
		if err != nil {
			return err
		}
	}
	return f.Sync()
}

type BlockRequest struct {
	Index    int
	Offset   int64
	Size     int
	Checksum []byte
}

func (r BlockRequest) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 4+8+4+4+len(r.Checksum))
	binary.BigEndian.PutUint32(buf[0:4], uint32(r.Index))
	binary.BigEndian.PutUint64(buf[4:12], uint64(r.Offset))
	binary.BigEndian.PutUint32(buf[12:16], uint32(r.Size))
	binary.BigEndian.PutUint32(buf[16:20], uint32(len(r.Checksum)))
	copy(buf[20:], r.Checksum)
	return buf, nil
}

func UnmarshalBlockRequest(data []byte) (*BlockRequest, error) {
	if len(data) < 20 {
		return nil, errors.New("data too short")
	}

	checksumLen := int(binary.BigEndian.Uint32(data[16:20]))
	if len(data) < 20+checksumLen {
		return nil, errors.New("data too short for checksum")
	}
	return &BlockRequest{
		Index:    int(binary.BigEndian.Uint32(data[0:4])),
		Offset:   int64(binary.BigEndian.Uint64(data[4:12])),
		Size:     int(binary.BigEndian.Uint32(data[12:16])),
		Checksum: data[20 : 20+checksumLen],
	}, nil
}

type BlockResponse struct {
	Index  int
	Data   []byte
	Hash   []byte
	Status BlockStatus
}

type BlockStatus uint8

const (
	BlockOK BlockStatus = iota
	BlockNotFound
	BlockCorrupt
	BlockError
)

func (r BlockResponse) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 4+4+len(r.Data)+len(r.Hash)+1)
	binary.BigEndian.PutUint32(buf[0:4], uint32(r.Index))
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(r.Data)))
	offset := 8
	copy(buf[offset:offset+len(r.Data)], r.Data)
	offset += len(r.Data)
	copy(buf[offset:offset+len(r.Hash)], r.Hash)
	offset += len(r.Hash)
	buf[offset] = byte(r.Status)
	return buf, nil
}

func UnmarshalBlockResponse(data []byte) (*BlockResponse, error) {
	if len(data) < 9 {
		return nil, errors.New("data too short")
	}

	dataLen := int(binary.BigEndian.Uint32(data[4:8]))
	hashLen := len(data) - 9 - dataLen
	if hashLen < 0 {
		return nil, errors.New("invalid data length")
	}
	return &BlockResponse{
		Index:  int(binary.BigEndian.Uint32(data[0:4])),
		Data:   data[8 : 8+dataLen],
		Hash:   data[8+dataLen : 8+dataLen+hashLen],
		Status: BlockStatus(data[len(data)-1]),
	}, nil
}
