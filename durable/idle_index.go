package durable

import "slices"

// idleScope separates task roots from conversation roots, including global 0.
// The forest retains ended owners as connectors. Background tasks cut only
// their upward edge; conversations they own still have their own busy state.
type idleScope struct {
	id           ID
	conversation bool
}
type idleScopeNode struct {
	key                        idleScope
	parent                     *idleScopeNode
	activeChildren             map[*idleScopeNode]struct{}
	childrenLive, childrenIdle int
	live, idle                 int
}
type idleLiveDocument struct {
	owner ID
	held  bool
}

// idleIndex is Session-line owned and uses only immutable published metadata.
// Space is linear in records, not active-task x ancestor membership sets.
type idleIndex struct {
	image               Snapshot
	nodes               map[idleScope]*idleScopeNode
	generations         map[ID]map[ID]bool
	documents           map[ID]idleLiveDocument
	docCount, heldCount map[ID]int
	ambiguous           int
	invalid             bool
	// Actual work counters, kept private for work-bound regression tests.
	rebuilds, work uint64
	targetVisits   uint64
}

func (x *idleIndex) node(key idleScope) *idleScopeNode {
	n := x.nodes[key]
	if n == nil {
		n = &idleScopeNode{key: key}
		x.nodes[key] = n
	}
	return n
}
func idleParent(state Snapshot, key idleScope) (idleScope, bool) {
	if key.conversation {
		if key.id == 0 {
			return idleScope{}, false
		}
		c := state.Conversations[key.id]
		if c.Owner != 0 {
			return idleScope{id: c.Owner}, true
		}
		return idleScope{conversation: true}, true
	}
	t := state.Tasks[key.id]
	if taskBackground(t) {
		return idleScope{}, false
	}
	if t.Owner != 0 {
		return idleScope{id: t.Owner}, true
	}
	return idleScope{id: t.Conversation, conversation: true}, true
}

func (x *idleIndex) refresh(n *idleScopeNode, hosts map[ID]*TaskRuntime) {
	for n != nil {
		x.work++
		live, idle := n.childrenLive, n.childrenIdle
		if !n.key.conversation {
			t, exists := x.image.Tasks[n.key.id]
			host := hosts[n.key.id] != nil
			if exists && (!terminalStatus(t.Status) || host) {
				live++
				held := t.Kind == "pi.generation" && !terminalStatus(t.Status) && !taskHasDecidedOutcome(t) && !taskAborted(t) && !host && n.childrenLive == 0 && x.heldCount[t.Conversation] != 0
				phase, _ := t.Checkpoint["phase"].(string)
				if !held || phase != "queued" {
					idle++
				}
			}
		}
		dl, di := live-n.live, idle-n.idle
		wasActive := n.idle != 0
		n.live, n.idle = live, idle
		if dl == 0 && di == 0 {
			return
		}
		child := n
		n = n.parent
		if n != nil {
			if wasActive != (idle != 0) {
				idleActiveChild(n, child, idle != 0)
			}
			n.childrenLive += dl
			n.childrenIdle += di
		}
	}
}
func (x *idleIndex) detach(n *idleScopeNode, hosts map[ID]*TaskRuntime) {
	if p := n.parent; p != nil {
		n.parent = nil
		idleActiveChild(p, n, false)
		p.childrenLive -= n.live
		p.childrenIdle -= n.idle
		x.refresh(p, hosts)
	}
}
func (x *idleIndex) attach(n *idleScopeNode, state Snapshot, hosts map[ID]*TaskRuntime) {
	key, attached := idleParent(state, n.key)
	if !attached {
		return
	}
	p := x.nodes[key]
	if p == nil {
		x.invalid = true
		return
	}
	// Legacy storage can contain combined task/conversation cycles that the
	// old predicate handles with a visited set. Never loop or invent a tree.
	for q := p; q != nil; q = q.parent {
		x.work++
		if q == n {
			x.invalid = true
			return
		}
	}
	n.parent = p
	if n.idle != 0 {
		idleActiveChild(p, n, true)
	}
	p.childrenLive += n.live
	p.childrenIdle += n.idle
	x.refresh(p, hosts)
}
func (x *idleIndex) generation(t Task, add bool) {
	phase, _ := t.Checkpoint["phase"].(string)
	// Only these placeholders depend on failedRun. Historical generations and
	// decided/running work must not turn a live-document delta into a history scan.
	if t.Kind != "pi.generation" || terminalStatus(t.Status) || taskHasDecidedOutcome(t) || taskAborted(t) || phase != "queued" {
		return
	}
	if add {
		if x.generations[t.Conversation] == nil {
			x.generations[t.Conversation] = map[ID]bool{}
		}
		x.generations[t.Conversation][t.ID] = true
	} else {
		delete(x.generations[t.Conversation], t.ID)
		if len(x.generations[t.Conversation]) == 0 {
			delete(x.generations, t.Conversation)
		}
	}
}
func (x *idleIndex) docChange(d idleLiveDocument, delta int) {
	ambiguous := func() bool { return x.heldCount[d.owner] > 0 && x.heldCount[d.owner] < x.docCount[d.owner] }
	if ambiguous() {
		x.ambiguous--
	}
	x.docCount[d.owner] += delta
	if d.held {
		x.heldCount[d.owner] += delta
	}
	if ambiguous() {
		x.ambiguous++
	}
	if x.docCount[d.owner] == 0 {
		delete(x.docCount, d.owner)
		delete(x.heldCount, d.owner)
	}
}
func (x *idleIndex) document(d Document) {
	if d.Retired || d.Scope != "conversation" || d.Kind != "pi.live" {
		return
	}
	held, _ := d.Value["failedRun"].(bool)
	v := idleLiveDocument{d.Owner, held}
	x.documents[d.ID] = v
	x.docChange(v, 1)
}
func (s *taskScheduler) rebuildIdleIndex() *idleIndex {
	x := &idleIndex{image: s.h.session.readImage, nodes: map[idleScope]*idleScopeNode{}, generations: map[ID]map[ID]bool{}, documents: map[ID]idleLiveDocument{}, docCount: map[ID]int{}, heldCount: map[ID]int{}, rebuilds: 1}
	if s.idleIndex != nil {
		x.rebuilds += s.idleIndex.rebuilds
		x.work = s.idleIndex.work
		x.targetVisits = s.idleIndex.targetVisits
	}
	x.node(idleScope{conversation: true})
	for id := range x.image.Conversations {
		if id == 0 {
			x.invalid = true
		}
		x.node(idleScope{id: id, conversation: true})
	}
	for id, t := range x.image.Tasks {
		x.node(idleScope{id: id})
		x.generation(t, true)
	}
	for _, d := range x.image.Documents {
		x.document(d)
	}
	for _, n := range x.nodes {
		x.attach(n, x.image, s.invocations)
		if x.invalid {
			break
		}
	}
	if !x.invalid {
		for id := range x.image.Tasks {
			x.refresh(x.nodes[idleScope{id: id}], s.invocations)
		}
	}
	s.idleIndex = x
	return x
}
func (s *taskScheduler) currentIdleIndex() *idleIndex {
	if s.idleIndex == nil || s.idleIndex.image.Seq != s.h.session.readImage.Seq {
		return s.rebuildIdleIndex()
	}
	return s.idleIndex
}
func (s *taskScheduler) refreshIdleHost(id ID) {
	if s.idleIndex == nil {
		return
	}
	x := s.currentIdleIndex()
	if !x.invalid {
		if n := x.nodes[idleScope{id: id}]; n != nil {
			x.refresh(n, s.invocations)
		}
	}
}

// Publication updates happen after receipt adoption but before retention reads.
// Repeated writes use final immutable rows. Changed edges are all detached
// first: otherwise a legal same-batch legacy reparent can form a transient cycle.
func (s *taskScheduler) adoptIdleImage(next Snapshot, writes []Write) {
	x := s.idleIndex
	if x == nil {
		return
	}
	if x.invalid || x.image.Seq+1 != next.Seq {
		s.rebuildIdleIndex()
		return
	}
	changed := map[idleScope]bool{}
	docs := map[ID]bool{}
	for _, w := range writes {
		if w.Task != nil {
			changed[idleScope{id: w.Task.ID}] = true
		}
		if w.Conversation != nil {
			changed[idleScope{id: w.Conversation.ID, conversation: true}] = true
		}
		if w.Document != nil {
			docs[w.Document.ID] = true
		}
		if w.Delta != nil {
			docs[w.Delta.ID] = true
		}
	}
	// An entry-only publication advances the witness without visiting nodes.
	edges := map[idleScope]bool{}
	for key := range changed {
		n := x.node(key)
		parent, attached := idleParent(next, key)
		if attached != (n.parent != nil) || attached && n.parent.key != parent {
			edges[key] = true
		}
	}
	for key := range edges {
		x.detach(x.nodes[key], s.invocations)
	}
	for key := range edges {
		x.attach(x.nodes[key], next, s.invocations)
		if x.invalid {
			break
		}
	}
	old := x.image
	x.image = next
	if x.invalid {
		return
	}
	for key := range changed {
		if !key.conversation {
			x.generation(old.Tasks[key.id], false)
			x.generation(next.Tasks[key.id], true)
		}
	}
	affected := map[ID]bool{}
	for id := range docs {
		if d, exists := x.documents[id]; exists {
			affected[d.owner] = true
			x.docChange(d, -1)
			delete(x.documents, id)
		}
		x.document(next.Documents[id])
		if d, exists := x.documents[id]; exists {
			affected[d.owner] = true
		}
	}
	for key := range changed {
		if !key.conversation {
			x.refresh(x.nodes[key], s.invocations)
		}
	}
	for cid := range affected {
		for id := range x.generations[cid] {
			x.refresh(x.nodes[idleScope{id: id}], s.invocations)
		}
	}
}

func (s *taskScheduler) indexedContextBusy(conversation ID) (bool, bool) {
	x := s.currentIdleIndex()
	// The old failedQueueHeld predicate returns the first matching live doc.
	// Conflicting duplicate kinds have map-order semantics, not one cached bit.
	if x.invalid || x.ambiguous != 0 {
		return false, false
	}
	n := x.nodes[idleScope{id: conversation, conversation: true}]
	return n != nil && n.idle != 0, true
}

// Only idle-relevant branches are retained here. Zero-weight historical leaves
// never enter target traversal, and empty child maps release their allocations.
func idleActiveChild(parent, child *idleScopeNode, active bool) {
	if active {
		if parent.activeChildren == nil {
			parent.activeChildren = map[*idleScopeNode]struct{}{}
		}
		parent.activeChildren[child] = struct{}{}
	} else {
		delete(parent.activeChildren, child)
		if len(parent.activeChildren) == 0 {
			parent.activeChildren = nil
		}
	}
}

func (s *taskScheduler) indexedIdleTargets(conversation ID) ([]ID, bool) {
	x := s.currentIdleIndex()
	if x.invalid || x.ambiguous != 0 {
		return nil, false
	}
	targets := []ID{}
	root := x.nodes[idleScope{id: conversation, conversation: true}]
	if root == nil || root.idle == 0 {
		return targets, true
	}
	// The aggregate is the exact output cardinality. Keep the usual small
	// traversal frontier on the stack; wide scopes can grow it normally.
	targets = make([]ID, 0, root.idle)
	var frontier [16]*idleScopeNode
	stack := append(frontier[:0], root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x.targetVisits++
		if !n.key.conversation && n.idle > n.childrenIdle {
			targets = append(targets, n.key.id)
		}
		for child := range n.activeChildren {
			stack = append(stack, child)
		}
	}
	slices.Sort(targets)
	return targets, true
}

// published is supplied only by a native admitted read or receipted publication.
// Equal sequence numbers alone cannot identify a Snapshot: adapters and private
// transaction candidates can carry different records at the same sequence.
func (s *taskScheduler) idleTargetsForView(state Snapshot, conversation ID, published bool) []ID {
	if published {
		if targets, ok := s.indexedIdleTargets(conversation); ok {
			return targets
		}
	}
	return s.idleTargets(state, conversation)
}
