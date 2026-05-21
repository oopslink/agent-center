package peek

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

// Client is the center-side peek-trace client. Dials a worker daemon
// socket and yields trace lines back to the CLI handler via a channel.
type Client struct {
	socketPath string
}

// NewClient constructs a client.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Frame is what Stream sends to the caller. Exactly one of Line, Err is
// set per Frame; Done==true is the final frame.
type Frame struct {
	Line string
	Err  *ErrorPayload
	Done bool
}

// ErrConnectFailed wraps dial errors.
type ErrConnectFailed struct{ Cause error }

func (e *ErrConnectFailed) Error() string { return fmt.Sprintf("peek client: dial: %v", e.Cause) }

// Unwrap returns the underlying cause.
func (e *ErrConnectFailed) Unwrap() error { return e.Cause }

// Stream sends a single Request and reads server frames until done /
// connection close / ctx cancel. Returned channel is closed on completion.
//
// Use case:
//
//	frames, err := c.Stream(ctx, peek.Request{ExecutionID:"E-1", Last:10, Follow:true})
//	if err != nil { return err }
//	for f := range frames {
//	    if f.Err != nil { ... }
//	    if f.Done { break }
//	    fmt.Println(f.Line)
//	}
func (c *Client) Stream(ctx context.Context, req Request) (<-chan Frame, error) {
	if req.ExecutionID == "" {
		return nil, fmt.Errorf("%w: execution_id required", ErrInvalidRequest)
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, &ErrConnectFailed{Cause: err}
	}
	// Send the request line.
	reqBytes, err := json.Marshal(req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	reqBytes = append(reqBytes, '\n')
	if _, err := conn.Write(reqBytes); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("peek client: write: %w", err)
	}
	out := make(chan Frame, 32)
	go func() {
		defer close(out)
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			select {
			case <-ctx.Done():
				out <- Frame{Err: &ErrorPayload{Reason: ReasonStreamCanceled, Message: "client cancelled"}}
				return
			default:
			}
			line, err := r.ReadBytes('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				// Network error → surface as ErrorPayload.
				out <- Frame{Err: &ErrorPayload{Reason: ReasonStreamCanceled, Message: err.Error()}}
				return
			}
			var resp Response
			if err := json.Unmarshal(line, &resp); err != nil {
				out <- Frame{Err: &ErrorPayload{Reason: ReasonInvalidRequest, Message: err.Error()}}
				return
			}
			if resp.Done {
				out <- Frame{Done: true}
				return
			}
			if resp.Error != nil {
				out <- Frame{Err: resp.Error}
				return
			}
			out <- Frame{Line: resp.Line}
		}
	}()
	return out, nil
}
