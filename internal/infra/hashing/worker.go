package hashing

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"sync"
)

type hashJob struct {
	file   *os.File
	result chan<- string
}

type HashWorker struct {
	queue    chan hashJob
	workers  int
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewHashWorker(workers int) *HashWorker {
	if workers <= 0 {
		workers = 4
	}
	return &HashWorker{
		queue:   make(chan hashJob, workers*2),
		workers: workers,
		done:    make(chan struct{}),
	}
}

// Submit queues an already-open file to be hashed. The worker hashes the file
// from the start; the caller retains ownership and must close the file. The
// returned channel yields the SHA-256 hex digest ("" on failure) and is closed
// after delivery.
func (w *HashWorker) Submit(file *os.File) <-chan string {
	result := make(chan string, 1)
	select {
	case w.queue <- hashJob{file: file, result: result}:
	case <-w.done:
		close(result)
	}
	return result
}

func (w *HashWorker) Start() {
	for range w.workers {
		w.wg.Go(w.worker)
	}
}

func (w *HashWorker) worker() {
	for {
		select {
		case <-w.done:
			return
		case job := <-w.queue:
			job.result <- computeFileHash(job.file)
			close(job.result)
		}
	}
}

func (w *HashWorker) Close() {
	w.stopOnce.Do(func() { close(w.done) })
	w.wg.Wait()
}

// computeFileHash hashes the file from its start, resetting the file position
// first. The caller owns and closes the file.
func computeFileHash(file *os.File) string {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		slog.Debug("failed to seek file for hashing", "error", err)
		return ""
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		slog.Debug("failed to hash file", "error", err)
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
