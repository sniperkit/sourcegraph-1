// +build pgsqltest

package localstore

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"golang.org/x/net/context"

	"sourcegraph.com/sourcegraph/sourcegraph/api/sourcegraph"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/jsonutil"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/store"
	"sourcegraph.com/sqs/pbtypes"
)

func repoURIs(repos []*sourcegraph.Repo) []string {
	var uris []string
	for _, repo := range repos {
		uris = append(uris, repo.URI)
	}
	sort.Strings(uris)
	return uris
}

func TestRepos_List(t *testing.T) {
	t.Parallel()

	var s repos
	ctx, _, done := testContext()
	defer done()

	want := s.mustCreate(ctx, t, &sourcegraph.Repo{URI: "r"})

	repos, err := s.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonutil.JSONEqual(t, repos, want) {
		t.Errorf("got %v, want %v", repos, want)
	}
}

func TestRepos_List_type(t *testing.T) {
	t.Parallel()

	r1 := &sourcegraph.Repo{URI: "r1", Private: true}
	r2 := &sourcegraph.Repo{URI: "r2"}

	var s repos
	ctx, _, done := testContext()
	defer done()

	s.mustCreate(ctx, t, r1, r2)

	getRepoURIsByType := func(typ string) []string {
		repos, err := s.List(ctx, &sourcegraph.RepoListOptions{Type: typ})
		if err != nil {
			t.Fatal(err)
		}
		uris := make([]string, len(repos))
		for i, repo := range repos {
			uris[i] = repo.URI
		}
		sort.Strings(uris)
		return uris
	}

	if got, want := getRepoURIsByType("private"), []string{"r1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("type %s: got %v, want %v", "enabled", got, want)
	}
	if got, want := getRepoURIsByType("public"), []string{"r2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("type %s: got %v, want %v", "disabled", got, want)
	}
	all := []string{"r1", "r2"}
	if got := getRepoURIsByType("all"); !reflect.DeepEqual(got, all) {
		t.Errorf("type %s: got %v, want %v", "all", got, all)
	}
	if got := getRepoURIsByType(""); !reflect.DeepEqual(got, all) {
		t.Errorf("type %s: got %v, want %v", "empty", got, all)
	}
}

// TestRepos_List_query tests the behavior of Repos.List when called with
// a query.
func TestRepos_List_query(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	// Add some repos.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "abc/def", Name: "def", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "def/ghi", Name: "ghi", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "jkl/mno/pqr", Name: "pqr", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"de", []string{"abc/def", "def/ghi"}},
		{"def", []string{"abc/def", "def/ghi"}},
		{"ABC/DEF", []string{"abc/def"}},
		{"xyz", nil},
	}
	for _, test := range tests {
		repos, err := s.List(ctx, &sourcegraph.RepoListOptions{Query: test.query})
		if err != nil {
			t.Fatal(err)
		}
		if got := repoURIs(repos); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q: got repos %v, want %v", test.query, got, test.want)
		}
	}
}

// TestRepos_List_URIs tests the behavior of Repos.List when called with
// URIs.
func TestRepos_List_URIs(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	// Add some repos.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "c/d", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		uris []string
		want []string
	}{
		{[]string{"a/b"}, []string{"a/b"}},
		{[]string{"x/y"}, nil},
		{[]string{"a/b", "c/d"}, []string{"a/b", "c/d"}},
		{[]string{"a/b", "x/y", "c/d"}, []string{"a/b", "c/d"}},
	}
	for _, test := range tests {
		repos, err := s.List(ctx, &sourcegraph.RepoListOptions{URIs: test.uris})
		if err != nil {
			t.Fatal(err)
		}
		if got := repoURIs(repos); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%v: got repos %v, want %v", test.uris, got, test.want)
		}
	}
}

type RepoGetterMockPublicRepo struct{}

func (r *RepoGetterMockPublicRepo) Get(ctx context.Context, uri string) (*sourcegraph.RemoteRepo, error) {
	return &sourcegraph.RemoteRepo{Private: false}, nil
}

type RepoGetterMockPrivateRepo struct{}

func (r *RepoGetterMockPrivateRepo) Get(ctx context.Context, uri string) (*sourcegraph.RemoteRepo, error) {
	return &sourcegraph.RemoteRepo{Private: true}, nil
}

type RepoGetterMockUnauthorizedRepo struct{}

func (r *RepoGetterMockUnauthorizedRepo) Get(ctx context.Context, uri string) (*sourcegraph.RemoteRepo, error) {
	return nil, grpc.Errorf(codes.Unauthenticated, "%s", "github.Repos.Get")
}

func TestRepos_List_GitHubURIs_PublicRepo(t *testing.T) {
	repoGetter = &RepoGetterMockPublicRepo{}

	ctx, _, done := testContext()
	defer done()

	s := &repos{}

	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	if err := s.Create(ctx, &sourcegraph.Repo{URI: "github.com/public", DefaultBranch: "master", Mirror: true}); err != nil {
		t.Fatal(err)
	}

	repoList, err := s.List(ctx, &sourcegraph.RepoListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a/b"}
	if got := repoURIs(repoList); !reflect.DeepEqual(got, want) {
		t.Fatalf("got repos: %v, want %v", got, want)
	}

	repoList, err = s.List(ctx, &sourcegraph.RepoListOptions{SlowlyIncludePublicGitHubRepos: true})
	if err != nil {
		t.Fatal(err)
	}

	want = []string{"a/b", "github.com/public"}
	if got := repoURIs(repoList); !reflect.DeepEqual(got, want) {
		t.Fatalf("got repos: %v, want %v", got, want)
	}
}

func TestRepos_List_GitHubURIs_PrivateRepo(t *testing.T) {
	repoGetter = &RepoGetterMockPrivateRepo{}

	ctx, _, done := testContext()
	defer done()

	s := &repos{}

	if err := s.Create(ctx, &sourcegraph.Repo{URI: "github.com/private", DefaultBranch: "master", Mirror: true}); err != nil {
		t.Fatal(err)
	}

	repoList, err := s.List(ctx, &sourcegraph.RepoListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := repoURIs(repoList); len(got) != 0 {
		t.Fatal("List should not have returned any repos, got:", got)
	}
}

func TestRepos_List_GithubURIs_UnauthenticatedRepo(t *testing.T) {
	repoGetter = &RepoGetterMockUnauthorizedRepo{}

	ctx, _, done := testContext()
	defer done()

	s := &repos{}

	if err := s.Create(ctx, &sourcegraph.Repo{URI: "github.com/private", DefaultBranch: "master", Mirror: true}); err != nil {
		t.Fatal(err)
	}

	repoList, err := s.List(ctx, &sourcegraph.RepoListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := repoURIs(repoList); len(got) != 0 {
		t.Fatal("List should not have returned any repos, got:", got)
	}

}

func TestRepos_Create(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	tm := time.Now().Round(time.Second)
	ts := pbtypes.NewTimestamp(tm)

	// Add a repo.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", CreatedAt: &ts, DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	repo, err := s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.CreatedAt == nil {
		t.Fatal("got CreatedAt nil")
	}
	if want := ts.Time(); !repo.CreatedAt.Time().Equal(want) {
		t.Errorf("got CreatedAt %q, want %q", repo.CreatedAt.Time(), want)
	}
}

func TestRepos_Create_dupe(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	tm := time.Now().Round(time.Second)
	ts := pbtypes.NewTimestamp(tm)

	// Add a repo.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", CreatedAt: &ts, DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	// Add another repo with the same name.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", CreatedAt: &ts, DefaultBranch: "master"}); err == nil {
		t.Fatalf("got err == nil, want an error when creating a duplicate repo")
	}
}

// TestRepos_Update_Description tests the behavior of Repos.Update to
// update a repo's description.
func TestRepos_Update_Description(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	// Add a repo.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	repo, err := s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if want := ""; repo.Description != want {
		t.Errorf("got description %q, want %q", repo.Description, want)
	}

	if err := s.Update(ctx, store.RepoUpdate{ReposUpdateOp: &sourcegraph.ReposUpdateOp{Repo: sourcegraph.RepoSpec{URI: "a/b"}, Description: "d"}}); err != nil {
		t.Fatal(err)
	}

	repo, err = s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if want := "d"; repo.Description != want {
		t.Errorf("got description %q, want %q", repo.Description, want)
	}
}

func TestRepos_Update_UpdatedAt(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	// Add a repo.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	repo, err := s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.UpdatedAt != nil {
		t.Errorf("got UpdatedAt %v, want nil", repo.UpdatedAt.Time())
	}

	// Perform any update.
	newTime := time.Unix(123456, 0)
	if err := s.Update(ctx, store.RepoUpdate{ReposUpdateOp: &sourcegraph.ReposUpdateOp{Repo: sourcegraph.RepoSpec{URI: "a/b"}}, UpdatedAt: &newTime}); err != nil {
		t.Fatal(err)
	}

	repo, err = s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.UpdatedAt == nil {
		t.Fatal("got UpdatedAt nil, want non-nil")
	}
	if want := newTime; !repo.UpdatedAt.Time().Equal(want) {
		t.Errorf("got UpdatedAt %q, want %q", repo.UpdatedAt.Time(), want)
	}
}

func TestRepos_Update_PushedAt(t *testing.T) {
	t.Parallel()
	ctx, _, done := testContext()
	defer done()

	s := &repos{}
	// Add a repo.
	if err := s.Create(ctx, &sourcegraph.Repo{URI: "a/b", DefaultBranch: "master"}); err != nil {
		t.Fatal(err)
	}

	repo, err := s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.PushedAt != nil {
		t.Errorf("got PushedAt %v, want nil", repo.PushedAt.Time())
	}

	newTime := time.Unix(123456, 0)
	if err := s.Update(ctx, store.RepoUpdate{ReposUpdateOp: &sourcegraph.ReposUpdateOp{Repo: sourcegraph.RepoSpec{URI: "a/b"}}, PushedAt: &newTime}); err != nil {
		t.Fatal(err)
	}

	repo, err = s.Get(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.PushedAt == nil {
		t.Fatal("got PushedAt nil, want non-nil")
	}
	if repo.UpdatedAt != nil {
		t.Fatal("got UpdatedAt non-nil, want nil")
	}
	if want := newTime; !repo.PushedAt.Time().Equal(want) {
		t.Errorf("got PushedAt %q, want %q", repo.PushedAt.Time(), want)
	}
}
