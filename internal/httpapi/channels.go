// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/atakanatamert/aicm/internal/notify"
)

// mapChannelError maps the channel service's sentinel errors to HTTP.
func mapChannelError(err error) error {
	switch {
	case errors.Is(err, notify.ErrChannelForbidden):
		return ErrForbidden
	case errors.Is(err, notify.ErrChannelNotFound):
		return NotFound("notification channel not found")
	case errors.Is(err, notify.ErrChannelInvalid):
		return BadRequest(err.Error())
	}
	return err
}

// toChannelResponse maps one service channel. The secret is metadata only:
// whether it is set and its last four characters.
func toChannelResponse(ch notify.Channel) NotificationChannel {
	kinds := make([]AlertKind, 0, len(ch.Kinds))
	for _, kind := range ch.Kinds {
		kinds = append(kinds, AlertKind(kind))
	}
	config := ch.Config
	if config == nil {
		config = map[string]string{}
	}
	secret := SecretInfo{Set: ch.SecretSet}
	if ch.SecretSet && ch.Secret.Last4 != "" {
		last4 := ch.Secret.Last4
		secret.Last4 = &last4
	}
	return NotificationChannel{
		Id:          ch.ID,
		Kind:        ChannelKind(ch.Kind),
		Name:        ch.Name,
		Config:      config,
		Secret:      secret,
		MinSeverity: Severity(ch.MinSeverity),
		Kinds:       kinds,
		Enabled:     ch.Enabled,
		LastSentAt:  ch.LastSentAt,
		LastError:   ch.LastError,
		CreatedAt:   ch.CreatedAt,
	}
}

// ListNotificationChannels returns every channel of the org.
func (s *Server) ListNotificationChannels(ctx context.Context, _ ListNotificationChannelsRequestObject) (ListNotificationChannelsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := s.Channels.List(ctx, p)
	if err != nil {
		return nil, mapChannelError(err)
	}
	out := make([]NotificationChannel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, toChannelResponse(ch))
	}
	return ListNotificationChannels200JSONResponse(out), nil
}

// CreateNotificationChannel adds an alert channel.
func (s *Server) CreateNotificationChannel(ctx context.Context, req CreateNotificationChannelRequestObject) (CreateNotificationChannelResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}

	in := notify.ChannelCreate{
		Kind:    string(req.Body.Kind),
		Name:    req.Body.Name,
		Secret:  req.Body.Secret,
		Enabled: req.Body.Enabled,
	}
	if req.Body.Config != nil {
		in.Config = *req.Body.Config
	}
	if req.Body.MinSeverity != nil {
		in.MinSeverity = string(*req.Body.MinSeverity)
	}
	if req.Body.Kinds != nil {
		for _, kind := range *req.Body.Kinds {
			in.Kinds = append(in.Kinds, string(kind))
		}
	}

	ch, err := s.Channels.Create(ctx, p, in)
	if err != nil {
		return nil, mapChannelError(err)
	}
	return CreateNotificationChannel201JSONResponse(toChannelResponse(ch)), nil
}

// UpdateNotificationChannel patches one channel. A present secret replaces
// the stored one.
func (s *Server) UpdateNotificationChannel(ctx context.Context, req UpdateNotificationChannelRequestObject) (UpdateNotificationChannelResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}

	in := notify.ChannelUpdate{
		Name:    req.Body.Name,
		Secret:  req.Body.Secret,
		Enabled: req.Body.Enabled,
	}
	if req.Body.Config != nil {
		in.Config = req.Body.Config
	}
	if req.Body.MinSeverity != nil {
		severity := string(*req.Body.MinSeverity)
		in.MinSeverity = &severity
	}
	if req.Body.Kinds != nil {
		kinds := make([]string, 0, len(*req.Body.Kinds))
		for _, kind := range *req.Body.Kinds {
			kinds = append(kinds, string(kind))
		}
		in.Kinds = &kinds
	}

	ch, err := s.Channels.Update(ctx, p, req.ChannelId, in)
	if err != nil {
		return nil, mapChannelError(err)
	}
	return UpdateNotificationChannel200JSONResponse(toChannelResponse(ch)), nil
}

// DeleteNotificationChannel removes one channel.
func (s *Server) DeleteNotificationChannel(ctx context.Context, req DeleteNotificationChannelRequestObject) (DeleteNotificationChannelResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Channels.Delete(ctx, p, req.ChannelId); err != nil {
		return nil, mapChannelError(err)
	}
	return DeleteNotificationChannel204Response{}, nil
}

// TestNotificationChannel sends a fixed message synchronously. A provider
// refusal is a 422 carrying the provider's status and a short body excerpt.
func (s *Server) TestNotificationChannel(ctx context.Context, req TestNotificationChannelRequestObject) (TestNotificationChannelResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Channels.Test(ctx, p, req.ChannelId); err != nil {
		if errors.Is(err, notify.ErrChannelSend) {
			return nil, &APIError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "channel_failed",
				Message: err.Error(),
			}
		}
		return nil, mapChannelError(err)
	}
	return TestNotificationChannel204Response{}, nil
}
