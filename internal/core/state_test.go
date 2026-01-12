// StateStore 与动作属性单元测试。
package core

import (
	"testing"
	"time"
)

func TestStateStore_TTL(t *testing.T) {
	t.Parallel()

	store := NewStateStore(20 * time.Millisecond)
	store.Set("u", ConversationState{
		Step:   StepAwaitingContainerName,
		Action: ActionRestart,
	})

	if _, ok := store.Get("u"); !ok {
		t.Fatalf("Get() ok = false, want true")
	}

	time.Sleep(25 * time.Millisecond)
	if _, ok := store.Get("u"); ok {
		t.Fatalf("Get() ok = true, want false")
	}
}

func TestAction_RequiresConfirm(t *testing.T) {
	t.Parallel()

	if !ActionRestart.RequiresConfirm() {
		t.Fatalf("ActionRestart RequiresConfirm() = false, want true")
	}
	if ActionViewStatus.RequiresConfirm() {
		t.Fatalf("ActionViewStatus RequiresConfirm() = true, want false")
	}
}
