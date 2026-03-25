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
	path   string
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

func (w *HashWorker) Submit(path string) <-chan string {
	result := make(chan string, 1)
	select {
	case w.queue <- hashJob{path: path, result: result}:
	case <-w.done:
		close(result)
	}
	return result
}

func (w *HashWorker) Start() {
	for range w.workers {
		w.wg.Add(1)
		go w.worker()
	}
}

func (w *HashWorker) worker() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case job := <-w.queue:
			job.result <- computeFileHash(job.path)
			close(job.result)
		}
	}
}

func (w *HashWorker) Close() {
	w.stopOnce.Do(func() { close(w.done) })
	w.wg.Wait()
}

func computeFileHash(path string) string {
	file, err := os.Open(path)
	if err != nil {
		slog.Debug("failed to open file for hashing", "path", path, "error", err)
		return ""
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Debug("failed to close file after hashing", "path", path, "error", err)
		}
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		slog.Debug("failed to hash file", "path", path, "error", err)
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}
