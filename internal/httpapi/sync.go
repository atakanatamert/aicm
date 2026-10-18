// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"

	"github.com/atakanatamert/aicm/internal/syncer"
)

// SyncAccount queues a fetch now, respecting the provider's poll interval.
func (s *Server) SyncAccount(ctx context.Context, req SyncAccountRequestObject) (SyncAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	err = s.Sync.Request(ctx, p, req.AccountId, s.Jobs)
	var soon *syncer.TooSoonError
	switch {
	case err == nil:
		return SyncAccount202JSONResponse{Queued: true}, nil
	case errors.Is(err, syncer.ErrNotFound):
		return nil, NotFound("account not found")
	case errors.Is(err, syncer.ErrNothingToSync):
		return SyncAccount409JSONResponse(errorBody("nothing_to_sync", err.Error())), nil
	case errors.As(err, &soon):
		secs := int(soon.RetryAfter.Seconds())
		return SyncAccount429JSONResponse{
			Body:    errorBody("too_soon", err.Error()),
			Headers: SyncAccount429ResponseHeaders{RetryAfter: &secs},
		}, nil
	}
	return nil, err
}

// errorBody builds the standard error envelope for typed error responses.
func errorBody(code, message string) Error {
	var e Error
	e.Error.Code = code
	e.Error.Message = message
	return e
}
