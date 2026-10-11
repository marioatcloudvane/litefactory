package github

import (
	"context"
	"errors"
)

// sanitizeFailure strips every registered connection token before an error can
// reach daemon logs, without dropping the taxonomy that recovery depends on.
func (c *Client) sanitizeFailure(ctx context.Context, err error) error {
	if err == nil || c.redactor == nil {
		return err
	}
	safe := func(text string) string {
		body, redactionErr := c.redactor.Apply(ctx, text, 8192)
		if redactionErr != nil {
			return "remote diagnostic unavailable"
		}
		return body.String()
	}
	var api *APIError
	if errors.As(err, &api) {
		api.Method = safe(api.Method)
		api.URL = safe(api.URL)
		api.Message = safe(api.Message)
		api.DocumentationURL = safe(api.DocumentationURL)
		api.RequestID = safe(api.RequestID)
		var limited *RateLimitError
		if errors.As(err, &limited) {
			limited.Resource = safe(limited.Resource)
		}
		var invalid *ValidationError
		if errors.As(err, &invalid) {
			for i := range invalid.Errors {
				detail := &invalid.Errors[i]
				detail.Resource = safe(detail.Resource)
				detail.Field = safe(detail.Field)
				detail.Code = safe(detail.Code)
				detail.Message = safe(detail.Message)
			}
		}
		return err
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New(safe(err.Error()))
}
