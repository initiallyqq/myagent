package sse

import (
	"encoding/json"
	"fmt"
	"io"
)

// Writer wraps an http.ResponseWriter (via gin's c.Writer) for SSE output.
// Each call to Send emits one "data: ...\n\n" frame.
type Writer struct {
	w     io.Writer
	flush func()
}

// New creates an SSE Writer. Pass c.Writer and c.Writer.Flush from Gin.
func New(w io.Writer, flush func()) *Writer {
	return &Writer{w: w, flush: flush}
}

// Send serialises v to JSON and emits it as one SSE data frame.
func (sw *Writer) Send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", data); err != nil {
		return err
	}
	sw.flush()
	return nil
}

// SendText emits a raw string as one SSE data frame (useful for heartbeats).
func (sw *Writer) SendText(text string) error {
	_, err := fmt.Fprintf(sw.w, "data: %s\n\n", text)
	if err != nil {
		return err
	}
	sw.flush()
	return nil
}

// Done emits a terminal SSE frame signalling the stream is complete.
func (sw *Writer) Done() {
	fmt.Fprintf(sw.w, "data: [DONE]\n\n")
	sw.flush()
}
