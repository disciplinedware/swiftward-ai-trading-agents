package sandbox

import (
	"strings"
	"testing"
)

func TestLimitWriter_ShortWrite(t *testing.T) {
	var buf strings.Builder
	limit := int64(10)
	lw := &limitWriter{w: &buf, limit: limit}

	// Case 1: Write exactly limit
	input1 := []byte("1234567890")
	n, err := lw.Write(input1)
	if err != nil {
		t.Fatalf("Unexpected error writing exact limit: %v", err)
	}
	if int64(n) != limit {
		t.Errorf("Expected to write %d bytes, got %d", limit, n)
	}
	if buf.String() != "1234567890" {
		t.Errorf("Buffer content mismatch: got %q", buf.String())
	}

	// Reset
	buf.Reset()
	lw = &limitWriter{w: &buf, limit: limit}

	// Case 2: Write MORE than limit (Should NOT fail, but truncate output)
	// io.Copy expects write to consume all bytes unless error
	input2 := []byte("1234567890EXTRA")
	n, err = lw.Write(input2)

	// If n < len(input2) and err == nil, io.Copy will return ErrShortWrite
	if n != len(input2) {
		t.Errorf("BUG REPRODUCED: limitWriter returned %d bytes written, expected input length %d. This causes io.ErrShortWrite.", n, len(input2))
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check content is truncated
	if buf.String() != "1234567890" {
		t.Errorf("Buffer content should be truncated to limit. Got: %q", buf.String())
	}
	if !lw.truncated {
		t.Error("limitWriter should have set truncated=true")
	}
}

func TestLimitWriter_AccumulatedWrite(t *testing.T) {
	var buf strings.Builder
	limit := int64(10)
	lw := &limitWriter{w: &buf, limit: limit}

	// Write 5 bytes
	n, err := lw.Write([]byte("12345"))
	if err != nil || n != 5 {
		t.Errorf("Write 1 failed: n=%d err=%v", n, err)
	}

	// Write 10 bytes (total 15 > 10)
	n, err = lw.Write([]byte("67890ABCDE"))
	// With the fix, we expect it to return 10 (length of input), even though it only wrote 5
	if n != 10 {
		t.Errorf("BUG REPRODUCED: Write 2 returned %d, expected 10", n)
	}
	if err != nil {
		t.Errorf("Write 2 failed: %v", err)
	}

	if buf.String() != "1234567890" {
		t.Errorf("Buffer content mismatch: got %q", buf.String())
	}
}
