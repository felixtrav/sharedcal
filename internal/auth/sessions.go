package auth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/store"
)

// touchInterval limits how often activity extends the idle window, to keep
// session reads from turning into writes.
const touchInterval = time.Hour

// Sessions manages server-side sessions. The cookie holds a random 256-bit
// token; the database holds only its SHA-256 hash.
type Sessions struct {
	DB       *store.DB
	IdleTTL  time.Duration
	MaxTTL   time.Duration
	Now      func() time.Time
	OnRevoke func(hashes []string) // closes live SSE streams
}

// ErrNoSession means the token is unknown, expired, or its user inactive.
var ErrNoSession = errors.New("no valid session")

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Create starts a session and returns the raw token for the cookie.
func (s *Sessions) Create(ctx context.Context, userID, ip, userAgent string) (string, *store.Session, error) {
	raw := randomToken(32)
	now := s.now()
	sess := &store.Session{
		ID:            HashToken(raw),
		UserID:        userID,
		ExpiresAt:     now.Add(s.MaxTTL),
		IdleExpiresAt: minTime(now.Add(s.IdleTTL), now.Add(s.MaxTTL)),
		LastSeenAt:    now,
		IP:            truncate(ip, 64),
		UserAgent:     truncate(userAgent, 512),
	}
	if err := s.DB.W.WithContext(ctx).Create(sess).Error; err != nil {
		return "", nil, err
	}
	return raw, sess, nil
}

// Lookup validates a raw token and returns the session with its user.
func (s *Sessions) Lookup(ctx context.Context, raw string) (*store.Session, *store.User, error) {
	if raw == "" || len(raw) > 128 {
		return nil, nil, ErrNoSession
	}
	hash := HashToken(raw)
	var sess store.Session
	err := s.DB.R.WithContext(ctx).Where("id = ?", hash).Take(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNoSession
	}
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) || !now.Before(sess.IdleExpiresAt) {
		_ = s.delete(ctx, []string{hash})
		return nil, nil, ErrNoSession
	}
	var user store.User
	if err := s.DB.R.WithContext(ctx).Where("id = ?", sess.UserID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNoSession
		}
		return nil, nil, err
	}
	if !user.Active {
		return nil, nil, ErrNoSession
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		idle := minTime(now.Add(s.IdleTTL), sess.ExpiresAt)
		if err := s.DB.W.WithContext(ctx).Model(&store.Session{}).Where("id = ?", hash).
			Updates(map[string]any{"last_seen_at": now, "idle_expires_at": idle}).Error; err == nil {
			sess.LastSeenAt, sess.IdleExpiresAt = now, idle
		}
	}
	return &sess, &user, nil
}

// Valid reports whether a session hash still refers to a live session of an
// active user. Used by long-lived SSE connections.
func (s *Sessions) Valid(ctx context.Context, hash string) bool {
	var n int64
	err := s.DB.R.WithContext(ctx).Table("sessions").
		Joins("JOIN users ON users.id = sessions.user_id").
		Where("sessions.id = ? AND sessions.expires_at > ? AND sessions.idle_expires_at > ? AND users.active = ?", hash, s.now(), s.now(), true).
		Count(&n).Error
	return err == nil && n == 1
}

// Revoke deletes one session by hash.
func (s *Sessions) Revoke(ctx context.Context, hash string) error {
	return s.delete(ctx, []string{hash})
}

// RevokeUser deletes every session of a user, optionally keeping one.
func (s *Sessions) RevokeUser(ctx context.Context, userID, keepHash string) error {
	var hashes []string
	q := s.DB.W.WithContext(ctx).Model(&store.Session{}).Where("user_id = ?", userID)
	if keepHash != "" {
		q = q.Where("id <> ?", keepHash)
	}
	if err := q.Pluck("id", &hashes).Error; err != nil {
		return err
	}
	return s.delete(ctx, hashes)
}

func (s *Sessions) delete(ctx context.Context, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	if err := s.DB.W.WithContext(ctx).Where("id IN ?", hashes).Delete(&store.Session{}).Error; err != nil {
		return err
	}
	if s.OnRevoke != nil {
		s.OnRevoke(hashes)
	}
	return nil
}

// PurgeExpired deletes sessions past either deadline.
func (s *Sessions) PurgeExpired(ctx context.Context) error {
	now := s.now()
	return s.DB.W.WithContext(ctx).Where("expires_at <= ? OR idle_expires_at <= ?", now, now).Delete(&store.Session{}).Error
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
