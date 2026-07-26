package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/git"
	"github.com/glapsfun/gskill/internal/skillslock"
)

// planStubGit is a canned git.Runner keyed by repository URL.
type planStubGit struct {
	tags  map[string][]git.TagRef
	refs  map[string]map[string]string
	errs  map[string]error
	calls atomic.Int64
}

func (s *planStubGit) LsRemoteTags(_ context.Context, url string) ([]git.TagRef, error) {
	s.calls.Add(1)
	if err := s.errs[url]; err != nil {
		return nil, err
	}
	return s.tags[url], nil
}

func (s *planStubGit) LsRemoteHeads(_ context.Context, _ string) ([]git.BranchRef, error) {
	s.calls.Add(1)
	return nil, nil
}

func (s *planStubGit) ResolveRef(_ context.Context, url, ref string) (string, error) {
	s.calls.Add(1)
	if err := s.errs[url]; err != nil {
		return "", err
	}
	if sha, ok := s.refs[url][ref]; ok {
		return sha, nil
	}
	return "", errors.New("unknown ref " + ref)
}

func (s *planStubGit) FetchCommit(_ context.Context, _, _, _ string) error { return nil }

func gitRecord(url string, requested skillslock.Requested, resolved skillslock.Resolved) skillslock.Record {
	return skillslock.Record{
		Source:    skillslock.Source{Type: "git", Original: url, URL: url},
		Requested: requested,
		Resolved:  resolved,
	}
}

// planMatrixProject writes a lockfile covering the full policy matrix and
// returns the root plus a stub runner wired for it.
func planMatrixProject(t *testing.T) (string, *planStubGit) {
	t.Helper()
	root := t.TempDir()
	lf := skillslock.NewState()
	lf.Skills["kubernetes"] = gitRecord("https://example.com/k8s.git",
		skillslock.Requested{Version: "^1.2.0"},
		skillslock.Resolved{RefKind: "semver", Version: "1.2.0", Tag: "v1.2.0", Commit: "aaaa"})
	lf.Skills["security"] = gitRecord("https://example.com/sec.git",
		skillslock.Requested{Version: "^2.0.0"},
		skillslock.Resolved{RefKind: "semver", Version: "2.3.2", Tag: "v2.3.2", Commit: "bbbb"})
	lf.Skills["legacy"] = gitRecord("https://example.com/legacy.git",
		skillslock.Requested{Version: "^1.0.0"},
		skillslock.Resolved{RefKind: "semver", Version: "1.3.0", Tag: "v1.3.0", Commit: "cccc"})
	lf.Skills["deploy"] = gitRecord("https://example.com/deploy.git",
		skillslock.Requested{Ref: "v1.0.0"},
		skillslock.Resolved{RefKind: "tag", Tag: "v1.0.0", Commit: "dddd"})
	lf.Skills["cdep"] = gitRecord("https://example.com/cdep.git",
		skillslock.Requested{Commit: "eeeeeeeeeeeeeeeeeeee"},
		skillslock.Resolved{RefKind: "commit", Commit: "eeeeeeeeeeeeeeeeeeee"})
	lf.Skills["edge"] = gitRecord("https://example.com/edge.git",
		skillslock.Requested{Ref: "main"},
		skillslock.Resolved{RefKind: "branch", Branch: "main", Commit: "ffffffffffffffffffff", MutableRef: true})
	lf.Skills["local-tools"] = skillslock.Record{
		Source:   skillslock.Source{Type: "local", Original: "../tools"},
		Resolved: skillslock.Resolved{RefKind: "local", LocalPathHash: "x"},
	}
	if err := saveLock(filepath.Join(root, skillslock.FileName), lf); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	stub := &planStubGit{
		tags: map[string][]git.TagRef{
			"https://example.com/k8s.git":    {{Name: "v1.2.0", Commit: "aaaa"}, {Name: "v1.4.1", Commit: "aaa2"}},
			"https://example.com/sec.git":    {{Name: "v2.3.2", Commit: "bbbb"}},
			"https://example.com/legacy.git": {{Name: "v1.3.0", Commit: "cccc"}, {Name: "v2.0.0", Commit: "ccc2"}},
			"https://example.com/deploy.git": {{Name: "v1.0.0", Commit: "dddd"}, {Name: "v2.0.0", Commit: "ddd2"}},
		},
		refs: map[string]map[string]string{
			"https://example.com/edge.git": {"main": "11111111111111111111"},
		},
		errs: map[string]error{},
	}
	return root, stub
}

func planApp(runner git.Runner) *App {
	return New(Options{Agents: agent.NewDefaultRegistry(), Git: runner})
}

func itemByName(t *testing.T, plan UpdatePlan, name string) UpdatePlanItem {
	t.Helper()
	for _, it := range plan.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("plan has no item %q: %+v", name, plan.Items)
	return UpdatePlanItem{}
}

func TestPlanUpdate_StatusMatrix(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	plan, err := planApp(stub).PlanUpdate(context.Background(), UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}

	want := map[string]UpdatePlanItem{
		"kubernetes":  {Status: StatusUpdateAvailable, Current: "1.2.0", Candidate: "1.4.1", Policy: "^1.2.0"},
		"security":    {Status: StatusUpToDate, Current: "2.3.2", Policy: "^2.0.0"},
		"legacy":      {Status: StatusNoCompatibleUpdate, Current: "1.3.0", Policy: "^1.0.0", Informational: "2.0.0"},
		"deploy":      {Status: StatusPinnedTag, Current: "v1.0.0", Policy: "tag:v1.0.0", Informational: "2.0.0"},
		"cdep":        {Status: StatusPinnedCommit, Current: "eeeeeeeeeeee", Policy: "commit:eeeeeeeeeeee"},
		"edge":        {Status: StatusUpdateAvailable, Current: "ffffffffffff", Candidate: "111111111111", Policy: "branch:main"},
		"local-tools": {Status: StatusLocalSource, Current: "local", Policy: "local"},
	}
	for name, w := range want {
		got := itemByName(t, plan, name)
		if got.Status != w.Status || got.Current != w.Current || got.Candidate != w.Candidate ||
			got.Policy != w.Policy || got.Informational != w.Informational {
			t.Errorf("%s = %+v,\nwant %+v", name, got, w)
		}
		if !got.Actionable() && got.Reason == "" {
			t.Errorf("%s: non-actionable item must carry a reason", name)
		}
		if got.Actionable() && got.Candidate == "" {
			t.Errorf("%s: actionable item must carry a candidate", name)
		}
	}
}

func TestPlanUpdate_DeterministicNameOrder(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	a := planApp(stub)
	first, err := a.PlanUpdate(context.Background(), UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	second, err := a.PlanUpdate(context.Background(), UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("repeated plans differ:\n%+v\n%+v", first, second)
	}
	want := []string{"cdep", "deploy", "edge", "kubernetes", "legacy", "local-tools", "security"}
	var got []string
	for _, it := range first.Items {
		got = append(got, it.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want name-sorted %v", got, want)
	}
}

func TestPlanUpdate_DiscoveryErrorIsolatedPerSkill(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	stub.errs["https://example.com/k8s.git"] = errors.New("remote unreachable")
	stub.errs["https://example.com/deploy.git"] = errors.New("remote unreachable")

	plan, err := planApp(stub).PlanUpdate(context.Background(), UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate must not fail the whole run on one skill: %v", err)
	}
	k8s := itemByName(t, plan, "kubernetes")
	if k8s.Status != StatusUnknown || k8s.Reason == "" || k8s.DiscoveryErr == "" {
		t.Errorf("kubernetes = %+v, want unknown status carrying the error", k8s)
	}
	// An exact tag pin stays classified as a pin even when the informational
	// newest-tag lookup fails; the failure is recorded (reason + discovery
	// error) so renderers and exit codes can surface it, never dropped.
	dep := itemByName(t, plan, "deploy")
	if dep.Status != StatusPinnedTag || dep.Informational != "" || dep.DiscoveryErr == "" {
		t.Errorf("deploy = %+v, want pinned-tag carrying the failed lookup", dep)
	}
	if !strings.Contains(dep.Reason, "lookup failed") {
		t.Errorf("deploy reason = %q, want the failed lookup recorded", dep.Reason)
	}
	if edge := itemByName(t, plan, "edge"); edge.Status != StatusUpdateAvailable {
		t.Errorf("edge = %+v, other skills must still be classified", edge)
	}
}

func TestPlanUpdate_CancelledContextAborts(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := planApp(stub).PlanUpdate(ctx, UpdatePlanRequest{Root: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled instead of fabricated unknown items", err)
	}
}

func TestPlanUpdate_ActionableAndAnyAvailable(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	plan, err := planApp(stub).PlanUpdate(context.Background(), UpdatePlanRequest{Root: root})
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	var names []string
	for _, it := range plan.Actionable() {
		names = append(names, it.Name)
	}
	if want := []string{"edge", "kubernetes"}; !reflect.DeepEqual(names, want) {
		t.Errorf("actionable = %v, want %v", names, want)
	}
	if !plan.AnyAvailable() {
		t.Error("AnyAvailable = false, want true")
	}
}

func TestPlanUpdate_OfflineSkipsRemoteLookups(t *testing.T) {
	t.Parallel()

	root, stub := planMatrixProject(t)
	plan, err := planApp(stub).PlanUpdate(context.Background(), UpdatePlanRequest{Root: root, Offline: true})
	if err != nil {
		t.Fatalf("PlanUpdate offline: %v", err)
	}
	if n := stub.calls.Load(); n != 0 {
		t.Errorf("offline plan made %d remote calls, want 0", n)
	}
	// Pins and local sources classify without the network; mutable kinds
	// honestly report that they were not checked.
	if it := itemByName(t, plan, "deploy"); it.Status != StatusPinnedTag || it.Informational != "" {
		t.Errorf("deploy offline = %+v, want pinned-tag without informational", it)
	}
	if it := itemByName(t, plan, "cdep"); it.Status != StatusPinnedCommit {
		t.Errorf("cdep offline = %+v, want pinned-commit", it)
	}
	if it := itemByName(t, plan, "local-tools"); it.Status != StatusLocalSource {
		t.Errorf("local-tools offline = %+v, want local-source", it)
	}
	if it := itemByName(t, plan, "kubernetes"); it.Status != StatusUnknown || it.Reason == "" {
		t.Errorf("kubernetes offline = %+v, want unknown with reason", it)
	}
	if plan.AnyAvailable() {
		t.Error("offline plan must not claim actionable updates")
	}
}
