package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestResolveLinearChainOrdersParentsFirst(t *testing.T) {
	nodes := []Node{
		{Ref: "load", DependsOn: []string{"transform"}},
		{Ref: "transform", DependsOn: []string{"extract"}},
		{Ref: "extract"},
	}

	res, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	position := map[int]int{}
	for slot, index := range res.Order {
		position[index] = slot
	}
	if position[2] > position[1] || position[1] > position[0] {
		t.Fatalf("order %v does not run extract before transform before load", res.Order)
	}
	if !slices.Equal(res.Internal[0], []int{1}) {
		t.Fatalf("load depends on %v, want [1]", res.Internal[0])
	}
}

func TestResolveFanOutAndFanIn(t *testing.T) {
	nodes := []Node{
		{Ref: "seed"},
		{Ref: "a", DependsOn: []string{"seed"}},
		{Ref: "b", DependsOn: []string{"seed"}},
		{Ref: "join", DependsOn: []string{"a", "b"}},
	}

	res, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Order) != 4 {
		t.Fatalf("ordered %d nodes, want 4", len(res.Order))
	}
	if got := res.Internal[3]; len(got) != 2 {
		t.Fatalf("join depends on %v, want both a and b", got)
	}
}

func TestResolveSeparatesExternalDependencies(t *testing.T) {
	const existing = "0f8b6c2e-1111-4222-8333-444455556666"
	nodes := []Node{
		{Ref: "first"},
		{Ref: "second", DependsOn: []string{"first", existing}},
	}

	res, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !slices.Equal(res.Internal[1], []int{0}) {
		t.Fatalf("internal deps for second = %v, want [0]", res.Internal[1])
	}
	if !slices.Equal(res.External[1], []string{existing}) {
		t.Fatalf("external deps for second = %v, want [%s]", res.External[1], existing)
	}
}

func TestResolveRejectsCycles(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
	}{
		{"two node cycle", []Node{
			{Ref: "a", DependsOn: []string{"b"}},
			{Ref: "b", DependsOn: []string{"a"}},
		}},
		{"three node cycle", []Node{
			{Ref: "a", DependsOn: []string{"c"}},
			{Ref: "b", DependsOn: []string{"a"}},
			{Ref: "c", DependsOn: []string{"b"}},
		}},
		{"cycle alongside a valid chain", []Node{
			{Ref: "ok"},
			{Ref: "x", DependsOn: []string{"y"}},
			{Ref: "y", DependsOn: []string{"x"}},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.nodes)
			var cycle *CycleError
			if !errors.As(err, &cycle) {
				t.Fatalf("Resolve error = %v, want a CycleError", err)
			}
			if len(cycle.Path) < 2 {
				t.Fatalf("cycle path %v does not name the jobs involved", cycle.Path)
			}
			if !strings.Contains(cycle.Error(), "->") {
				t.Fatalf("cycle message %q does not show the path", cycle.Error())
			}
		})
	}
}

func TestResolveRejectsSelfDependency(t *testing.T) {
	_, err := Resolve([]Node{{Ref: "a", DependsOn: []string{"a"}}})
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Resolve error = %v, want a ValidationError", err)
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Fatalf("error %q does not explain the self dependency", err)
	}
}

func TestResolveRejectsDuplicateRefs(t *testing.T) {
	_, err := Resolve([]Node{{Ref: "dup"}, {Ref: "dup"}})
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Resolve error = %v, want a ValidationError", err)
	}
	if invalid.Index != 1 {
		t.Fatalf("duplicate reported at index %d, want 1", invalid.Index)
	}
}

func TestResolveRejectsEmptyDependencyReference(t *testing.T) {
	_, err := Resolve([]Node{{Ref: "a", DependsOn: []string{"  "}}})
	if err == nil {
		t.Fatal("a blank dependency reference was accepted")
	}
}

func TestResolveRejectsTooManyDependencies(t *testing.T) {
	deps := make([]string, MaxDependenciesPerJob+1)
	for i := range deps {
		deps[i] = fmt.Sprintf("dep-%d", i)
	}
	if _, err := Resolve([]Node{{Ref: "a", DependsOn: deps}}); err == nil {
		t.Fatalf("accepted %d dependencies, want the limit of %d enforced",
			len(deps), MaxDependenciesPerJob)
	}
}

func TestResolveDeduplicatesRepeatedDependencies(t *testing.T) {
	res, err := Resolve([]Node{
		{Ref: "a"},
		{Ref: "b", DependsOn: []string{"a", "a", "a"}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := res.Internal[1]; len(got) != 1 {
		t.Fatalf("repeated dependency recorded %d times, want 1", len(got))
	}
}

func TestResolveAcceptsIndependentJobs(t *testing.T) {
	res, err := Resolve([]Node{{}, {}, {}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Order) != 3 {
		t.Fatalf("ordered %d nodes, want 3", len(res.Order))
	}
	for i := range 3 {
		if len(res.Internal[i]) != 0 || len(res.External[i]) != 0 {
			t.Fatalf("node %d gained dependencies it never declared", i)
		}
	}
}

func TestResolveHandlesEmptyBatch(t *testing.T) {
	res, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve(nil): %v", err)
	}
	if len(res.Order) != 0 {
		t.Fatalf("empty batch produced order %v", res.Order)
	}
}

func TestResolveTreatsUnknownRefsAsExternalIDs(t *testing.T) {
	res, err := Resolve([]Node{{Ref: "a", DependsOn: []string{"not-declared-here"}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !slices.Equal(res.External[0], []string{"not-declared-here"}) {
		t.Fatalf("external deps = %v, want the undeclared reference passed through for id validation",
			res.External[0])
	}
}

func TestResolveScalesToLargeBatches(t *testing.T) {
	const size = 1000
	nodes := make([]Node, size)
	for i := range nodes {
		nodes[i] = Node{Ref: fmt.Sprintf("n%d", i)}
		if i > 0 {
			nodes[i].DependsOn = []string{fmt.Sprintf("n%d", i-1)}
		}
	}

	res, err := Resolve(nodes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for slot, index := range res.Order {
		if slot != index {
			t.Fatalf("chain of %d jobs ordered out of sequence at slot %d", size, slot)
		}
	}
}
