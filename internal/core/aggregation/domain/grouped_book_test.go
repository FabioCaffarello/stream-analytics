package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
)

func TestGroupLevels_Deterministic(t *testing.T) {
	levels := []domain.Level{
		{Price: 90010, Quantity: 1},
		{Price: 90009, Quantity: 2},
		{Price: 90008, Quantity: 3},
		{Price: 90001, Quantity: 4},
	}
	left := domain.GroupLevels(levels, 10, 25)
	right := domain.GroupLevels(levels, 10, 25)
	assertGroupedEqual(t, left, right)
}

func TestGroupLevels_FloorDivision(t *testing.T) {
	levels := []domain.Level{{Price: 90001, Quantity: 1}}
	got := domain.GroupLevels(levels, 10, 25)
	if len(got) != 1 {
		t.Fatalf("len=%d want=1", len(got))
	}
	if got[0].Price != 90000 {
		t.Fatalf("bucket=%f want=90000", got[0].Price)
	}
}

func TestGroupLevels_MaxRows(t *testing.T) {
	levels := make([]domain.Level, 0, 100)
	for i := 0; i < 100; i++ {
		levels = append(levels, domain.Level{Price: domain.Price(1000 - i), Quantity: 1})
	}
	got := domain.GroupLevels(levels, 1, 25)
	if len(got) != 25 {
		t.Fatalf("len=%d want=25", len(got))
	}
}

func TestGroupLevels_EmptyInput(t *testing.T) {
	got := domain.GroupLevels(nil, 10, 25)
	if len(got) != 0 {
		t.Fatalf("len=%d want=0", len(got))
	}
}

func TestGroupLevels_SingleLevel(t *testing.T) {
	got := domain.GroupLevels([]domain.Level{{Price: 123.45, Quantity: 2}}, 1, 25)
	if len(got) != 1 {
		t.Fatalf("len=%d want=1", len(got))
	}
	if got[0].LevelCount != 1 {
		t.Fatalf("level_count=%d want=1", got[0].LevelCount)
	}
}

func TestGroupLevels_AllSameBucket(t *testing.T) {
	levels := make([]domain.Level, 0, 50)
	for i := 0; i < 50; i++ {
		levels = append(levels, domain.Level{Price: domain.Price(100.0 + float64(i)*0.01), Quantity: 1})
	}
	// Inputs sorted asc for ask side.
	got := domain.GroupLevels(levels, 10, 25)
	if len(got) != 1 {
		t.Fatalf("len=%d want=1", len(got))
	}
	if got[0].LevelCount != 50 {
		t.Fatalf("level_count=%d want=50", got[0].LevelCount)
	}
}

func TestFillCumulative(t *testing.T) {
	grouped := []domain.GroupedLevel{
		{Price: 100, TotalQuantity: 1},
		{Price: 99, TotalQuantity: 2},
		{Price: 98, TotalQuantity: 3},
	}
	domain.FillCumulative(grouped)
	if len(grouped) != 3 {
		t.Fatalf("len=%d want=3", len(grouped))
	}
	if grouped[0].CumulativeQuantity != 1 || grouped[1].CumulativeQuantity != 3 || grouped[2].CumulativeQuantity != 6 {
		t.Fatalf("unexpected cumulative values: %+v", grouped)
	}
}

func TestGroupLevels_MatchesClient(t *testing.T) {
	fixture := []domain.Level{
		{Price: 101.99, Quantity: 1},
		{Price: 101.01, Quantity: 2},
		{Price: 100.99, Quantity: 3},
		{Price: 100.01, Quantity: 4},
	}
	got := domain.GroupLevels(fixture, 1, 10)
	want := []domain.GroupedLevel{
		{Price: 101, TotalQuantity: 3, LevelCount: 2},
		{Price: 100, TotalQuantity: 7, LevelCount: 2},
	}
	assertGroupedEqual(t, got, want)
}

func TestGroupedBook_Determinism_SameInputSameOutput(t *testing.T) {
	levels := make([]domain.Level, 0, 500)
	for i := 0; i < 500; i++ {
		levels = append(levels, domain.Level{Price: domain.Price(5000 - i), Quantity: 1})
	}
	baseline := domain.GroupLevels(levels, 10, 25)
	for i := 0; i < 100; i++ {
		got := domain.GroupLevels(levels, 10, 25)
		assertGroupedEqual(t, baseline, got)
	}
}

func TestGroupedBook_Determinism_MatchesClientLogic(t *testing.T) {
	levels := []domain.Level{
		{Price: 110.9, Quantity: 1},
		{Price: 110.1, Quantity: 2},
		{Price: 109.9, Quantity: 3},
		{Price: 109.1, Quantity: 4},
	}
	got := domain.GroupLevels(levels, 1, 10)
	want := []domain.GroupedLevel{
		{Price: 110, TotalQuantity: 3, LevelCount: 2},
		{Price: 109, TotalQuantity: 7, LevelCount: 2},
	}
	assertGroupedEqual(t, got, want)
}

func TestGroupLevels_Deterministic_ShuffledEqualPrices(t *testing.T) {
	base := []domain.Level{{Price: 100.5, Quantity: 1}, {Price: 100.4, Quantity: 2}, {Price: 100.3, Quantity: 3}}
	want := domain.GroupLevels(base, 1, 25)

	permutations := [][]int{
		{0, 1, 2},
		{0, 2, 1},
		{1, 0, 2},
		{1, 2, 0},
		{2, 0, 1},
		{2, 1, 0},
	}
	for _, perm := range permutations {
		in := []domain.Level{
			base[perm[0]],
			base[perm[1]],
			base[perm[2]],
		}
		// Re-sort deterministically to match precondition expected by GroupLevels.
		sortLevelsByPriceDesc(in)
		got := domain.GroupLevels(in, 1, 25)
		assertGroupedEqual(t, want, got)
	}
}

func sortLevelsByPriceDesc(levels []domain.Level) {
	for i := 1; i < len(levels); i++ {
		j := i
		for j > 0 && levels[j-1].Price < levels[j].Price {
			levels[j-1], levels[j] = levels[j], levels[j-1]
			j--
		}
	}
}

func assertGroupedEqual(t *testing.T, got, want []domain.GroupedLevel) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d got=%+v want=%+v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].Price != want[i].Price || got[i].TotalQuantity != want[i].TotalQuantity || got[i].LevelCount != want[i].LevelCount {
			t.Fatalf("idx=%d got=%+v want=%+v", i, got[i], want[i])
		}
	}
}
