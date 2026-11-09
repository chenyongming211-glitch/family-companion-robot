package conversation

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 11, 8, 23, 50, 0, 0, time.UTC)
var scope = Scope{FamilyID: "f", DeviceID: "d", BindingID: "b", BindingVersion: 1}

func fact(k Kind, minute int, turn int64) Fact {
	return Fact{Kind: k, At: epoch.Add(time.Duration(minute) * time.Minute), TurnID: turn}
}
func source(id string, facts ...Fact) Source {
	return Source{ID: strings.Repeat(id, 32), Scope: scope, StartedAt: epoch, Facts: facts}
}
func reduce(t *testing.T, now int, s ...Source) []Chat {
	t.Helper()
	c, e := Reduce(s, epoch.Add(time.Duration(now)*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestBoundaryAndReconnect(t *testing.T) {
	a := source("a", fact("admit", 0, 1), fact("settle", 1, 1))
	if c := reduce(t, 30, a); len(c) != 1 || c[0].EndedAt != nil {
		t.Fatal(c)
	}
	if c := reduce(t, 31, a); len(c) != 1 || c[0].ArchiveReason != "inactivity" || !c[0].EndedAt.Equal(epoch.Add(31*time.Minute)) {
		t.Fatal(c)
	}
	b := source("b", fact("admit", 30, 1), fact("settle", 30, 1))
	b.StartedAt = epoch.Add(30 * time.Minute)
	if c := reduce(t, 31, b, a); len(c) != 1 || len(c[0].Turns) != 2 || len(c[0].SourceIDs) != 2 {
		t.Fatal(c)
	}
	b.StartedAt = epoch.Add(31 * time.Minute)
	b.Facts = []Fact{fact("admit", 31, 1)}
	if c := reduce(t, 31, a, b); len(c) != 2 {
		t.Fatal(c)
	}
}

func TestLifecycleCases(t *testing.T) {
	tests := []struct {
		name    string
		facts   []Fact
		now     int
		count   int
		pending bool
		reason  string
		last    int
	}{
		{"long pending crash", []Fact{fact("admit", 0, 1)}, 200, 1, true, "", 0},
		{"midnight", []Fact{fact("admit", 0, 1), fact("settle", 20, 1)}, 49, 1, false, "", 20},
		{"overlap", []Fact{fact("admit", 0, 1), fact("admit", 1, 2), fact("settle", 2, 1)}, 99, 1, true, "", 2},
		{"early settlement", []Fact{fact("settle", 0, 1), fact("admit", 10, 1)}, 40, 1, false, "inactivity", 10},
		{"duplicate late", []Fact{fact("admit", 0, 1), fact("settle", 1, 1), fact("settle", 50, 1), fact("admit", 60, 1)}, 99, 1, false, "inactivity", 1},
		{"disconnect", []Fact{fact("admit", 0, 1), {Kind: "close", At: epoch.Add(time.Minute), Reason: "transport_lost"}}, 2, 1, false, "", 1},
		{"explicit stop", []Fact{fact("admit", 0, 1), {Kind: "close", At: epoch.Add(time.Minute), Reason: "client_stop"}}, 2, 1, false, "client_stop", 1},
		{"uncertain", []Fact{fact("admit", 0, 1), fact("settle", 1, 1), fact("uncertain", 2, 0)}, 99, 1, true, "", 1},
		{"late uncertain", []Fact{fact("admit", 0, 1), fact("settle", 1, 1), fact("uncertain", 31, 0)}, 99, 1, false, "inactivity", 1},
		{"empty uncertain", []Fact{fact("uncertain", 0, 0)}, 99, 0, false, "", 0},
		{"uncertain stop", []Fact{fact("admit", 0, 1), fact("uncertain", 1, 0), {Kind: "close", At: epoch.Add(2 * time.Minute), Reason: "client_stop"}}, 99, 1, false, "client_stop", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := reduce(t, tt.now, source("a", tt.facts...))
			if len(c) != tt.count {
				t.Fatal(c)
			}
			if tt.count > 0 && (c[0].Pending != tt.pending || c[0].ArchiveReason != tt.reason || !c[0].LastActivityAt.Equal(epoch.Add(time.Duration(tt.last)*time.Minute))) {
				t.Fatalf("%+v", c[0])
			}
		})
	}
}
func TestCloseSourceMappingAndRestart(t *testing.T) {
	a := source("a", fact("admit", 0, 1), fact("settle", 1, 1), Fact{Kind: "close", At: epoch.Add(45 * time.Minute), Reason: "client_stop"})
	b := source("b", fact("admit", 40, 1))
	b.StartedAt = epoch.Add(40 * time.Minute)
	c := reduce(t, 46, a, b)
	if len(c) != 2 || c[1].EndedAt != nil {
		t.Fatal(c)
	}
	if other := reduce(t, 46, b, a); !reflect.DeepEqual(c, other) {
		t.Fatal("restart/order changed")
	}
	if len(c[0].ID) != 69 || c[0].ID == c[1].ID {
		t.Fatal("IDs")
	}
	empty := source("c", Fact{Kind: "close", At: epoch.Add(46 * time.Minute), Reason: "client_stop"})
	if got := reduce(t, 46, a, b, empty); got[1].EndedAt != nil {
		t.Fatal("old empty source ended new chat")
	}
	empty.StartedAt = epoch.Add(41 * time.Minute)
	if got := reduce(t, 46, a, b, empty); got[1].ArchiveReason != "client_stop" || len(got[1].SourceIDs) != 2 || got[1].SourceIDs[1] != empty.ID {
		t.Fatal(got)
	}
}
func TestScopes(t *testing.T) {
	a := source("a", fact("admit", 0, 1))
	b := source("b", fact("admit", 0, 1))
	b.Scope.BindingVersion++
	c := source("c", fact("admit", 0, 1))
	c.Scope.FamilyID = "other"
	if got := reduce(t, 1, c, b, a); len(got) != 3 || got[0].ID == got[1].ID {
		t.Fatal(got)
	}
}
func TestInvalidAndBudgets(t *testing.T) {
	base := source("a", fact("admit", 0, 1))
	tests := [][]Source{{base, base}, {source("A")}, {source("a", fact("oops", 0, 0))}, {source("a", fact("admit", 0, 0))}, {source("a", fact("admit", 2, 1), fact("settle", 1, 1))}, {source("a", fact("admit", -1, 1))}, {source("a", fact("admit", 100, 1))}, {source("a", fact("close", 0, 0), fact("admit", 1, 1))}, {source("a", fact("uncertain", 0, 1))}}
	bad := base
	bad.Scope.BindingVersion = 0
	tests = append(tests, []Source{bad})
	for _, s := range tests {
		if _, e := Reduce(s, epoch.Add(99*time.Minute)); !errors.Is(e, ErrInvalid) {
			t.Fatal(s, e)
		}
	}
	many := source("a")
	for i := 0; i < 100000; i++ {
		many.Facts = append(many.Facts, fact("settle", 0, 1))
	}
	if _, e := Reduce([]Source{many}, epoch); e != nil {
		t.Fatal(e)
	}
	many.Facts = append(many.Facts, fact("settle", 0, 1))
	if _, e := Reduce([]Source{many}, epoch); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	many.Facts = nil
	for i := int64(1); i <= 10000; i++ {
		many.Facts = append(many.Facts, fact("admit", 0, i))
	}
	if c := reduce(t, 0, many); len(c[0].Turns) != 10000 {
		t.Fatal("turn boundary")
	}
	many.Facts = append(many.Facts, fact("admit", 0, 10001))
	if _, e := Reduce([]Source{many}, epoch); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestFirstCloseFreezesUnknownSettlement(t *testing.T) {
	a := source("a", fact("admit", 0, 1), Fact{Kind: "close", At: epoch.Add(time.Minute), Reason: "client_closed"}, Fact{Kind: "close", At: epoch.Add(20 * time.Minute), Reason: "client_stop"}, fact("settle", 25, 1))
	c := reduce(t, 31, a)
	if c[0].ArchiveReason != "inactivity" || !c[0].LastActivityAt.Equal(epoch.Add(time.Minute)) {
		t.Fatal(c)
	}
}
func TestUncertainSourceAcrossOtherSourceStop(t *testing.T) {
	a := source("a", fact("admit", 0, 1), fact("settle", 0, 1), fact("uncertain", 1, 0), fact("admit", 4, 2), fact("settle", 4, 2), fact("uncertain", 5, 0))
	b := source("b", Fact{Kind: "close", At: epoch.Add(3 * time.Minute), Reason: "client_stop"})
	b.StartedAt = epoch.Add(2 * time.Minute)
	c := reduce(t, 100, a, b)
	if len(c) != 2 || c[0].ArchiveReason != "client_stop" || !c[1].Pending || c[1].EndedAt != nil {
		t.Fatal(c)
	}
}
func TestInputNotMutated(t *testing.T) {
	s := source("a", fact("admit", 0, 1), fact("settle", 1, 1))
	original := append([]Fact(nil), s.Facts...)
	c := reduce(t, 2, s)
	c[0].Turns[0].TurnID = 99
	if !reflect.DeepEqual(s.Facts, original) || reduce(t, 2, s)[0].Turns[0].TurnID != 1 {
		t.Fatal("input/output alias")
	}
}
