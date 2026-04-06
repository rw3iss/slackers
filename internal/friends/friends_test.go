package friends

import (
	"path/filepath"
	"testing"
)

func TestFriendStore_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "friends.json")
	store := NewFriendStore(path)

	f := Friend{UserID: "U123", Name: "Alice", PublicKey: "abc123"}
	if err := store.Add(f); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := store.Get("U123")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want Alice", got.Name)
	}
	if got.AddedAt == 0 {
		t.Error("AddedAt should be set automatically")
	}
}

func TestFriendStore_DuplicateAdd(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	err := store.Add(Friend{UserID: "U1", Name: "B"})
	if err == nil {
		t.Fatal("expected error on duplicate add")
	}
}

func TestFriendStore_Remove(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	store.Add(Friend{UserID: "U2", Name: "B"})
	store.Remove("U1")
	if store.Count() != 1 {
		t.Errorf("Count = %d, want 1", store.Count())
	}
	if store.Get("U1") != nil {
		t.Error("U1 should be removed")
	}
}

func TestFriendStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "friends.json")

	s1 := NewFriendStore(path)
	s1.Add(Friend{UserID: "U1", Name: "Alice", PublicKey: "key1"})
	s1.Add(Friend{UserID: "U2", Name: "Bob", PublicKey: "key2"})
	if err := s1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2 := NewFriendStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s2.Count() != 2 {
		t.Errorf("Count = %d, want 2", s2.Count())
	}
	if s2.Get("U1").Name != "Alice" {
		t.Error("Alice not found after reload")
	}
}

func TestFriendStore_LoadMissing(t *testing.T) {
	store := NewFriendStore("/nonexistent/path/friends.json")
	if err := store.Load(); err != nil {
		t.Fatalf("Load should not error on missing file: %v", err)
	}
	if store.Count() != 0 {
		t.Errorf("Count = %d, want 0", store.Count())
	}
}

func TestFriendStore_OnlineStatus(t *testing.T) {
	store := NewFriendStore("")
	store.Add(Friend{UserID: "U1", Name: "A"})
	store.SetOnline("U1", true)
	if !store.Get("U1").Online {
		t.Error("should be online")
	}
	store.SetOnline("U1", false)
	if store.Get("U1").Online {
		t.Error("should be offline")
	}
}
