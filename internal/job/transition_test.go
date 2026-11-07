package job

import "testing"

func TestCanTransition(t *testing.T) {
	allowed := [][2]Status{
		{StatusPending, StatusRunning},
		{StatusPending, StatusCancelled},
		{StatusRunning, StatusSucceeded},
		{StatusRunning, StatusPending},
		{StatusRunning, StatusDead},
		{StatusDead, StatusPending},
	}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("expected %s -> %s", pair[0], pair[1])
		}
	}

	rejected := [][2]Status{
		{StatusPending, StatusPending},
		{StatusPending, StatusSucceeded},
		{StatusPending, StatusDead},
		{StatusRunning, StatusCancelled},
		{StatusSucceeded, StatusPending},
		{StatusSucceeded, StatusRunning},
		{StatusCancelled, StatusPending},
		{StatusDead, StatusRunning},
		{StatusDead, StatusSucceeded},
		{StatusDead, StatusCancelled},
	}
	for _, pair := range rejected {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("rejected %s -> %s", pair[0], pair[1])
		}
	}
}
