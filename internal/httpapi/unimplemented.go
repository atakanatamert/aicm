// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import "context"

// Operations not implemented yet. Each answers 501 not_implemented. A task
// that implements one moves the method into its feature file.

func (s *Server) ExportReadings(context.Context, ExportReadingsRequestObject) (ExportReadingsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportUsage(context.Context, ExportUsageRequestObject) (ExportUsageResponseObject, error) {
	return nil, ErrNotImplemented
}
