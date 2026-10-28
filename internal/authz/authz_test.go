package authz

import (
	"fmt"
	"testing"
)

// This table is the executable form of the "Roles and permissions" matrix in
// the spec. Every role × ownership × action combination is listed explicitly;
// the test also fails if a combination is missing, so new actions must be
// added here deliberately.

type ownership string

const (
	own   ownership = "own"
	other ownership = "other"
)

type action string

const (
	aView             action = "view"
	aSubscribeFeed    action = "subscribe-feed"
	aComment          action = "comment"
	aEditComment      action = "edit-comment"
	aDeleteComment    action = "delete-comment"
	aAttachComment    action = "attach-to-comment"
	aCreateEvent      action = "create-event"
	aEditEvent        action = "edit-event"
	aDeleteEvent      action = "delete-event"
	aDecideEdit       action = "decide-edit" // expects "allowed"/"suggest"/"forbidden"
	aManageAttach     action = "manage-event-attachments"
	aSuggest          action = "suggest"
	aReview           action = "review-suggestion"
	aWithdraw         action = "withdraw-suggestion"
	aDeleteAttachment action = "delete-attachment"
	aSeePending       action = "see-pending-attachment"
	aSeeInfected      action = "see-infected-attachment"
	aAdminister       action = "administer"
	aTransfer         action = "transfer-ownership"
	aActivity         action = "activity-scope" // expects "own"/"all"/"none"
	aRestoreUndo      action = "restore-within-undo"
	aRestoreLate      action = "restore-after-undo"
	aAttachComments   action = "attach-files-to-comments"
	aEraseUser        action = "erase-user" // own = erasing oneself
)

var allActions = []action{
	aView, aSubscribeFeed, aComment, aEditComment, aDeleteComment, aAttachComment,
	aCreateEvent, aEditEvent, aDeleteEvent, aDecideEdit, aManageAttach, aSuggest,
	aReview, aWithdraw, aDeleteAttachment, aSeePending, aSeeInfected, aAdminister,
	aTransfer, aActivity, aRestoreUndo, aRestoreLate, aAttachComments, aEraseUser,
}

type subject string

const (
	sViewer   subject = "viewer"
	sWriter   subject = "writer"
	sAdmin    subject = "admin"
	sInactive subject = "inactive-admin"
	sAnon     subject = "anonymous"
)

var allSubjects = []subject{sViewer, sWriter, sAdmin, sInactive, sAnon}

func actorFor(s subject) Actor {
	switch s {
	case sViewer:
		return Actor{ID: "me", Role: Viewer, Active: true}
	case sWriter:
		return Actor{ID: "me", Role: Writer, Active: true}
	case sAdmin:
		return Actor{ID: "me", Role: Admin, Active: true}
	case sInactive:
		return Actor{ID: "me", Role: Admin, Active: false}
	}
	return Actor{}
}

// Expected outcomes: key is subject/ownership/action.
type key struct {
	s subject
	o ownership
	a action
}

func expectations() map[key]string {
	m := map[key]string{}
	set := func(s subject, a action, ownV, otherV string) {
		m[key{s, own, a}] = ownV
		m[key{s, other, a}] = otherV
	}
	const T, F = "true", "false"

	// Viewer
	set(sViewer, aView, T, T)
	set(sViewer, aSubscribeFeed, T, T)
	set(sViewer, aComment, T, T)
	set(sViewer, aEditComment, T, F)
	set(sViewer, aDeleteComment, T, F)
	set(sViewer, aAttachComment, F, F)
	set(sViewer, aCreateEvent, F, F)
	set(sViewer, aEditEvent, F, F)
	set(sViewer, aDeleteEvent, F, F)
	set(sViewer, aDecideEdit, "forbidden", "forbidden")
	set(sViewer, aManageAttach, F, F)
	set(sViewer, aSuggest, F, F)
	set(sViewer, aReview, F, F)
	set(sViewer, aWithdraw, T, F) // a demoted writer can still withdraw their own
	set(sViewer, aDeleteAttachment, F, F)
	set(sViewer, aSeePending, T, F)
	set(sViewer, aSeeInfected, F, F)
	set(sViewer, aAdminister, F, F)
	set(sViewer, aTransfer, F, F)
	set(sViewer, aActivity, "own", "own")
	set(sViewer, aRestoreUndo, F, F)
	set(sViewer, aRestoreLate, F, F)
	set(sViewer, aAttachComments, F, F)
	set(sViewer, aEraseUser, F, F)

	// Writer
	set(sWriter, aView, T, T)
	set(sWriter, aSubscribeFeed, T, T)
	set(sWriter, aComment, T, T)
	set(sWriter, aEditComment, T, F)
	set(sWriter, aDeleteComment, T, F)
	set(sWriter, aAttachComment, T, F)
	set(sWriter, aCreateEvent, T, T)
	set(sWriter, aEditEvent, T, F)
	set(sWriter, aDeleteEvent, T, F)
	set(sWriter, aDecideEdit, "allowed", "suggest")
	set(sWriter, aManageAttach, T, F)
	set(sWriter, aSuggest, F, T)
	set(sWriter, aReview, T, F)
	set(sWriter, aWithdraw, T, F)
	set(sWriter, aDeleteAttachment, T, F)
	set(sWriter, aSeePending, T, F)
	set(sWriter, aSeeInfected, F, F)
	set(sWriter, aAdminister, F, F)
	set(sWriter, aTransfer, F, F)
	set(sWriter, aActivity, "own", "own")
	set(sWriter, aRestoreUndo, T, F)
	set(sWriter, aRestoreLate, F, F)
	set(sWriter, aAttachComments, T, T)
	set(sWriter, aEraseUser, F, F)

	// Admin
	set(sAdmin, aView, T, T)
	set(sAdmin, aSubscribeFeed, T, T)
	set(sAdmin, aComment, T, T)
	set(sAdmin, aEditComment, T, F)
	set(sAdmin, aDeleteComment, T, T)
	set(sAdmin, aAttachComment, T, F)
	set(sAdmin, aCreateEvent, T, T)
	set(sAdmin, aEditEvent, T, T)
	set(sAdmin, aDeleteEvent, T, T)
	set(sAdmin, aDecideEdit, "allowed", "allowed")
	set(sAdmin, aManageAttach, T, T)
	set(sAdmin, aSuggest, F, T)
	set(sAdmin, aReview, T, T)
	set(sAdmin, aWithdraw, T, F)
	set(sAdmin, aDeleteAttachment, T, T)
	set(sAdmin, aSeePending, T, F)
	set(sAdmin, aSeeInfected, F, F)
	set(sAdmin, aAdminister, T, T)
	set(sAdmin, aTransfer, T, T)
	set(sAdmin, aActivity, "all", "all")
	set(sAdmin, aRestoreUndo, T, T)
	set(sAdmin, aRestoreLate, T, T)
	set(sAdmin, aAttachComments, T, T)
	set(sAdmin, aEraseUser, F, T)

	// Inactive users and anonymous callers can do nothing at all.
	for _, s := range []subject{sInactive, sAnon} {
		for _, a := range allActions {
			v := F
			switch a {
			case aDecideEdit:
				v = "forbidden"
			case aActivity:
				v = "none"
			}
			set(s, a, v, v)
		}
	}
	return m
}

func evaluate(a Actor, o ownership, act action) string {
	ownerID := "someone-else"
	if o == own {
		ownerID = "me"
	}
	ev := Event{OwnerID: ownerID}
	cm := Comment{AuthorID: ownerID}
	// For attachments, "own" means the actor uploaded it to their own event;
	// "other" means someone else's file on someone else's event.
	at := Attachment{UploaderID: ownerID, EventOwnerID: ownerID}
	// For suggestions, "own" means the actor is the event owner and also the
	// author; review and withdraw are checked against the matching field.
	sg := Suggestion{AuthorID: ownerID, EventOwnerID: ownerID, Open: true}
	b := func(v bool) string { return fmt.Sprint(v) }

	switch act {
	case aView:
		return b(CanView(a))
	case aSubscribeFeed:
		return b(CanSubscribeFeed(a))
	case aComment:
		return b(CanComment(a, ev))
	case aEditComment:
		return b(CanEditComment(a, cm))
	case aDeleteComment:
		return b(CanDeleteComment(a, cm))
	case aAttachComment:
		return b(CanAttachToComment(a, cm))
	case aCreateEvent:
		return b(CanCreateEvent(a))
	case aEditEvent:
		return b(CanEditEvent(a, ev))
	case aDeleteEvent:
		return b(CanDeleteEvent(a, ev))
	case aDecideEdit:
		return DecideEdit(a, ev).String()
	case aManageAttach:
		return b(CanManageEventAttachments(a, ev))
	case aSuggest:
		return b(CanSuggest(a, ev))
	case aReview:
		return b(CanReviewSuggestion(a, Suggestion{AuthorID: "x", EventOwnerID: ownerID, Open: true}))
	case aWithdraw:
		return b(CanWithdrawSuggestion(a, sg))
	case aDeleteAttachment:
		return b(CanDeleteAttachment(a, at))
	case aSeePending:
		p := at
		p.Pending = true
		return b(CanSeeAttachment(a, p))
	case aSeeInfected:
		p := at
		p.Infected = true
		return b(CanSeeAttachment(a, p))
	case aAdminister:
		return b(CanAdminister(a))
	case aTransfer:
		return b(CanTransferOwnership(a))
	case aActivity:
		switch ActivityAccess(a) {
		case ActivityAll:
			return "all"
		case ActivityOwn:
			return "own"
		}
		return "none"
	case aRestoreUndo:
		e := ev
		e.Deleted = true
		return b(CanRestoreEvent(a, e, true))
	case aRestoreLate:
		e := ev
		e.Deleted = true
		return b(CanRestoreEvent(a, e, false))
	case aAttachComments:
		return b(CanAttachFilesToComments(a))
	case aEraseUser:
		return b(CanEraseUser(a, ownerID))
	}
	panic("unhandled action " + act)
}

func TestPermissionMatrix(t *testing.T) {
	exp := expectations()
	for _, s := range allSubjects {
		for _, o := range []ownership{own, other} {
			for _, a := range allActions {
				k := key{s, o, a}
				want, ok := exp[k]
				if !ok {
					t.Fatalf("missing expectation for %s/%s/%s", s, o, a)
				}
				t.Run(fmt.Sprintf("%s/%s/%s", s, o, a), func(t *testing.T) {
					if got := evaluate(actorFor(s), o, a); got != want {
						t.Errorf("got %s, want %s", got, want)
					}
				})
			}
		}
	}
	if want := len(allSubjects) * 2 * len(allActions); len(exp) != want {
		t.Fatalf("expectation table has %d entries, want %d", len(exp), want)
	}
}

func TestDeletedResourcesAreImmutable(t *testing.T) {
	admin := actorFor(sAdmin)
	ev := Event{OwnerID: "me", Deleted: true}
	if CanEditEvent(admin, ev) || CanSuggest(admin, Event{OwnerID: "x", Deleted: true}) || CanComment(admin, ev) {
		t.Fatal("deleted events must not be editable, suggestible or commentable")
	}
	if CanEditComment(admin, Comment{AuthorID: "me", Deleted: true}) || CanDeleteComment(admin, Comment{AuthorID: "me", Deleted: true}) {
		t.Fatal("deleted comments are immutable")
	}
	if CanSeeAttachment(admin, Attachment{Deleted: true}) || CanDeleteAttachment(admin, Attachment{Deleted: true}) {
		t.Fatal("deleted attachments are invisible")
	}
	if CanRestoreEvent(admin, Event{OwnerID: "me"}, true) {
		t.Fatal("cannot restore a live event")
	}
}

func TestClosedSuggestions(t *testing.T) {
	for _, s := range allSubjects {
		a := actorFor(s)
		closed := Suggestion{AuthorID: "me", EventOwnerID: "me", Open: false}
		if CanReviewSuggestion(a, closed) || CanWithdrawSuggestion(a, closed) {
			t.Fatalf("%s can act on a closed suggestion", s)
		}
	}
}

func TestEmptyOwnerNeverMatches(t *testing.T) {
	// An actor with an empty ID must never be treated as owner of an
	// ownerless resource.
	a := Actor{ID: "", Role: Writer, Active: true}
	if CanEditEvent(a, Event{OwnerID: ""}) {
		t.Fatal("empty IDs must not match")
	}
}

func TestEventPermissionsObject(t *testing.T) {
	w := actorFor(sWriter)
	p := EventPermissions(w, Event{OwnerID: "someone-else"})
	want := Permissions{Edit: false, Delete: false, Suggest: true, Comment: true, Attach: false, Review: false}
	if p != want {
		t.Fatalf("got %+v want %+v", p, want)
	}
	v := actorFor(sViewer)
	if p := EventPermissions(v, Event{OwnerID: "me"}); p != (Permissions{Comment: true}) {
		t.Fatalf("viewer: %+v", p)
	}
}

func TestRoleRank(t *testing.T) {
	if !(Admin.Rank() > Writer.Rank() && Writer.Rank() > Viewer.Rank() && Viewer.Rank() > Role("x").Rank()) {
		t.Fatal("role ranks out of order")
	}
}

// String names a decision in test expectations and failure messages.
func (d EditDecision) String() string {
	switch d {
	case Allowed:
		return "allowed"
	case SuggestOnly:
		return "suggest"
	}
	return "forbidden"
}
