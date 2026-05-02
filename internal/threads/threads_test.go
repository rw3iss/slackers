package threads

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rw3iss/slackers/internal/types"
)

// fixtures ----

func msg(uid, text string, replies ...types.Message) types.Message {
	return types.Message{
		MessageID: "m-" + uid,
		UserID:    uid,
		Text:      text,
		Replies:   replies,
	}
}

// detector ----

func TestDetect_RequiresReplies(t *testing.T) {
	// Parent with no replies must NOT be classified as a thread,
	// even if the local user authored it or is mentioned in it.
	parent := msg("U1", "hello")
	if _, ok := Detect(parent, "U1", true); ok {
		t.Fatalf("author with no replies should not be a thread")
	}
	mentioned := msg("U2", "hey <@U1> check this")
	if _, ok := Detect(mentioned, "U1", true); ok {
		t.Fatalf("mention with no replies should not be a thread")
	}
}

func TestDetect_AuthorTakesPriority(t *testing.T) {
	parent := msg("U1", "hello", msg("U2", "first reply"))
	reason, ok := Detect(parent, "U1", true)
	if !ok || reason != ReasonAuthor {
		t.Fatalf("want author/true, got %s/%v", reason, ok)
	}
}

func TestDetect_Mentioned(t *testing.T) {
	parent := msg("U2", "hey <@U1> can you check this", msg("U2", "ping"))
	reason, ok := Detect(parent, "U1", true)
	if !ok || reason != ReasonMentioned {
		t.Fatalf("want mentioned/true, got %s/%v", reason, ok)
	}
}

func TestDetect_Replied(t *testing.T) {
	parent := msg("U2", "no mention",
		msg("U3", "first reply"),
		msg("U1", "i replied"),
	)
	reason, ok := Detect(parent, "U1", true)
	if !ok || reason != ReasonReplied {
		t.Fatalf("want replied/true, got %s/%v", reason, ok)
	}
}

func TestDetect_ReplyMentionForwardOnly(t *testing.T) {
	parent := msg("U2", "no parent mention",
		msg("U3", "ping <@U1> here"),
	)
	if reason, ok := Detect(parent, "U1", true); !ok || reason != ReasonReplyMention {
		t.Fatalf("forward: want reply_mention/true, got %s/%v", reason, ok)
	}
	if _, ok := Detect(parent, "U1", false); ok {
		t.Fatalf("backfill: rule 4 should be skipped")
	}
}

func TestDetect_NoMatch(t *testing.T) {
	parent := msg("U2", "irrelevant",
		msg("U3", "no one tags U1"),
	)
	if _, ok := Detect(parent, "U1", true); ok {
		t.Fatalf("want no match")
	}
}

func TestDetect_EmptyMe(t *testing.T) {
	parent := msg("U1", "anything")
	if _, ok := Detect(parent, "", true); ok {
		t.Fatalf("empty me should not match")
	}
}

func TestDetect_FriendIDFormat(t *testing.T) {
	// Friend chats use slacker:<id> in UserID. Mention syntax is N/A
	// for friends, so only rules 2 and 3 should ever fire.
	me := "slacker:abc123"
	parent := msg(me, "i wrote this", msg("slacker:other", "thanks"))
	if reason, ok := Detect(parent, me, true); !ok || reason != ReasonAuthor {
		t.Fatalf("friend author: want author/true, got %s/%v", reason, ok)
	}

	parent2 := msg("slacker:other", "they wrote",
		msg(me, "i replied"),
	)
	if reason, ok := Detect(parent2, me, true); !ok || reason != ReasonReplied {
		t.Fatalf("friend reply: want replied/true, got %s/%v", reason, ok)
	}
}

// store ----

func snap(uid, ts string) ThreadSnapshot {
	return ThreadSnapshot{
		Ref: ThreadRef{
			Source:    SourceSlack,
			ChannelID: "C" + uid,
			ParentTS:  ts,
		},
		ChannelName: "channel-" + uid,
		ParentText:  "preview",
		// Two participants — the parent author plus a reply
		// participant — keeps the snapshot above the load-time
		// stale-entry filter (which drops Author/Mentioned snapshots
		// with ≤1 participant).
		Participants:   []string{"Alice", "Bob"},
		LastActivityTS: ts,
		Reason:         ReasonAuthor,
		AddedAt:        time.Now(),
	}
}

func TestStore_AddDedupAndUpdate(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	a := snap("1", "100.0")
	if !s.Add(a) {
		t.Fatalf("first add should be new")
	}
	// Same ref, refreshed metadata.
	a2 := a
	a2.ChannelName = "renamed"
	a2.LastActivityTS = "200.0"
	if s.Add(a2) {
		t.Fatalf("dup add should return false")
	}
	got, ok := s.Get(a.Ref)
	if !ok || got.ChannelName != "renamed" || got.LastActivityTS != "200.0" {
		t.Fatalf("update lost: %+v", got)
	}
}

func TestStore_Remove(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	a := snap("1", "100.0")
	s.Add(a)
	if !s.Remove(a.Ref) {
		t.Fatalf("remove should succeed")
	}
	if s.Count() != 0 {
		t.Fatalf("count should be 0 after remove")
	}
	if s.Remove(a.Ref) {
		t.Fatalf("second remove should fail")
	}
}

func TestStore_TouchSortsActiveNewestFirst(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	older := snap("1", "100.0")
	newer := snap("2", "200.0")
	s.Add(older)
	s.Add(newer)

	got := s.Active()
	if len(got) != 2 || got[0].Ref != newer.Ref {
		t.Fatalf("expected newer first, got: %+v", got)
	}

	// Touching the older one with a higher TS should bubble it up.
	s.Touch(older.Ref, "300.0")
	got = s.Active()
	if got[0].Ref != older.Ref {
		t.Fatalf("expected toucheded older to be first, got: %+v", got)
	}
}

func TestStore_LoadSaveRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.json")
	s := NewStore(path)
	s.Add(snap("1", "100.0"))
	s.Add(snap("2", "200.0"))
	s.SetLastClearedAt(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	s2 := NewStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if s2.Count() != 2 {
		t.Fatalf("want 2 items, got %d", s2.Count())
	}
	if !s2.LastClearedAt().Equal(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("last-cleared lost: %v", s2.LastClearedAt())
	}
}

func TestStore_ClearOlderThan(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	now := time.Now()
	old := ThreadSnapshot{
		Ref:            ThreadRef{Source: SourceSlack, ChannelID: "C1", ParentTS: "1000000000.0"}, // 2001
		LastActivityTS: "1000000000.0",
		AddedAt:        now.Add(-48 * time.Hour),
	}
	fresh := ThreadSnapshot{
		Ref:            ThreadRef{Source: SourceSlack, ChannelID: "C2", ParentTS: "9999999999.0"},
		LastActivityTS: "9999999999.0",
		AddedAt:        now,
	}
	s.Add(old)
	s.Add(fresh)

	cutoff := now.Add(-24 * time.Hour)
	removed := s.ClearOlderThan(cutoff)
	if removed != 1 {
		t.Fatalf("want 1 removed, got %d", removed)
	}
	if s.Count() != 1 {
		t.Fatalf("want 1 remaining, got %d", s.Count())
	}
}

// scheduler ----

func TestScheduler_DisabledSkipsSweep(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	old := ThreadSnapshot{
		Ref:            ThreadRef{Source: SourceSlack, ChannelID: "C1", ParentTS: "1000000000.0"},
		LastActivityTS: "1000000000.0",
	}
	s.Add(old)

	sch := NewScheduler(s, 0)
	sch.Start()
	defer sch.Stop()
	if s.Count() != 1 {
		t.Fatalf("disabled scheduler should not sweep, got count=%d", s.Count())
	}
}

func TestScheduler_StartSweepsWhenDue(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	old := ThreadSnapshot{
		Ref:            ThreadRef{Source: SourceSlack, ChannelID: "C1", ParentTS: "1000000000.0"}, // 2001
		LastActivityTS: "1000000000.0",
	}
	s.Add(old)
	// last_cleared is zero, so the catch-up sweep runs immediately.
	sch := NewScheduler(s, 24*time.Hour)
	sch.Start()
	defer sch.Stop()
	if s.Count() != 0 {
		t.Fatalf("expected sweep to clear stale entry, got count=%d", s.Count())
	}
}
