package workflow

import (
	"fmt"
	"sort"
	"strings"
)

const MaxDependenciesPerJob = 64

type Node struct {
	Ref       string
	DependsOn []string
}

type Resolution struct {
	Order    []int
	Internal map[int][]int
	External map[int][]string
}

type CycleError struct{ Path []string }

func (e *CycleError) Error() string {
	return "dependency cycle: " + strings.Join(e.Path, " -> ")
}

type ValidationError struct {
	Index int
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("job at index %d: %s", e.Index, e.Msg)
}

func Resolve(nodes []Node) (*Resolution, error) {
	refIndex := make(map[string]int, len(nodes))
	for i, n := range nodes {
		if n.Ref == "" {
			continue
		}
		if prev, dup := refIndex[n.Ref]; dup {
			return nil, &ValidationError{Index: i, Msg: fmt.Sprintf("duplicate ref %q, already declared at index %d", n.Ref, prev)}
		}
		refIndex[n.Ref] = i
	}

	res := &Resolution{
		Internal: make(map[int][]int, len(nodes)),
		External: make(map[int][]string, len(nodes)),
	}

	adjacency := make([][]int, len(nodes))
	indegree := make([]int, len(nodes))

	for i, n := range nodes {
		if len(n.DependsOn) > MaxDependenciesPerJob {
			return nil, &ValidationError{Index: i, Msg: fmt.Sprintf("at most %d dependencies allowed, got %d", MaxDependenciesPerJob, len(n.DependsOn))}
		}
		seen := make(map[string]bool, len(n.DependsOn))
		for _, dep := range n.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				return nil, &ValidationError{Index: i, Msg: "dependency reference must not be empty"}
			}
			if seen[dep] {
				continue
			}
			seen[dep] = true

			if j, internal := refIndex[dep]; internal {
				if j == i {
					return nil, &ValidationError{Index: i, Msg: "job cannot depend on itself"}
				}
				adjacency[j] = append(adjacency[j], i)
				indegree[i]++
				res.Internal[i] = append(res.Internal[i], j)
				continue
			}
			res.External[i] = append(res.External[i], dep)
		}
	}

	queue := make([]int, 0, len(nodes))
	for i := range nodes {
		if indegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	sort.Ints(queue)

	order := make([]int, 0, len(nodes))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, next := range adjacency[cur] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(order) != len(nodes) {
		return nil, &CycleError{Path: findCycle(nodes, res.Internal, indegree)}
	}

	res.Order = order
	return res, nil
}

func findCycle(nodes []Node, internal map[int][]int, indegree []int) []string {
	start := -1
	for i := range nodes {
		if indegree[i] > 0 {
			start = i
			break
		}
	}
	if start == -1 {
		return []string{"unknown"}
	}

	label := func(i int) string {
		if nodes[i].Ref != "" {
			return nodes[i].Ref
		}
		return fmt.Sprintf("index:%d", i)
	}

	seen := map[int]int{}
	path := []int{}
	cur := start
	for {
		if pos, ok := seen[cur]; ok {
			loop := path[pos:]
			out := make([]string, 0, len(loop)+1)
			for _, n := range loop {
				out = append(out, label(n))
			}
			return append(out, label(cur))
		}
		seen[cur] = len(path)
		path = append(path, cur)

		next := -1
		for _, dep := range internal[cur] {
			if indegree[dep] > 0 || dep == start {
				next = dep
				break
			}
		}
		if next == -1 {
			return []string{label(cur)}
		}
		cur = next
	}
}
