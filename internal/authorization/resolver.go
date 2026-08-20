package authorization

import (
	"context"
	"errors"
	"strings"
)

// Resolver is the single stable authorization entry point for production
// callers. HTTP, MCP, admin, and background code must depend on this interface,
// not on Service.Check directly.
type Resolver interface {
	Authorize(context.Context, CheckRequest) (AccessDecision, error)
}

type resolver struct {
	service *Service
}

func NewResolver(service *Service) Resolver {
	return resolver{service: service}
}

func (r resolver) Authorize(ctx context.Context, req CheckRequest) (AccessDecision, error) {
	decision := AccessDecision{
		SubjectRef: req.SubjectRef,
		Permission: req.Permission,
		Resource:   req.Resource,
		Reason:     "permission_denied",
	}
	if r.service == nil {
		decision.Reason = "authorization_not_wired"
		return decision, ErrDenied
	}
	if strings.TrimSpace(string(req.Transport)) == "" {
		decision.Reason = "transport_required"
		return decision, ErrInvalid
	}
	decision, err := r.service.Check(ctx, req)
	if err != nil {
		return decision, err
	}
	if !decision.Allowed {
		return decision, ErrDenied
	}
	return decision, nil
}

func Authorize(ctx context.Context, r Resolver, req CheckRequest) (AccessDecision, error) {
	if r == nil {
		return AccessDecision{
			SubjectRef: req.SubjectRef,
			Permission: req.Permission,
			Resource:   req.Resource,
			Reason:     "authorization_not_wired",
		}, ErrDenied
	}
	return r.Authorize(ctx, req)
}

func IsDenied(err error) bool {
	return errors.Is(err, ErrDenied) || errors.Is(err, ErrUnauthenticated)
}
