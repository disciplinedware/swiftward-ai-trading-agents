package swiftward

import (
	"context"
	"fmt"
	"time"

	proto "ai-trading-agents/internal/proto"

	"github.com/vmihailenco/msgpack/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// VerdictType represents the policy evaluation verdict.
type VerdictType string

const (
	VerdictApproved VerdictType = "approved"
	VerdictFlagged  VerdictType = "flagged"
	VerdictRejected VerdictType = "rejected"
)

// EvalResult holds the evaluation result.
type EvalResult struct {
	ID       string
	Verdict  VerdictType
	Response map[string]any
}

// ShouldBlock returns true if the request should be blocked.
func (r *EvalResult) ShouldBlock() (bool, string) {
	if r.Verdict == VerdictRejected || r.Verdict == VerdictFlagged {
		reason := "Policy violation"
		if r.Response != nil {
			if v, ok := r.Response["reason"].(string); ok && v != "" {
				reason = v
			}
		}
		return true, reason
	}
	if r.Response != nil {
		if behavior, ok := r.Response["behavior"].(string); ok && behavior == "block" {
			reason := "Blocked by policy"
			if v, ok := r.Response["reason"].(string); ok && v != "" {
				reason = v
			}
			return true, reason
		}
	}
	return false, ""
}

// EvalError represents an evaluation error.
type EvalError struct {
	Code    codes.Code
	Message string
	Err     error
}

func (e *EvalError) Error() string {
	return fmt.Sprintf("evaluation error: code=%s message=%s err=%v", e.Code, e.Message, e.Err)
}

func (e *EvalError) IsTransient() bool {
	switch e.Code {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

// Evaluator handles gRPC-based policy evaluation via the Swiftward ingestion service.
type Evaluator struct {
	grpcAddr string
	client   proto.IngestionServiceClient
	conn     *grpc.ClientConn
	timeout  time.Duration
	log      *zap.Logger
}

// NewEvaluator creates a new evaluator that connects to the Swiftward ingestion service.
func NewEvaluator(grpcAddr string, timeoutStr string, log *zap.Logger) (*Evaluator, error) {
	timeout := 5 * time.Second
	if timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			log.Warn("Invalid ingestion_timeout, using default",
				zap.String("configured", timeoutStr),
				zap.Duration("default", timeout),
				zap.Error(err),
			)
		} else {
			timeout = d
		}
	}

	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{
			"loadBalancingPolicy": "round_robin"
		}`),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.MaxCallRecvMsgSize(10*1024*1024),
			grpc.MaxCallSendMsgSize(10*1024*1024),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: false,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	client := proto.NewIngestionServiceClient(conn)

	return &Evaluator{
		grpcAddr: grpcAddr,
		client:   client,
		conn:     conn,
		timeout:  timeout,
		log:      log,
	}, nil
}

// EvaluateSync performs synchronous policy evaluation.
func (e *Evaluator) EvaluateSync(ctx context.Context, stream, entityID, eventType string, eventData map[string]any) (*EvalResult, error) {
	dataMsgPack, err := msgpack.Marshal(eventData)
	if err != nil {
		return nil, &EvalError{
			Code:    codes.Internal,
			Message: "failed to marshal event data",
			Err:     err,
		}
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	req := &proto.IngestionEvent{
		Event: &proto.EventPayload{
			Stream:   stream,
			Type:     eventType,
			EntityId: entityID,
			Data:     dataMsgPack,
		},
	}

	resp, err := e.client.IngestSync(ctx, req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			return nil, &EvalError{
				Code:    st.Code(),
				Message: st.Message(),
				Err:     err,
			}
		}
		return nil, &EvalError{
			Code:    codes.Unknown,
			Message: err.Error(),
			Err:     err,
		}
	}

	result := &EvalResult{
		ID:      resp.Id,
		Verdict: VerdictType(resp.Verdict),
	}

	if len(resp.Response) > 0 {
		var respMap map[string]any
		if unmarshalErr := msgpack.Unmarshal(resp.Response, &respMap); unmarshalErr == nil {
			result.Response = respMap
		}
	}

	return result, nil
}

// EvaluateAsync performs fire-and-forget async evaluation.
func (e *Evaluator) EvaluateAsync(ctx context.Context, stream, entityID, eventType string, eventData map[string]any) (string, error) {
	dataMsgPack, err := msgpack.Marshal(eventData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal event data: %w", err)
	}

	asyncTimeout := 2 * time.Second
	ctx, cancel := context.WithTimeout(ctx, asyncTimeout)
	defer cancel()

	req := &proto.IngestionEvent{
		Event: &proto.EventPayload{
			Stream:   stream,
			Type:     eventType,
			EntityId: entityID,
			Data:     dataMsgPack,
		},
	}

	resp, err := e.client.IngestAsync(ctx, req)
	if err != nil {
		return "", fmt.Errorf("async evaluation failed: %w", err)
	}

	return resp.Id, nil
}

// IsReady checks if the gRPC connection is healthy.
func (e *Evaluator) IsReady() bool {
	state := e.conn.GetState()
	return state == connectivity.Ready || state == connectivity.Idle
}

// Close closes the gRPC connection.
func (e *Evaluator) Close() error {
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}
