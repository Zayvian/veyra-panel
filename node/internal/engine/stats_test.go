package engine

import (
	"reflect"
	"testing"

	"github.com/Zayvian/veyra-node/internal/proto"
)

type usage = map[string]proto.Usage

func u(up, down int64) proto.Usage { return proto.Usage{Up: up, Down: down} }

func wantDelta(t *testing.T, got, want usage) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delta = %v, want %v", got, want)
	}
}

// The ordinary cycle: each report is the change since the last acknowledged one.
func TestDeltaIsMeasuredFromTheAcknowledgedBaseline(t *testing.T) {
	c := newCounters()
	g := c.generation()

	wantDelta(t, c.delta(g, usage{"a": u(10, 20)}), usage{"a": u(10, 20)})
	c.commit()
	wantDelta(t, c.delta(g, usage{"a": u(15, 20)}), usage{"a": u(5, 0)})
}

// A report that never reached the panel is not lost: the next one covers both
// intervals.
func TestAnUnacknowledgedReportIsCoveredByTheNext(t *testing.T) {
	c := newCounters()
	g := c.generation()

	c.delta(g, usage{"a": u(10, 0)}) // sent, never acknowledged
	wantDelta(t, c.delta(g, usage{"a": u(25, 0)}), usage{"a": u(25, 0)})
}

// The defect this bookkeeping exists for: a configuration apply replaces the
// instance, and whatever it counted since the last report must still be
// reported — once.
func TestTrafficOnAReplacedInstanceIsCarriedIntoTheNextReport(t *testing.T) {
	c := newCounters()
	g := c.generation()
	c.delta(g, usage{"a": u(100, 100)})
	c.commit()

	// 30 more bytes up on the old instance, then it is replaced.
	c.retire(usage{"a": u(130, 100), "b": u(0, 7)})
	g = c.generation()

	// The new instance starts from zero and has counted 5 so far.
	wantDelta(t, c.delta(g, usage{"a": u(5, 0)}), usage{"a": u(35, 0), "b": u(0, 7)})
	c.commit()

	// Carry is settled; only the new instance's traffic from here.
	wantDelta(t, c.delta(g, usage{"a": u(9, 0)}), usage{"a": u(4, 0)})
}

// A report in flight while the instance is replaced: its share of the old
// instance's traffic is in the final reading too. Acknowledged, it must not be
// reported again.
func TestAnInFlightReportAcknowledgedAfterRetireIsNotCountedTwice(t *testing.T) {
	c := newCounters()
	g := c.generation()

	wantDelta(t, c.delta(g, usage{"a": u(10, 0)}), usage{"a": u(10, 0)}) // in flight
	c.retire(usage{"a": u(12, 0)})                                     // 2 more before stop
	c.commit()                                                          // the in-flight report landed

	g = c.generation()
	wantDelta(t, c.delta(g, usage{}), usage{"a": u(2, 0)})
	c.commit()
	wantDelta(t, c.delta(g, usage{}), usage{})
}

// The same, but the in-flight report failed: all of it is still owed.
func TestAnInFlightReportThatFailsAcrossRetireIsReportedAgain(t *testing.T) {
	c := newCounters()
	g := c.generation()

	c.delta(g, usage{"a": u(10, 0)}) // in flight, will fail
	c.retire(usage{"a": u(12, 0)})

	g = c.generation()
	wantDelta(t, c.delta(g, usage{"a": u(1, 0)}), usage{"a": u(13, 0)})
	c.commit()
	wantDelta(t, c.delta(g, usage{"a": u(1, 0)}), usage{})
}

// Carry survives any number of rebuilds without being acknowledged.
func TestCarryAccumulatesAcrossSeveralRebuilds(t *testing.T) {
	c := newCounters()
	c.retire(usage{"a": u(3, 0)})
	c.retire(usage{"a": u(4, 1)})

	g := c.generation()
	wantDelta(t, c.delta(g, usage{"a": u(1, 0)}), usage{"a": u(8, 1)})
}

// A reading taken from an instance that was replaced before it was measured
// would be compared against the new instance's empty baseline and counted in
// full a second time. It must be dropped, and acknowledging the empty report
// that results must not settle anything.
func TestAStaleReadingIsIgnored(t *testing.T) {
	c := newCounters()
	old := c.generation()

	c.delta(old, usage{"a": u(10, 0)}) // in flight, will fail
	c.retire(usage{"a": u(10, 0)})

	wantDelta(t, c.delta(old, usage{"a": u(10, 0)}), usage{})
	c.commit()

	wantDelta(t, c.delta(c.generation(), usage{}), usage{"a": u(10, 0)})
}

// A counter that goes backwards inside one instance — a user removed and
// re-added — reports the new value rather than a negative.
func TestACounterThatWentBackwardsReportsItsNewValue(t *testing.T) {
	c := newCounters()
	g := c.generation()
	c.delta(g, usage{"a": u(50, 50)})
	c.commit()
	wantDelta(t, c.delta(g, usage{"a": u(3, 60)}), usage{"a": u(3, 10)})
}

// A retire with no reading — the old instance could not be queried — loses that
// interval but must leave the books consistent.
func TestRetireWithoutAReadingStartsCleanly(t *testing.T) {
	c := newCounters()
	g := c.generation()
	c.delta(g, usage{"a": u(10, 0)})
	c.commit()

	c.retire(nil)
	wantDelta(t, c.delta(c.generation(), usage{"a": u(2, 0)}), usage{"a": u(2, 0)})
}
