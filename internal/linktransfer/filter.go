package linktransfer

import (
	"bytes"
	"io"
	"os"
	"sync"
)

type stderrFilter struct {
	real       *os.File
	pw         *os.File
	pr         *os.File
	done       chan struct{}
	mu         sync.Mutex
	suppressed bool
}

func newStderrFilter() *stderrFilter {
	pr, pw, _ := os.Pipe()
	f := &stderrFilter{
		real: os.Stderr,
		pw:   pw,
		pr:   pr,
		done: make(chan struct{}),
	}
	os.Stderr = pw
	go f.filterLoop()
	return f
}

func (f *stderrFilter) filterLoop() {
	defer close(f.done)
	buf := make([]byte, 4096)
	var pending []byte
	for {
		n, err := f.pr.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			pending = f.flush(pending)
		}
		if err != nil {
			if len(pending) > 0 {
				f.real.Write(pending)
			}
			return
		}
	}
}

func (f *stderrFilter) flush(data []byte) []byte {
	f.mu.Lock()
	suppressed := f.suppressed
	f.mu.Unlock()

	if !suppressed {
		f.real.Write(data)
		return nil
	}

	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			return data
		}
		line := data[:idx+1]
		data = data[idx+1:]

		if !shouldSuppressLine(line) {
			f.real.Write(line)
		}
	}
}

func shouldSuppressLine(line []byte) bool {
	s := bytes.TrimSpace(line)
	if len(s) == 0 {
		return true
	}
	if bytes.HasPrefix(s, []byte("croc ")) {
		return true
	}
	if bytes.HasPrefix(s, []byte("Code is:")) {
		return true
	}
	if bytes.HasPrefix(s, []byte("On the other computer")) {
		return true
	}
	if bytes.HasPrefix(s, []byte("(For ")) {
		return true
	}
	if bytes.Contains(s, []byte("lt recv")) || bytes.Contains(s, []byte("CROC_SECRET")) {
		return true
	}
	if bytes.HasPrefix(s, []byte("Copied")) {
		return true
	}
	return false
}

func (f *stderrFilter) suppress(on bool) {
	f.mu.Lock()
	f.suppressed = on
	f.mu.Unlock()
}

func (f *stderrFilter) restore() {
	f.pw.Close()
	<-f.done
	os.Stderr = f.real
}

func withSuppressedStderr(fn func() error) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return fn()
	}
	saved := os.Stderr
	os.Stderr = pw

	var fnErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(io.Discard, pr)
	}()

	fnErr = fn()

	pw.Close()
	wg.Wait()
	os.Stderr = saved
	return fnErr
}
