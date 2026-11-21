package durable

import (
	"fmt"
	"maps"
)

func signalMemoName(signal, key string) string { return "pi.signal." + signal + "/" + key }

// Pick the first delivered and unused key in the order the task listed them.
func readySignal(state TaskState, memos map[string]*TaskValue) (SignalKey, bool) {
	for _, key := range state.Signals {
		if memo := memos[signalMemoName(key.Signal, key.Key)]; memo != nil {
			stored := generationObject(memo.Value)
			if stored != nil && stored["expired"] != true && stored["consumed"] != true {
				return key, true
			}
		}
	}
	return SignalKey{}, false
}

func checkSignalSleep(id ID, state TaskState, memos map[string]*TaskValue) error {
	if state.Until == nil && len(state.Signals) == 0 {
		return fmt.Errorf("Task %d cannot sleep without until or signals", id)
	}
	for _, key := range state.Signals {
		if memo := memos[signalMemoName(key.Signal, key.Key)]; memo != nil {
			stored := generationObject(memo.Value)
			if _, expired := stored["expired"]; expired {
				return fmt.Errorf("Task %d cannot sleep on signal %s key %s: it expired", id, key.Signal, key.Key)
			}
			if stored["consumed"] == true {
				return fmt.Errorf("Task %d cannot sleep on signal %s key %s: it already woke a wait", id, key.Signal, key.Key)
			}
		}
	}
	return nil
}

func nativeSleepDue(native *NativeTaskExecution, now int64) bool {
	_, ready := readySignal(native.State, native.Memos)
	return ready || native.State.Until != nil && *native.State.Until <= now
}

// Reserve the wake and its memo transitions together with the running state.
// In particular, consuming a key must survive a crash before its phase runs.
func reserveNativeSleepWake(native *NativeTaskExecution, now int64) {
	state := native.State
	if key, ready := readySignal(state, native.Memos); ready {
		name := signalMemoName(key.Signal, key.Key)
		stored := maps.Clone(generationObject(native.Memos[name].Value))
		stored["consumed"] = true
		native.Memos[name] = &TaskValue{Present: true, Value: stored}
		native.State.Wake = &WakeReason{Reason: "signal", Signal: key.Signal, Key: key.Key}
		return
	}
	if state.Until == nil || *state.Until > now {
		return
	}
	if len(state.Signals) == 0 {
		native.State.Wake = &WakeReason{Reason: "time"}
		return
	}
	native.State.Wake = &WakeReason{Reason: "expired"}
	if native.Memos == nil {
		native.Memos = make(map[string]*TaskValue)
	}
	for _, key := range state.Signals {
		name := signalMemoName(key.Signal, key.Key)
		if native.Memos[name] == nil {
			native.Memos[name] = &TaskValue{Present: true, Value: JSON{"expired": true}}
		}
	}
}
