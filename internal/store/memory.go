package store

import (
	"sort"
	"sync"
	"time"

	"patrol-platform/internal/domain"
)

// MemoryStore is a concurrency-safe, in-process persistence layer.
// All maps are guarded by a single RWMutex; write paths acquire a full
// lock so multi-step operations (e.g. equipment allocation with queue
// promotion) are atomic.
type MemoryStore struct {
	mu        sync.RWMutex
	shifts    map[string]domain.Shift
	equipment map[string]domain.Equipment
	claims    map[string]domain.EquipmentClaim
	alerts    map[string]domain.Alert
	checkIns  map[string]domain.CheckIn
	routes    map[string]domain.Route
	archives  map[string]domain.Archive

	// idempotency index: key -> record ID, per entity kind.
	idemCheckIn map[string]string
	idemAlert   map[string]string
}

// New creates an empty MemoryStore.
func New() *MemoryStore {
	return &MemoryStore{
		shifts:      make(map[string]domain.Shift),
		equipment:   make(map[string]domain.Equipment),
		claims:      make(map[string]domain.EquipmentClaim),
		alerts:      make(map[string]domain.Alert),
		checkIns:    make(map[string]domain.CheckIn),
		routes:      make(map[string]domain.Route),
		archives:    make(map[string]domain.Archive),
		idemCheckIn: make(map[string]string),
		idemAlert:   make(map[string]string),
	}
}

// ---------------------------------------------------------------------------
// Shifts
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveShift(sh domain.Shift) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shifts[sh.ID] = sh
}

func (s *MemoryStore) GetShift(id string) (domain.Shift, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shifts[id]
	return sh, ok
}

func (s *MemoryStore) ListShifts() []domain.Shift {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Shift, 0, len(s.shifts))
	for _, sh := range s.shifts {
		out = append(out, sh)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DepartureAt.Before(out[j].DepartureAt)
	})
	return out
}

func (s *MemoryStore) ListShiftsByWeek(weekStart time.Time) []domain.Shift {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Shift
	for _, sh := range s.shifts {
		if sh.WeekStart.Equal(weekStart) {
			out = append(out, sh)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DepartureAt.Before(out[j].DepartureAt)
	})
	return out
}

// ---------------------------------------------------------------------------
// Equipment
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveEquipment(e domain.Equipment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.equipment[e.ID] = e
}

func (s *MemoryStore) GetEquipment(id string) (domain.Equipment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.equipment[id]
	return e, ok
}

func (s *MemoryStore) ListEquipment() []domain.Equipment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Equipment, 0, len(s.equipment))
	for _, e := range s.equipment {
		out = append(out, e)
	}
	return out
}

// ---------------------------------------------------------------------------
// Claims — the concurrency boundary for equipment allocation
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveClaim(c domain.EquipmentClaim) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims[c.ID] = c
}

func (s *MemoryStore) GetClaim(id string) (domain.EquipmentClaim, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.claims[id]
	return c, ok
}

func (s *MemoryStore) ListClaimsByShift(shiftID string) []domain.EquipmentClaim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.EquipmentClaim
	for _, c := range s.claims {
		if c.ShiftID == shiftID {
			out = append(out, c)
		}
	}
	return out
}

// OutstandingBorrowedByOfficerAndType returns claims that are borrowed
// but not yet returned for the given officer and equipment type.
func (s *MemoryStore) OutstandingBorrowedByOfficerAndType(officerID string, et domain.EquipmentType) []domain.EquipmentClaim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.EquipmentClaim
	for _, c := range s.claims {
		if c.OfficerID == officerID && c.EquipmentType == et && c.Status == domain.ClaimStatusBorrowed {
			out = append(out, c)
		}
	}
	return out
}

// ActiveClaimForEquipment returns the locked or borrowed claim currently
// holding the equipment, or false if none.
func (s *MemoryStore) ActiveClaimForEquipment(equipmentID string) (domain.EquipmentClaim, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.claims {
		if c.EquipmentID == equipmentID && (c.Status == domain.ClaimStatusLocked || c.Status == domain.ClaimStatusBorrowed) {
			return c, true
		}
	}
	return domain.EquipmentClaim{}, false
}

// QueuedClaimsForEquipment returns all queued claims for the equipment,
// sorted by priority then submission time (winner first).
func (s *MemoryStore) QueuedClaimsForEquipment(equipmentID string) []domain.EquipmentClaim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.EquipmentClaim
	for _, c := range s.claims {
		if c.EquipmentID == equipmentID && c.Status == domain.ClaimStatusQueued {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Beats(out[j])
	})
	return out
}

// TryLockEquipment attempts to assign equipment to a claim atomically.
// If the equipment is available it is locked immediately. If it is locked
// (but not yet borrowed) the incoming claim may preempt the holder based
// on priority and submission time. If the equipment is borrowed the claim
// is queued. Returns (won=true, nil) when the claim now holds the lock.
func (s *MemoryStore) TryLockEquipment(claim domain.EquipmentClaim) (won bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	equip, ok := s.equipment[claim.EquipmentID]
	if !ok {
		return false, domain.ErrEquipmentNotFound
	}

	switch equip.Status {
	case domain.EquipmentStatusAvailable, domain.EquipmentStatusMaintenance:
		equip.Status = domain.EquipmentStatusLocked
		equip.LockedByShiftID = claim.ShiftID
		s.equipment[equip.ID] = equip
		claim.Status = domain.ClaimStatusLocked
		s.claims[claim.ID] = claim
		return true, nil

	case domain.EquipmentStatusLocked:
		// Find the current holding claim.
		var holder domain.EquipmentClaim
		holderFound := false
		for _, c := range s.claims {
			if c.EquipmentID == claim.EquipmentID && c.Status == domain.ClaimStatusLocked {
				holder = c
				holderFound = true
				break
			}
		}
		if !holderFound {
			// Stale lock with no claim — take it.
			equip.LockedByShiftID = claim.ShiftID
			s.equipment[equip.ID] = equip
			claim.Status = domain.ClaimStatusLocked
			s.claims[claim.ID] = claim
			return true, nil
		}
		if claim.Beats(holder) {
			// Preempt: demote holder to queued, new claim takes the lock.
			holder.Status = domain.ClaimStatusQueued
			s.claims[holder.ID] = holder
			equip.LockedByShiftID = claim.ShiftID
			s.equipment[equip.ID] = equip
			claim.Status = domain.ClaimStatusLocked
			s.claims[claim.ID] = claim
			return true, nil
		}
		// Loser goes to the standby queue.
		claim.Status = domain.ClaimStatusQueued
		s.claims[claim.ID] = claim
		return false, nil

	case domain.EquipmentStatusBorrowed:
		// Already handed out — cannot preempt, must queue.
		claim.Status = domain.ClaimStatusQueued
		s.claims[claim.ID] = claim
		return false, nil
	}

	return false, domain.ErrEquipmentUnavailable
}

// PromoteNextQueued promotes the highest-priority queued claim for the
// given equipment to locked, if the equipment is available. Returns the
// promoted claim and true when a promotion occurred.
func (s *MemoryStore) PromoteNextQueued(equipmentID string, now time.Time) (domain.EquipmentClaim, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	equip, ok := s.equipment[equipmentID]
	if !ok || equip.Status != domain.EquipmentStatusAvailable {
		return domain.EquipmentClaim{}, false
	}

	var best *domain.EquipmentClaim
	bestID := ""
	for id, c := range s.claims {
		if c.EquipmentID == equipmentID && c.Status == domain.ClaimStatusQueued {
			if best == nil || c.Beats(*best) {
				cc := c
				best = &cc
				bestID = id
			}
		}
	}
	if best == nil {
		return domain.EquipmentClaim{}, false
	}

	best.Status = domain.ClaimStatusLocked
	best.LockedAt = &now
	equip.Status = domain.EquipmentStatusLocked
	equip.LockedByShiftID = best.ShiftID
	s.equipment[equip.ID] = equip
	s.claims[bestID] = *best
	return *best, true
}

// ReleaseEquipment marks equipment as available and clears lock metadata.
func (s *MemoryStore) ReleaseEquipment(equipmentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.equipment[equipmentID]
	if !ok {
		return
	}
	e.Status = domain.EquipmentStatusAvailable
	e.LockedByShiftID = ""
	e.HeldBy = ""
	s.equipment[equipmentID] = e
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveAlert(a domain.Alert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts[a.ID] = a
	if a.IdempotencyKey != "" {
		s.idemAlert[a.IdempotencyKey] = a.ID
	}
}

func (s *MemoryStore) GetAlert(id string) (domain.Alert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.alerts[id]
	return a, ok
}

func (s *MemoryStore) AlertByidempotencyKey(key string) (domain.Alert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.idemAlert[key]
	if !ok {
		return domain.Alert{}, false
	}
	a, ok := s.alerts[id]
	return a, ok
}

func (s *MemoryStore) ListAlerts() []domain.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Alert, 0, len(s.alerts))
	for _, a := range s.alerts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReportedAt.Before(out[j].ReportedAt)
	})
	return out
}

func (s *MemoryStore) ListAlertsByStatus(status domain.AlertStatus) []domain.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Alert
	for _, a := range s.alerts {
		if a.Status == status {
			out = append(out, a)
		}
	}
	return out
}

func (s *MemoryStore) ListAlertsByShift(shiftID string) []domain.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Alert
	for _, a := range s.alerts {
		if a.ShiftID == shiftID {
			out = append(out, a)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Check-ins
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveCheckIn(c domain.CheckIn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkIns[c.ID] = c
	if c.IdempotencyKey != "" {
		s.idemCheckIn[c.IdempotencyKey] = c.ID
	}
}

func (s *MemoryStore) GetCheckIn(id string) (domain.CheckIn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.checkIns[id]
	return c, ok
}

func (s *MemoryStore) CheckInByIdempotencyKey(key string) (domain.CheckIn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.idemCheckIn[key]
	if !ok {
		return domain.CheckIn{}, false
	}
	c, ok := s.checkIns[id]
	return c, ok
}

func (s *MemoryStore) ListCheckInsByShift(shiftID string) []domain.CheckIn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.CheckIn
	for _, c := range s.checkIns {
		if c.ShiftID == shiftID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveRoute(r domain.Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[r.ID] = r
}

func (s *MemoryStore) GetRoute(id string) (domain.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[id]
	return r, ok
}

func (s *MemoryStore) ListRoutes() []domain.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------
// Archives
// ---------------------------------------------------------------------------

func (s *MemoryStore) SaveArchive(a domain.Archive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archives[a.ShiftID] = a
}

func (s *MemoryStore) GetArchive(shiftID string) (domain.Archive, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.archives[shiftID]
	return a, ok
}
