package audio

import (
	"io"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_BasicWriteRead(t *testing.T) {
	rb := NewRingBuffer(1024)
	data := []byte("hello world")
	n, err := rb.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}
	if rb.Used() != len(data) {
		t.Errorf("Used() = %d, want %d", rb.Used(), len(data))
	}

	buf := make([]byte, 1024)
	n, err = rb.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Read() = %d, want %d", n, len(data))
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("Read() = %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := NewRingBuffer(100)
	first := make([]byte, 60)
	for i := range first {
		first[i] = byte(i)
	}
	if _, err := rb.Write(first); err != nil {
		t.Fatalf("Write() first error = %v", err)
	}

	discard := make([]byte, 30)
	if _, err := rb.Read(discard); err != nil {
		t.Fatalf("Read() discard error = %v", err)
	}

	second := make([]byte, 50)
	for i := range second {
		second[i] = byte(i + 100)
	}
	if _, err := rb.Write(second); err != nil {
		t.Fatalf("Write() second error = %v", err)
	}

	buf := make([]byte, 80)
	_, err := rb.Read(buf)
	if err != nil {
		t.Fatalf("Read() final error = %v", err)
	}
	for i := 30; i < 60; i++ {
		if buf[i-30] != byte(i) {
			t.Errorf("buf[%d] = %d, want %d", i-30, buf[i-30], i)
		}
	}
	for i := range 50 {
		if buf[30+i] != byte(i+100) {
			t.Errorf("buf[%d] = %d, want %d", 30+i, buf[30+i], i+100)
		}
	}
}

func TestRingBuffer_FillLevel(t *testing.T) {
	rb := NewRingBuffer(100)
	if level := rb.FillLevel(); level != 0 {
		t.Errorf("FillLevel() = %f, want 0", level)
	}

	rb.Write([]byte{1, 2, 3})
	if level := rb.FillLevel(); level != 0.03 {
		t.Errorf("FillLevel() = %f, want 0.03", level)
	}
}

func TestRingBuffer_Close(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("test"))
	if err := rb.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err := rb.Write([]byte("more"))
	if err != ErrClosed {
		t.Errorf("Write() after close = %v, want %v", err, ErrClosed)
	}

	buf := make([]byte, 100)
	n, err := rb.Read(buf)
	if err != nil {
		t.Errorf("Read() with data after close = %v", err)
	}
	if n != 4 {
		t.Errorf("Read() with data = %d, want 4", n)
	}
	if string(buf[:n]) != "test" {
		t.Errorf("Read() data = %q, want %q", string(buf[:n]), "test")
	}

	_, err = rb.Read(buf)
	if err != io.EOF {
		t.Errorf("Read() after draining = %v, want %v", err, io.EOF)
	}
}

func TestRingBuffer_Available(t *testing.T) {
	rb := NewRingBuffer(100)
	if avail := rb.Available(); avail != 100 {
		t.Errorf("Available() = %d, want 100", avail)
	}

	rb.Write([]byte{1, 2, 3})
	if avail := rb.Available(); avail != 97 {
		t.Errorf("Available() after write = %d, want 97", avail)
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	rb := NewRingBuffer(1024 * 1024)

	var wg sync.WaitGroup
	data := []byte("test data")
	for range 10 {
		wg.Go(func() {
			for range 100 {
				rb.Write(data)
				time.Sleep(time.Microsecond)
			}
		})
	}
	for range 10 {
		wg.Go(func() {
			buf := make([]byte, 9)
			for range 100 {
				rb.Read(buf)
				time.Sleep(time.Microsecond)
			}
		})
	}
	wg.Wait()
}

func TestRingBuffer_PartialWrite(t *testing.T) {
	rb := NewRingBuffer(10)
	large := []byte("hello world this is longer than buffer")
	n, err := rb.Write(large)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 10 {
		t.Errorf("Write() partial = %d, want 10", n)
	}

	buf := make([]byte, 10)
	n, err = rb.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 10 {
		t.Errorf("Read() = %d, want 10", n)
	}
}

func TestRingBuffer_PartialRead(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello world"))
	buf := make([]byte, 5)
	n, err := rb.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Read() = %d, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("Read() = %q, want %q", string(buf), "hello")
	}

	remaining := make([]byte, 20)
	n, err = rb.Read(remaining)
	if err != nil {
		t.Fatalf("Read() remaining error = %v", err)
	}
	if n != 6 {
		t.Errorf("Read() remaining = %d, want 6", n)
	}
	if string(remaining[:n]) != " world" {
		t.Errorf("Read() remaining = %q, want %q", string(remaining[:n]), " world")
	}
}

func TestRingBuffer_Discard(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello world"))
	if count := rb.Discard(6); count != 6 {
		t.Errorf("Discard() = %d, want 6", count)
	}

	buf := make([]byte, 5)
	n, _ := rb.Read(buf)
	if string(buf[:n]) != "world" {
		t.Errorf("Read() after discard = %q, want %q", string(buf[:n]), "world")
	}
}

func TestRingBuffer_Peek(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello world"))
	buf := make([]byte, 5)
	n, err := rb.Peek(buf, 6)
	if err != nil {
		t.Fatalf("Peek() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Peek() = %d, want 5", n)
	}
	if string(buf[:n]) != "world" {
		t.Errorf("Peek() = %q, want %q", string(buf[:n]), "world")
	}

	count := rb.Used()
	if count != 11 {
		t.Errorf("Used() after peek = %d, want 11", count)
	}
}

func TestRingBuffer_EmptyRead(t *testing.T) {
	rb := NewRingBuffer(100)
	buf := make([]byte, 10)
	_, err := rb.Read(buf)
	if err != ErrEmpty {
		t.Errorf("Read() empty = %v, want %v", err, ErrEmpty)
	}
}

func TestRingBuffer_FullWrite(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("0123456789"))
	_, err := rb.Write([]byte("X"))
	if err != ErrFull {
		t.Errorf("Write() full = %v, want %v", err, ErrFull)
	}
}

func TestRingBuffer_Size(t *testing.T) {
	rb := NewRingBuffer(0)
	if size := rb.Size(); size != DefaultBufferSize {
		t.Errorf("Size() default = %d, want %d", size, DefaultBufferSize)
	}

	rb = NewRingBuffer(2048)
	if size := rb.Size(); size != 2048 {
		t.Errorf("Size() custom = %d, want 2048", size)
	}
}
