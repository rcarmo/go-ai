package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Direct writer boundary tests, not credit for the Session integration tests.
// Contract: pinned src/session/writer.ts (admit/send/publish/open), especially
// session-group-commit.test.ts:170,414,441,461. Native frames substitute for
// public JS commands so receipt adoption retains its exact-prefix witness.
type writerTestHost struct {
	mu        sync.Mutex
	cause     error
	calls     []int
	published []int
	discarded []int
	failAt    uint64
	own       []uint64
	commit    func(context.Context, []Batch, func(commitReceipt)) (commitGroupResult, error)
}

func (h *writerTestHost) host() sessionWriterHost {
	return sessionWriterHost{
		commit: func(ctx context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
			h.mu.Lock()
			h.calls = append(h.calls, len(b))
			h.mu.Unlock()
			return h.commit(ctx, b, receive)
		},
		publishing: func(_ context.Context, f func()) <-chan struct{} { f(); return nil },
		failure:    func() error { h.mu.Lock(); defer h.mu.Unlock(); return h.cause },
		fail: func(err error, at uint64, own []uint64) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.cause == nil {
				h.cause = err
				h.failAt = at
				h.own = append([]uint64(nil), own...)
			}
		},
		report: func(error, context.Context) {},
		hop:    func() {},
	}
}
func (h *writerTestHost) spec(index uint64, key string, held bool) sessionWriterSpec {
	spec := sessionWriterSpec{index: index, keys: []string{key}, ctx: context.Background(),
		publish: func(r *commitReceipt) *writerPublishFailure {
			h.mu.Lock()
			h.published = append(h.published, int(index))
			h.mu.Unlock()
			return nil
		},
		discard: func(error) { h.mu.Lock(); h.discarded = append(h.discarded, int(index)); h.mu.Unlock() },
	}
	if !held {
		spec.batch = &Batch{Writes: []Write{{Op: "test"}}}
	}
	return spec
}
func writerAwait(t *testing.T, part *sessionWriterPart) error {
	t.Helper()
	select {
	case <-part.outcome:
		return part.err
	case <-time.After(5 * time.Second):
		t.Fatal("writer outcome stranded")
		return nil
	}
}
func writerClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	if ch == nil {
		t.Fatal("missing gate")
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("writer gate stranded")
	}
}
func TestV150WriterBatchesAdmittedPartsAndNewestKeyGate(t *testing.T) {
	h := &writerTestHost{}
	hit, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	seq := uint64(0)
	h.commit = func(ctx context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		if len(h.calls) == 1 {
			close(hit)
			<-release
		}
		if ctx.Err() != nil {
			t.Error("admitted commit retained caller cancellation")
		}
		for i := range b {
			seq++
			receive(commitReceipt{index: i, seq: seq})
		}
		return commitGroupResult{stored: len(b), lastSeq: seq}, nil
	}
	w := newSessionWriter(h.host())
	ctx, cancel := context.WithCancel(context.Background())
	aSpec := h.spec(1, "a", false)
	aSpec.ctx = ctx
	a := w.admit(aSpec)
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("storage not entered")
	}
	cancel()
	b := w.admit(h.spec(2, "shared", false))
	c := w.admit(h.spec(3, "shared", false))
	held := w.admit(h.spec(4, "held", true))
	if w.gate("shared") != c.gate || w.drained() != held.gate || w.unpublishedCount() != 3 {
		t.Fatal("newest writer gates not registered")
	}
	select {
	case <-a.outcome:
		t.Fatal("published before storage settlement")
	default:
	}
	close(release)
	for _, p := range []*sessionWriterPart{a, b, c, held} {
		if err := writerAwait(t, p); err != nil {
			t.Fatal(err)
		}
	}
	writerClosed(t, held.gate)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.calls, []int{1, 2}) || !reflect.DeepEqual(h.published, []int{1, 2, 3, 4}) {
		t.Fatalf("calls=%v published=%v", h.calls, h.published)
	}
	if w.drained() != nil || w.unpublishedCount() != 0 {
		t.Fatal("writer retained settled keys")
	}
}
func TestV150WriterRefusalFencesBeforeOpeningUnpublishedGates(t *testing.T) {
	refusal := errors.New("refused")
	h := &writerTestHost{}
	hit, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	h.commit = func(_ context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		if len(h.calls) == 1 {
			close(hit)
			<-release
			receive(commitReceipt{index: 0, seq: 1})
			return commitGroupResult{stored: 1}, nil
		}
		return commitGroupResult{refused: &commitRefusal{refusal}}, nil
	}
	w := newSessionWriter(h.host())
	a := w.admit(h.spec(1, "a", false))
	<-hit
	b := w.admit(h.spec(2, "b", false))
	c := w.admit(h.spec(3, "c", false))
	close(release)
	if err := writerAwait(t, a); err != nil {
		t.Fatal(err)
	}
	if err := writerAwait(t, b); err != refusal {
		t.Fatalf("refusal outcome %v", err)
	}
	var failed *SessionFailed
	if err := writerAwait(t, c); !errors.As(err, &failed) || !errors.Is(err, refusal) {
		t.Fatalf("later outcome %v", err)
	}
	writerClosed(t, c.gate)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cause != refusal || h.failAt != 2 || !reflect.DeepEqual(h.published, []int{1}) || !reflect.DeepEqual(h.discarded, []int{2, 3}) {
		t.Fatalf("host %+v", h)
	}
}
func TestV150WriterRejectedCallPublishesReceiptsButKeepsOwnErrors(t *testing.T) {
	backend := errors.New("backend failed after receipt")
	h := &writerTestHost{}
	hit, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	secondHit, secondRelease := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-secondRelease:
		default:
			close(secondRelease)
		}
	}()
	h.commit = func(_ context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		if len(h.calls) == 1 {
			close(hit)
			<-release
			receive(commitReceipt{index: 0, seq: 1})
			return commitGroupResult{stored: 1}, nil
		}
		close(secondHit)
		<-secondRelease
		receive(commitReceipt{index: 0, seq: 2})
		return commitGroupResult{}, backend
	}
	w := newSessionWriter(h.host())
	a := w.admit(h.spec(1, "a", false))
	<-hit
	b := w.admit(h.spec(2, "b", false))
	held := w.admit(h.spec(3, "held", true))
	c := w.admit(h.spec(4, "c", false))
	close(release)
	<-secondHit
	later := w.admit(h.spec(5, "later", false))
	close(secondRelease)
	if err := writerAwait(t, a); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*sessionWriterPart{b, held, c} {
		if err := writerAwait(t, p); err != backend {
			t.Fatalf("part %d outcome %v", p.spec.index, err)
		}
	}
	var failed *SessionFailed
	if err := writerAwait(t, later); !errors.As(err, &failed) || !errors.Is(err, backend) {
		t.Fatalf("later outcome %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.published, []int{1, 2, 3}) || !reflect.DeepEqual(h.own, []uint64{2, 3, 4}) || h.failAt != 2 {
		t.Fatalf("published=%v own=%v at=%d", h.published, h.own, h.failAt)
	}
}

// scopeTestHost models a Storage with a deferred publication scope, as the
// pinned host does: the writer asks for the scope and only learns the
// publication ran once the returned channel closes.
type scopeTestHost struct {
	writerTestHost
	log       []string
	inside    []bool
	depth     int
	ran       int
	defer_    func(call int) // called when the scope for part N is entered
	beforeRun func()         // called inside the scope, immediately before it runs the publication
}

func (h *scopeTestHost) host2() sessionWriterHost {
	hh := h.host()
	hh.hop = func() {}
	hh.publishing = func(_ context.Context, run func()) <-chan struct{} {
		done := make(chan struct{})
		h.mu.Lock()
		h.ran++
		call := h.ran
		h.mu.Unlock()
		if h.defer_ != nil {
			h.defer_(call)
		}
		go func() {
			if h.beforeRun != nil {
				h.beforeRun()
			}
			h.mu.Lock()
			h.depth++
			h.log = append(h.log, fmt.Sprintf("open %d", len(h.published)))
			depth := h.depth
			h.mu.Unlock()
			run()
			h.mu.Lock()
			h.inside = append(h.inside, depth == 1)
			h.log = append(h.log, fmt.Sprintf("close %d", len(h.published)))
			h.depth--
			h.mu.Unlock()
			close(done)
		}()
		return done
	}
	return hh
}

func TestV150WriterRunsEachPublicationInsideTheDeferredStorageScope(t *testing.T) {
	h := &scopeTestHost{}
	seq := uint64(0)
	h.commit = func(_ context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		for i := range b {
			seq++
			receive(commitReceipt{index: i, seq: seq})
		}
		return commitGroupResult{stored: len(b), lastSeq: seq}, nil
	}
	w := newSessionWriter(h.host2())
	hit, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	inFlight := true
	base := h.commit
	h.commit = func(ctx context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		if inFlight {
			inFlight = false
			close(hit)
			<-release
		}
		return base(ctx, b, receive)
	}
	a := w.admit(h.spec(1, "k1", false))
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("storage not entered")
	}
	b := w.admit(h.spec(2, "k2", false))
	c := w.admit(h.spec(3, "k3", false))
	close(release)
	for _, p := range []*sessionWriterPart{a, b, c} {
		if err := writerAwait(t, p); err != nil {
			t.Fatal(err)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.log, []string{"open 0", "close 1", "open 1", "close 2", "open 2", "close 3"}) {
		t.Fatalf("scope log=%v", h.log)
	}
	if !reflect.DeepEqual(h.inside, []bool{true, true, true}) {
		t.Fatalf("observers outside the scope: %v", h.inside)
	}
	if !reflect.DeepEqual(h.calls, []int{1, 2}) {
		t.Fatalf("calls=%v", h.calls)
	}
}

// A Storage scope that throws instead of running the publication is reported by
// the host and the publication still runs: the commit is durable, so it must be
// published. This mirrors the pinned host's `#publishing` containment.
func TestV150WriterHostRunsPublicationWhenTheScopeThrows(t *testing.T) {
	scope := errors.New("no scope")
	h := &scopeTestHost{}
	reports := make(chan error, 1)
	h.commit = func(_ context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		for i := range b {
			receive(commitReceipt{index: i, seq: uint64(i + 1)})
		}
		return commitGroupResult{stored: len(b)}, nil
	}
	hh := h.host()
	hh.report = func(err error, _ context.Context) { reports <- err }
	hh.publishing = func(ctx context.Context, run func()) <-chan struct{} {
		defer func() {
			if recovered := recover(); recovered != nil {
				hh.report(scope, ctx)
				run()
			}
		}()
		panic(scope)
	}
	w := newSessionWriter(hh)
	if err := writerAwait(t, w.admit(h.spec(1, "k", false))); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reports:
		if err != scope {
			t.Fatalf("reported %v", err)
		}
	default:
		t.Fatal("throwing scope not reported")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.published, []int{1}) {
		t.Fatalf("published=%v", h.published)
	}
}

// The failure is checked when a deferred scope runs the publication, not when
// the scope was entered: a part whose scope is entered before the Session fails
// must publish nothing.
func TestV150WriterChecksFailureWhenTheDeferredScopeRuns(t *testing.T) {
	failure := errors.New("failed before a deferred publication ran")
	h := &scopeTestHost{}
	h.commit = func(_ context.Context, b []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		for i := range b {
			receive(commitReceipt{index: i, seq: uint64(i + 1)})
		}
		return commitGroupResult{stored: len(b)}, nil
	}
	hh := h.host2()
	// The second part's scope is entered, fails the Session, and only then runs the
	// publication it deferred: the publication must not happen.
	h.beforeRun = func() {
		h.mu.Lock()
		second := h.ran == 2
		h.mu.Unlock()
		if second {
			hh.fail(failure, 2, nil)
		}
	}
	w := newSessionWriter(hh)
	a := w.admit(h.spec(1, "a", false))
	if err := writerAwait(t, a); err != nil {
		t.Fatal(err)
	}
	b := w.admit(h.spec(2, "b", false))
	var failed *SessionFailed
	if err := writerAwait(t, b); !errors.As(err, &failed) || !errors.Is(err, failure) {
		t.Fatalf("second outcome %v", err)
	}
	writerClosed(t, b.gate)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.published, []int{1}) {
		t.Fatalf("published=%v", h.published)
	}
}
