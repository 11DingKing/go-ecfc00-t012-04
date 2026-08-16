package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for business-rule violations and state transitions.
var (
	ErrShiftNotFound        = errors.New("shift not found")
	ErrEquipmentNotFound    = errors.New("equipment not found")
	ErrClaimNotFound        = errors.New("claim not found")
	ErrAlertNotFound        = errors.New("alert not found")
	ErrCheckInNotFound      = errors.New("check-in not found")
	ErrRouteNotFound        = errors.New("route not found")
	ErrArchiveNotFound      = errors.New("archive not found")
	ErrInvalidTransition    = errors.New("invalid state transition")
	ErrEquipmentUnavailable = errors.New("equipment unavailable")
	ErrLockWindowViolation  = errors.New("equipment must be locked at least 2 hours before departure")
	ErrOutstandingEquipment = errors.New("officer has outstanding unreturned equipment of the same type")
	ErrWeatherProhibited    = errors.New("weather conditions prohibit drone dispatch")
	ErrCrossStationApproval = errors.New("cross-station borrowing requires dispatcher approval")
	ErrArchiveAccessDenied  = errors.New("archive access denied: only chief and dispatcher may read")
	ErrRoleForbidden        = errors.New("role not permitted for this action")
	ErrShiftNotApproved     = errors.New("shift must be approved before claiming equipment")
	ErrDuplicateIdempotency = errors.New("duplicate record: idempotency key already processed")
	ErrValidation           = errors.New("validation error")
	ErrClaimAlreadyTerminal = errors.New("claim is already in a terminal state")
)

// Role identifies an actor in the system.
type Role string

const (
	RoleChief      Role = "chief"
	RoleOfficer    Role = "officer"
	RoleKeeper     Role = "keeper"
	RoleDispatcher Role = "dispatcher"
)

// Actor represents the authenticated user making a request.
type Actor struct {
	ID        string
	Role      Role
	StationID string
}

// Clock abstracts time so time-based rules are testable.
type Clock interface {
	Now() time.Time
}

// RealClock returns wall-clock time.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// FixedClock returns a fixed time, useful for tests.
type FixedClock struct {
	T time.Time
}

func (c FixedClock) Now() time.Time { return c.T }

// Advance returns a new FixedClock advanced by d.
func (c FixedClock) Advance(d time.Duration) FixedClock {
	return FixedClock{T: c.T.Add(d)}
}

// NewID generates a short random identifier with the given prefix.
func NewID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// LockLeadTime is the minimum gap between locking equipment and departure.
const LockLeadTime = 2 * time.Hour

// EscalationDeadline is how long an alert may sit unsigned before escalation.
const EscalationDeadline = 5 * time.Minute
