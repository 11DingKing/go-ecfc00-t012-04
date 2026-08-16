package app

import (
	"fmt"

	"patrol-platform/internal/domain"
)

// ArchiveStore is the persistence interface consumed by ArchiveService.
type ArchiveStore interface {
	SaveArchive(domain.Archive)
	GetArchive(shiftID string) (domain.Archive, bool)
}

// ArchiveService handles infrared-camera data archival and access control.
// Rule 4: infrared camera data is archived per shift and only the station
// chief and the dispatcher may read it.
type ArchiveService struct {
	store ArchiveStore
	clock domain.Clock
}

// NewArchiveService constructs an ArchiveService.
func NewArchiveService(s ArchiveStore, clock domain.Clock) *ArchiveService {
	return &ArchiveService{store: s, clock: clock}
}

// ArchiveRequest is the input for archiving camera data at end of shift.
type ArchiveRequest struct {
	ShiftID    string
	CameraData []byte
	ArchivedBy string
	Notes      string
}

// Archive stores infrared-camera data for a shift. The data is captured
// during the shift and archived before the shift completes.
func (svc *ArchiveService) Archive(req ArchiveRequest) (domain.Archive, error) {
	if req.ShiftID == "" {
		return domain.Archive{}, fmt.Errorf("%w: shift_id required", domain.ErrValidation)
	}
	if req.ArchivedBy == "" {
		return domain.Archive{}, fmt.Errorf("%w: archived_by required", domain.ErrValidation)
	}
	a := domain.Archive{
		ShiftID:    req.ShiftID,
		CameraData: req.CameraData,
		ArchivedBy: req.ArchivedBy,
		ArchivedAt: svc.clock.Now(),
		Notes:      req.Notes,
	}
	svc.store.SaveArchive(a)
	return a, nil
}

// GetArchive returns archived data for a shift. Only the chief and the
// dispatcher are permitted to read infrared-camera archives.
func (svc *ArchiveService) GetArchive(shiftID string, actor domain.Actor) (domain.Archive, error) {
	if actor.Role != domain.RoleChief && actor.Role != domain.RoleDispatcher {
		return domain.Archive{}, domain.ErrArchiveAccessDenied
	}
	a, ok := svc.store.GetArchive(shiftID)
	if !ok {
		return domain.Archive{}, domain.ErrArchiveNotFound
	}
	return a, nil
}
