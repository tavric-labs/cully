package cully

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	advisorRequestLimit    = 6
	advisorRequestWindow   = time.Hour
	advisorRequestCooldown = 2 * time.Minute
)

type advisorBudget struct {
	Requests []time.Time `json:"requests"`
	Evidence string      `json:"evidence"`
}

func advisorBudgetFile(session string) string {
	return filepath.Join(cullyDir(), "advisor-budget-"+sessionKey(session)+".json")
}

// Bound automatic dispatches without a model call. The durable state contains
// timestamps and a hash only; it is not a token count or a billing guarantee.
// If accounting is unavailable, skip optional model work, not the user's task.
func admitAdvisorRequest(session, signals string, now time.Time) string {
	f, err := os.OpenFile(advisorBudgetFile(session), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "advisor request accounting unavailable"
	}
	defer f.Close()
	if lockFile(f) != nil {
		return "advisor request accounting unavailable"
	}
	defer unlockFile(f)
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return "advisor request accounting unavailable"
	}
	var budget advisorBudget
	if len(data) > 0 && json.Unmarshal(data, &budget) != nil {
		return "advisor request accounting unavailable"
	}
	var recent []time.Time
	for _, stamp := range budget.Requests {
		if now.Before(stamp) {
			return "advisor request cooldown: clock moved backwards"
		}
		if now.Sub(stamp) < advisorRequestWindow {
			recent = append(recent, stamp)
		}
	}
	sum := sha256.Sum256([]byte(signals))
	evidence := hex.EncodeToString(sum[:])
	// An idle live session can refresh public discovery once the hourly window
	// expires. Duplicate evidence inside that window still costs no model call.
	if evidence == budget.Evidence && (len(recent) > 0 || !discoveryEnabled(advisorAgent(signals))) {
		return "unchanged advisor evidence"
	}
	if len(recent) >= advisorRequestLimit {
		return "advisor request limit reached (6 per hour)"
	}
	if len(recent) > 0 && now.Sub(recent[len(recent)-1]) < advisorRequestCooldown {
		return "advisor request cooldown (2 minutes)"
	}
	budget.Requests, budget.Evidence = append(recent, now.UTC()), evidence
	encoded, err := json.Marshal(budget)
	if err != nil {
		return "advisor request accounting unavailable"
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "advisor request accounting unavailable"
	}
	if _, err = f.Write(encoded); err != nil {
		return "advisor request accounting unavailable"
	}
	if f.Truncate(int64(len(encoded))) != nil {
		return "advisor request accounting unavailable"
	}
	return ""
}
