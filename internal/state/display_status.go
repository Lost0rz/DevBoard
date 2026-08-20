package state

import "time"

type DisplayStatus string

type CompletionPhase string

const (
	DisplayWorking   DisplayStatus = "WORKING"
	DisplayAttention DisplayStatus = "ATTENTION"
	DisplayError     DisplayStatus = "ERROR"
	DisplayStale     DisplayStatus = "STALE"
	DisplayComplete  DisplayStatus = "COMPLETE"
	DisplayIdle      DisplayStatus = "IDLE"
)

const (
	CompletionNone   CompletionPhase = "none"
	CompletionHigh   CompletionPhase = "high"
	CompletionRecent CompletionPhase = "recent"
)

type DerivedDisplay struct {
	Status          DisplayStatus
	CompletionPhase CompletionPhase
	Priority        int
}

func DeriveDisplay(turn CurrentTurn, now time.Time, highVisibility, retention time.Duration) DerivedDisplay {
	switch turn.Activity {
	case ActivityAttention:
		return DerivedDisplay{Status: DisplayAttention, CompletionPhase: CompletionNone, Priority: 0}
	case ActivityError:
		return DerivedDisplay{Status: DisplayError, CompletionPhase: CompletionNone, Priority: 1}
	}
	if turn.Freshness == FreshnessStale && turn.Activity != ActivityIdle {
		return DerivedDisplay{Status: DisplayStale, CompletionPhase: CompletionNone, Priority: 2}
	}
	if turn.Activity == ActivityIdle && turn.Outcome == OutcomeCompleted && turn.CompletedAt != nil {
		age := now.Sub(*turn.CompletedAt)
		if age >= 0 && age < retention {
			phase := CompletionRecent
			if age < highVisibility {
				phase = CompletionHigh
			}
			return DerivedDisplay{Status: DisplayComplete, CompletionPhase: phase, Priority: 3}
		}
	}
	if turn.Activity == ActivityWorking {
		return DerivedDisplay{Status: DisplayWorking, CompletionPhase: CompletionNone, Priority: 4}
	}
	return DerivedDisplay{Status: DisplayIdle, CompletionPhase: CompletionNone, Priority: 5}
}
