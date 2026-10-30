package api

import (
	"fmt"
	"net/http"
	"testing"
)

// last is the index of the last non-null value.
func last(vs []*float64) int {
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i] != nil {
			return i
		}
	}
	return -1
}

func TestActivityComparison(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	run := uploadCorpus(t, c, "intervals.fit")
	half := uploadCorpus(t, c, "half_marathon.fit")
	compare := func(query string) ActivityComparison {
		return decodeJSON[ActivityComparison](t, c.do(http.MethodGet,
			fmt.Sprintf("/api/activities/%d/comparison?with=%d%s", run, half, query), nil, nil), http.StatusOK)
	}
	detail := func(id int64) ActivityDetail {
		return decodeJSON[ActivityDetail](t, c.do(http.MethodGet, fmt.Sprintf("/api/activities/%d", id), nil, nil), http.StatusOK)
	}

	got := compare("&axis=distance&points=101")
	if got.Axis != ComparisonAxisDistance || len(got.X) != 101 || got.X[0] != 0 {
		t.Fatalf("axis %s, %d points from %v", got.Axis, len(got.X), got.X[0])
	}
	// The axis ends at the half marathon's distance; the shorter run ends earlier.
	if end, want := got.X[100], *detail(half).DistanceM; end < want-100 || end > want+100 {
		t.Errorf("axis ends at %v m, half marathon %v m", end, want)
	}
	if got.A.HrBpm == nil || got.B.HrBpm == nil || got.A.ElapsedS == nil || got.A.DistanceM == nil {
		t.Fatalf("channels: a %+v", got.A)
	}
	if a, b := last(*got.A.DistanceM), last(*got.B.DistanceM); a >= 100 || b != 100 {
		t.Errorf("last values at %d and %d", a, b)
	}

	// Edits apply: after trimming to 10 minutes, a ends by 600 s.
	editActivity(t, c, run, ActivityEditRequest{Kind: ActivityEditKindTrim, FromS: new(0.0), ToS: new(600.0)})
	got = compare("")
	if got.Axis != ComparisonAxisTime || len(got.X) != defaultComparisonPoints {
		t.Fatalf("defaults: %s, %d points", got.Axis, len(got.X))
	}
	if i := last(*got.A.ElapsedS); i < 0 || *(*got.A.ElapsedS)[i] > 600 || got.X[i] > 601 {
		t.Errorf("trimmed run ends at %v s", got.X[i])
	}
}

func TestActivityComparisonValidation(t *testing.T) {
	t.Parallel()
	c, _, _ := loggedIn(t, "UTC")
	run := uploadCorpus(t, c, "intervals.fit")
	path := fmt.Sprintf("/api/activities/%d/comparison", run)
	for _, q := range []string{"", "with=x", fmt.Sprintf("with=%d&axis=pace", run), fmt.Sprintf("with=%d&points=1", run), fmt.Sprintf("with=%d&points=5001", run)} {
		wantProblem(t, c.do(http.MethodGet, path+"?"+q, nil, nil), KindInvalidRequest)
	}
	wantProblem(t, c.do(http.MethodGet, path+"?with=999999", nil, nil), KindNotFound)

	bob := &apiClient{t: t, h: c.h}
	bob.session(bob.do(http.MethodPost, "/api/auth/register", register("bob"), nil), http.StatusCreated)
	other := uploadCorpus(t, bob, "intervals.fit")
	wantProblem(t, c.do(http.MethodGet, fmt.Sprintf("%s?with=%d", path, other), nil, nil), KindNotFound)
	wantProblem(t, bob.do(http.MethodGet, fmt.Sprintf("%s?with=%d", path, other), nil, nil), KindNotFound)
}
