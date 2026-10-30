package domain

import (
	"context"
	"errors"
	"io"
	"time"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// AttachmentDTO is an attachment as sent to clients.
type AttachmentDTO struct {
	ID           string    `json:"id"`
	EventID      string    `json:"eventId"`
	CommentID    *string   `json:"commentId"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"contentType"`
	Size         int64     `json:"size"`
	Width        *int      `json:"width"`
	Height       *int      `json:"height"`
	IsImage      bool      `json:"isImage"`
	URL          string    `json:"url"`
	ThumbURL     *string   `json:"thumbUrl"`
	ScanStatus   string    `json:"scanStatus" enum:"pending,clean,infected,skipped"`
	Uploader     UserRef   `json:"uploader"`
	EventOwnerID string    `json:"eventOwnerId"`
	CreatedAt    time.Time `json:"createdAt"`
	Permissions  struct {
		Delete bool `json:"delete"`
	} `json:"permissions"`
}

func authzAttachment(a *store.Attachment, eventOwnerID string) authz.Attachment {
	return authz.Attachment{
		UploaderID: a.UploaderID, EventOwnerID: eventOwnerID,
		Pending: a.ScanStatus == store.ScanPending, Infected: a.ScanStatus == store.ScanInfected,
		Deleted: a.DeletedAt != nil,
	}
}

func attachmentDTO(a *store.Attachment, ev *store.Event, uploader UserRef, actor authz.Actor) AttachmentDTO {
	d := AttachmentDTO{
		ID: a.ID, EventID: a.EventID, CommentID: a.CommentID, Filename: a.Filename, ContentType: a.ContentType,
		Size: a.SizeBytes, Width: a.Width, Height: a.Height, IsImage: a.ThumbKey != nil,
		URL: "/api/attachments/" + a.ID + "/content", ScanStatus: a.ScanStatus, Uploader: uploader,
		EventOwnerID: ev.OwnerID, CreatedAt: a.CreatedAt,
	}
	if a.ThumbKey != nil {
		d.ThumbURL = ptr("/api/attachments/" + a.ID + "/thumb")
	}
	if d.Uploader.ID == "" {
		d.Uploader.ID = a.UploaderID
	}
	d.Permissions.Delete = authz.CanDeleteAttachment(actor, authzAttachment(a, ev.OwnerID))
	return d
}

// AttachmentPermissions recomputes the per-user part of an attachment DTO
// (used to personalize SSE payloads).
func AttachmentPermissions(actor authz.Actor, d *AttachmentDTO) {
	a := store.Attachment{UploaderID: d.Uploader.ID, ScanStatus: d.ScanStatus}
	d.Permissions.Delete = authz.CanDeleteAttachment(actor, authzAttachment(&a, d.EventOwnerID))
}

// listAttachments returns the attachments of an event the actor may see.
func (s *Service) listAttachments(db *gorm.DB, actor authz.Actor, ev *store.Event, where string, args ...any) ([]AttachmentDTO, error) {
	var rows []store.Attachment
	q := db.Where("event_id = ? AND deleted_at IS NULL AND scan_status <> ?", ev.ID, store.ScanInfected)
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Order("created_at, id").Limit(1000).Find(&rows).Error; err != nil {
		return nil, err
	}
	var uploaders []string
	for _, r := range rows {
		uploaders = append(uploaders, r.UploaderID)
	}
	refs, err := userRefs(db, uploaders)
	if err != nil {
		return nil, err
	}
	out := []AttachmentDTO{}
	for i := range rows {
		if authz.CanSeeAttachment(actor, authzAttachment(&rows[i], ev.OwnerID)) {
			out = append(out, attachmentDTO(&rows[i], ev, refs[rows[i].UploaderID], actor))
		}
	}
	return out, nil
}

// Upload is an incoming file.
type Upload struct {
	Filename string
	Body     io.Reader
}

// UploadToEvent attaches a file to an event (owner or admin).
func (s *Service) UploadToEvent(ctx context.Context, actor authz.Actor, eventID string, up Upload) (*AttachmentDTO, error) {
	return s.upload(ctx, actor, eventID, nil, up)
}

// UploadToComment attaches a file to the caller's own comment (writers+).
func (s *Service) UploadToComment(ctx context.Context, actor authz.Actor, commentID string, up Upload) (*AttachmentDTO, error) {
	var c store.Comment
	if err := s.DB.R.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", commentID).Take(&c).Error; err != nil {
		if isNotFound(err) {
			return nil, notFound("comment")
		}
		return nil, err
	}
	return s.upload(ctx, actor, c.EventID, &c, up)
}

// checkUpload enforces permissions and count limits. It runs before the
// (expensive) pipeline and again inside the write transaction.
func (s *Service) checkUpload(db *gorm.DB, actor authz.Actor, eventID string, comment *store.Comment) (*store.Event, Settings, error) {
	ev, err := s.loadEvent(db, eventID, false)
	if err != nil {
		return nil, Settings{}, err
	}
	st, err := s.loadSettings(db)
	if err != nil {
		return nil, Settings{}, err
	}
	q := db.Model(&store.Attachment{}).Where("event_id = ? AND deleted_at IS NULL AND scan_status <> ?", eventID, store.ScanInfected)
	max := st.AttachmentMaxPerEvent
	if comment != nil {
		var c store.Comment
		if err := db.Where("id = ? AND deleted_at IS NULL", comment.ID).Take(&c).Error; err != nil {
			return nil, Settings{}, notFound("comment")
		}
		if !authz.CanAttachToComment(actor, authzComment(&c)) {
			return nil, Settings{}, forbidden("only writers can attach files, and only to their own comments")
		}
		q = q.Where("comment_id = ?", comment.ID)
		max = st.AttachmentMaxPerComment
	} else {
		if !authz.CanManageEventAttachments(actor, authzEvent(ev)) {
			return nil, Settings{}, forbidden("only the event owner or an admin can attach files to this event")
		}
		q = q.Where("comment_id IS NULL")
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return nil, Settings{}, err
	}
	if int(n) >= max {
		return nil, Settings{}, limit("attachment limit reached")
	}
	return ev, st, nil
}

func (s *Service) upload(ctx context.Context, actor authz.Actor, eventID string, comment *store.Comment, up Upload) (*AttachmentDTO, error) {
	if s.Attachments == nil {
		return nil, errors.New("attachments are not configured")
	}
	_, st, err := s.checkUpload(s.DB.R.WithContext(ctx), actor, eventID, comment)
	if err != nil {
		return nil, err
	}
	// Settings may lower the per-file limit below the server ceiling.
	body := up.Body
	if st.AttachmentMaxSize < s.Attachments.MaxSize {
		body = &limitedReader{r: body, n: st.AttachmentMaxSize}
	}
	res, err := s.Attachments.Process(ctx, up.Filename, body)
	if err != nil {
		return nil, err
	}
	var out *AttachmentDTO
	var row store.Attachment
	err = s.write(ctx, actor, func(tx *Tx) error {
		ev, st, err := s.checkUpload(tx.db, actor, eventID, comment)
		if err != nil {
			return err
		}
		used, err := storageUsed(tx.db)
		if err != nil {
			return err
		}
		if st.AttachmentQuota > 0 && used+res.Size > st.AttachmentQuota {
			return limit("the instance's attachment storage quota is full")
		}
		row = store.Attachment{
			ID: store.NewID(), EventID: eventID, UploaderID: actor.ID, Filename: res.Filename, ContentType: res.ContentType,
			SizeBytes: res.Size, SHA256: res.SHA256, StorageKey: res.StorageKey, Width: res.Width, Height: res.Height,
			ThumbKey: res.ThumbKey, ScanStatus: res.ScanStatus, CreatedAt: tx.now, UpdatedAt: tx.now,
		}
		if comment != nil {
			row.CommentID = &comment.ID
		}
		if err := tx.db.Create(&row).Error; err != nil {
			return err
		}
		refs, err := userRefs(tx.db, []string{actor.ID})
		if err != nil {
			return err
		}
		dto := attachmentDTO(&row, ev, refs[actor.ID], actor)
		out = &dto
		if err := tx.record("attachment.added", "attachment", row.ID, &eventID, nil, map[string]any{"filename": row.Filename, "size": row.SizeBytes, "contentType": row.ContentType, "commentId": row.CommentID}); err != nil {
			return err
		}
		if comment == nil {
			if err := s.notifyWatchers(tx, eventID, kindWatchUpdated); err != nil {
				return err
			}
		}
		audience := ""
		if row.ScanStatus == store.ScanPending {
			audience = actor.ID // pending files are visible only to their uploader
		}
		return tx.emit("attachment.added", row.ID, 0, dto, audience)
	})
	if err != nil {
		s.gcBlobs(context.WithoutCancel(ctx), []string{res.StorageKey, deref(res.ThumbKey)})
		return nil, err
	}
	if row.ScanStatus == store.ScanPending && s.Scanner != nil {
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			s.scan(context.WithoutCancel(ctx), row)
		}()
	}
	return out, nil
}

type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n < 0 {
		return 0, attachments.ErrTooLarge
	}
	if int64(len(p)) > l.n+1 {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n < 0 {
		return n, attachments.ErrTooLarge
	}
	return n, err
}

// scan runs the virus scan for a pending attachment and publishes the result.
func (s *Service) scan(ctx context.Context, a store.Attachment) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	verdict, err := s.Attachments.ScanStored(ctx, s.Scanner, a.StorageKey)
	if err != nil {
		// Stays pending (invisible to others); an admin can retry by re-uploading.
		s.Log.ErrorContext(ctx, "virus scan failed", "attachment", a.ID, "err", err)
		return
	}
	status := store.ScanClean
	if !verdict.Clean {
		status = store.ScanInfected
	}
	err = s.write(ctx, authz.Actor{}, func(tx *Tx) error {
		res := tx.db.Model(&store.Attachment{}).Where("id = ? AND scan_status = ?", a.ID, store.ScanPending).
			Updates(map[string]any{"scan_status": status, "updated_at": tx.now})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		a.ScanStatus = status
		ev, err := s.loadEvent(tx.db, a.EventID, true)
		if err != nil {
			return err
		}
		if status == store.ScanInfected {
			if err := tx.recordAs("", "attachment.infected", "attachment", a.ID, &a.EventID, nil, map[string]string{"filename": a.Filename, "signature": verdict.Signature, "uploader": a.UploaderID}); err != nil {
				return err
			}
			return tx.emit("attachment.removed", a.ID, 0, map[string]any{"id": a.ID, "eventId": a.EventID, "commentId": a.CommentID}, a.UploaderID)
		}
		refs, err := userRefs(tx.db, []string{a.UploaderID})
		if err != nil {
			return err
		}
		dto := attachmentDTO(&a, ev, refs[a.UploaderID], authz.Actor{})
		if err := tx.recordAs("", "attachment.scanned", "attachment", a.ID, &a.EventID, nil, map[string]string{"status": status}); err != nil {
			return err
		}
		return tx.emit("attachment.scanned", a.ID, 0, dto, "")
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "recording scan result failed", "attachment", a.ID, "err", err)
		return
	}
	if status == store.ScanInfected {
		s.Log.WarnContext(ctx, "infected upload deleted", "attachment", a.ID, "signature", verdict.Signature)
		s.gcBlobs(ctx, []string{a.StorageKey, deref(a.ThumbKey)})
	}
}

// AttachmentContent is an open attachment ready to serve.
type AttachmentContent struct {
	Body        io.ReadSeekCloser
	Size        int64
	ContentType string
	Filename    string
	Inline      bool
	ModTime     time.Time
}

// OpenAttachment authorizes and opens an attachment (or its thumbnail).
func (s *Service) OpenAttachment(ctx context.Context, actor authz.Actor, id string, thumb bool) (*AttachmentContent, error) {
	db := s.DB.R.WithContext(ctx)
	a, err := take[store.Attachment](db, id, "attachment")
	if err != nil {
		return nil, err
	}
	ev, err := s.loadEvent(db, a.EventID, false)
	if err != nil {
		return nil, notFound("attachment")
	}
	if !authz.CanSeeAttachment(actor, authzAttachment(&a, ev.OwnerID)) {
		return nil, notFound("attachment")
	}
	if a.CommentID != nil {
		var c store.Comment
		if err := db.Where("id = ? AND deleted_at IS NULL", *a.CommentID).Take(&c).Error; err != nil {
			return nil, notFound("attachment")
		}
	}
	key, ctype, inline := a.StorageKey, a.ContentType, a.ThumbKey != nil
	if thumb {
		if a.ThumbKey == nil {
			return nil, notFound("thumbnail")
		}
		key = *a.ThumbKey
	}
	rc, size, err := s.Blobs.Open(ctx, key)
	if err != nil {
		if errors.Is(err, attachments.ErrNotFound) {
			return nil, notFound("attachment")
		}
		return nil, err
	}
	if thumb {
		ctype = sniffImageType(rc)
	}
	return &AttachmentContent{Body: rc, Size: size, ContentType: ctype, Filename: a.Filename, Inline: inline, ModTime: a.CreatedAt}, nil
}

// DeleteAttachment soft-deletes an attachment (uploader, event owner, admin).
func (s *Service) DeleteAttachment(ctx context.Context, actor authz.Actor, id string) error {
	return s.write(ctx, actor, func(tx *Tx) error {
		var a store.Attachment
		if err := tx.db.Where("id = ? AND deleted_at IS NULL", id).Take(&a).Error; err != nil {
			if isNotFound(err) {
				return notFound("attachment")
			}
			return err
		}
		ev, err := s.loadEvent(tx.db, a.EventID, false)
		if err != nil {
			return err
		}
		if !authz.CanDeleteAttachment(actor, authzAttachment(&a, ev.OwnerID)) {
			return forbidden("you cannot remove this attachment")
		}
		if err := tx.db.Model(&store.Attachment{}).Where("id = ?", a.ID).Updates(map[string]any{"deleted_at": tx.now, "updated_at": tx.now}).Error; err != nil {
			return err
		}
		if err := tx.record("attachment.removed", "attachment", a.ID, &a.EventID, map[string]any{"filename": a.Filename, "commentId": a.CommentID}, nil); err != nil {
			return err
		}
		return tx.emit("attachment.removed", a.ID, 0, map[string]any{"id": a.ID, "eventId": a.EventID, "commentId": a.CommentID}, "")
	})
}

// RestoreAttachment undoes a removal within the retention window (admins).
func (s *Service) RestoreAttachment(ctx context.Context, actor authz.Actor, id string) (*AttachmentDTO, error) {
	if !authz.CanRestoreAttachment(actor) {
		return nil, forbidden("admins only")
	}
	var out *AttachmentDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		var a store.Attachment
		if err := tx.db.Where("id = ? AND deleted_at IS NOT NULL AND deleted_at > ?", id, tx.now.Add(-trashRetention)).Take(&a).Error; err != nil {
			if isNotFound(err) {
				return notFound("deleted attachment")
			}
			return err
		}
		ev, err := s.loadEvent(tx.db, a.EventID, false)
		if err != nil {
			return err
		}
		if a.CommentID != nil {
			if _, _, err := s.loadComment(tx.db, *a.CommentID); err != nil {
				if ErrorKind(err) == KindNotFound {
					return conflict("its comment has been deleted", nil)
				}
				return err
			}
		}
		a.DeletedAt, a.UpdatedAt = nil, tx.now
		if err := tx.db.Save(&a).Error; err != nil {
			return err
		}
		refs, err := userRefs(tx.db, []string{a.UploaderID})
		if err != nil {
			return err
		}
		dto := attachmentDTO(&a, ev, refs[a.UploaderID], actor)
		out = &dto
		if err := tx.record("attachment.restored", "attachment", a.ID, &a.EventID, nil, nil); err != nil {
			return err
		}
		return tx.emit("attachment.added", a.ID, 0, dto, "")
	})
	return out, err
}

// TrashedAttachment is a removed attachment that can still be restored.
type TrashedAttachment struct {
	AttachmentDTO
	EventTitle string    `json:"eventTitle"`
	DeletedAt  time.Time `json:"deletedAt"`
}

// AttachmentTrash lists removed attachments that are still restorable: within
// the retention window, on a live event, and not on a deleted comment.
func (s *Service) AttachmentTrash(ctx context.Context, actor authz.Actor) ([]TrashedAttachment, error) {
	if !authz.CanRestoreAttachment(actor) {
		return nil, forbidden("admins only")
	}
	db := s.DB.R.WithContext(ctx)
	var rows []store.Attachment
	err := db.Table("attachments AS a").Select("a.*").
		Joins("JOIN events e ON e.id = a.event_id AND e.deleted_at IS NULL").
		Joins("LEFT JOIN comments c ON c.id = a.comment_id").
		Where("a.deleted_at IS NOT NULL AND a.deleted_at > ? AND a.scan_status <> ? AND (a.comment_id IS NULL OR c.deleted_at IS NULL)", s.now().Add(-trashRetention), store.ScanInfected).
		Order("a.deleted_at DESC").Limit(1000).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	var eventIDs, uploaders []string
	for _, a := range rows {
		eventIDs = append(eventIDs, a.EventID)
		uploaders = append(uploaders, a.UploaderID)
	}
	events := map[string]*store.Event{}
	if len(eventIDs) > 0 {
		var evs []store.Event
		if err := db.Where("id IN ?", uniq(eventIDs)).Find(&evs).Error; err != nil {
			return nil, err
		}
		for i := range evs {
			events[evs[i].ID] = &evs[i]
		}
	}
	refs, err := userRefs(db, uploaders)
	if err != nil {
		return nil, err
	}
	out := make([]TrashedAttachment, 0, len(rows))
	for i := range rows {
		a, ev := &rows[i], events[rows[i].EventID]
		if ev == nil {
			continue
		}
		out = append(out, TrashedAttachment{AttachmentDTO: attachmentDTO(a, ev, refs[a.UploaderID], actor), EventTitle: ev.Title, DeletedAt: *a.DeletedAt})
	}
	return out, nil
}

// gcBlobs deletes blobs that no attachment row (or the logo) references.
func (s *Service) gcBlobs(ctx context.Context, keys []string) {
	if s.Blobs == nil {
		return
	}
	db := s.DB.R.WithContext(ctx)
	st, _ := s.loadSettings(db)
	for _, k := range uniq(keys) {
		if k == "" || k == st.LogoKey {
			continue
		}
		var n int64
		if err := db.Model(&store.Attachment{}).Where("storage_key = ? OR thumb_key = ?", k, k).Count(&n).Error; err != nil || n > 0 {
			continue
		}
		if err := s.Blobs.Delete(ctx, k); err != nil {
			s.Log.WarnContext(ctx, "deleting blob failed", "key", k, "err", err)
		}
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// SetLogo stores an uploaded logo (re-encoded like any image upload).
func (s *Service) SetLogo(ctx context.Context, actor authz.Actor, up Upload) (string, error) {
	if !authz.CanAdminister(actor) {
		return "", forbidden("admins only")
	}
	res, err := s.Attachments.Process(ctx, up.Filename, up.Body)
	if err != nil {
		return "", err
	}
	if !res.IsImage {
		s.gcBlobs(ctx, []string{res.StorageKey})
		return "", attachments.ErrTypeNotAllowed
	}
	var old string
	err = s.write(ctx, actor, func(tx *Tx) error {
		st, err := s.loadSettings(tx.db)
		if err != nil {
			return err
		}
		old = st.LogoKey
		st.LogoKey = res.StorageKey
		if err := s.saveSettings(tx, st); err != nil {
			return err
		}
		if err := tx.record("settings.logo_updated", "settings", settingsKey, nil, nil, nil); err != nil {
			return err
		}
		return tx.emit("settings.changed", settingsKey, 0, struct{}{}, "")
	})
	if err != nil {
		return "", err
	}
	if res.ThumbKey != nil {
		s.gcBlobs(ctx, []string{*res.ThumbKey})
	}
	if old != "" && old != res.StorageKey {
		s.gcBlobs(ctx, []string{old})
	}
	p, err := s.Public(ctx)
	return p.LogoURL, err
}

// RemoveLogo clears the branding logo.
func (s *Service) RemoveLogo(ctx context.Context, actor authz.Actor) error {
	if !authz.CanAdminister(actor) {
		return forbidden("admins only")
	}
	var old string
	err := s.write(ctx, actor, func(tx *Tx) error {
		st, err := s.loadSettings(tx.db)
		if err != nil {
			return err
		}
		old, st.LogoKey = st.LogoKey, ""
		if err := s.saveSettings(tx, st); err != nil {
			return err
		}
		if err := tx.record("settings.logo_removed", "settings", settingsKey, nil, nil, nil); err != nil {
			return err
		}
		return tx.emit("settings.changed", settingsKey, 0, struct{}{}, "")
	})
	if err == nil && old != "" {
		s.gcBlobs(ctx, []string{old})
	}
	return err
}

// OpenLogo opens the branding logo, which is public (shown on sign-in).
func (s *Service) OpenLogo(ctx context.Context) (*AttachmentContent, error) {
	st, err := s.loadSettings(s.DB.R.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if st.LogoKey == "" {
		return nil, notFound("logo")
	}
	rc, size, err := s.Blobs.Open(ctx, st.LogoKey)
	if err != nil {
		return nil, notFound("logo")
	}
	return &AttachmentContent{Body: rc, Size: size, ContentType: sniffImageType(rc), Filename: "logo", Inline: true}, nil
}

// sniffImageType identifies a stored, already re-encoded logo (PNG or JPEG).
func sniffImageType(r io.ReadSeeker) string {
	var head [8]byte
	n, _ := io.ReadFull(r, head[:])
	_, _ = r.Seek(0, io.SeekStart)
	if n >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	return "image/jpeg"
}
