package backup

import (
	"reflect"
	"sort"
	"testing"
)

func run(date, id string, complete bool) []string {
	k := []string{"p/" + date + "/" + id + "/app/goforge-db.dump"}
	if complete {
		k = append(k, "p/"+date+"/"+id+"/manifest.json")
	}
	return k
}

func keys(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	sort.Strings(out)
	return out
}

func TestPruneKeys(t *testing.T) {
	unrelated := []string{"p/README", "p/2026-10-01/notarun/x", "p/manual/2026-01-01/a", "other/2026-10-01/20261001T070000Z/manifest.json"}

	tests := []struct {
		name string
		keys []string
		keep int
		want []string
	}{
		{
			name: "fewer complete days than retention deletes nothing",
			keys: keys(run("2026-10-01", "20261001T070000Z", true), run("2026-10-02", "20261002T070000Z", false), unrelated),
			keep: 7,
			want: nil,
		},
		{
			name: "keeps newest N complete days and deletes older ones",
			keys: keys(
				run("2026-10-01", "20261001T070000Z", true),
				run("2026-10-02", "20261002T070000Z", true),
				run("2026-10-03", "20261003T070000Z", true),
				run("2026-10-04", "20261004T070000Z", true),
				unrelated,
			),
			keep: 2,
			want: keys(run("2026-10-01", "20261001T070000Z", true), run("2026-10-02", "20261002T070000Z", true)),
		},
		{
			name: "failed days do not count toward retention",
			keys: keys(
				run("2026-10-01", "20261001T070000Z", true),
				run("2026-10-02", "20261002T070000Z", true),
				run("2026-10-03", "20261003T070000Z", false),
				run("2026-10-04", "20261004T070000Z", false),
			),
			keep: 2,
			want: nil,
		},
		{
			name: "incomplete days older than the cutoff are deleted, newer kept",
			keys: keys(
				run("2026-10-01", "20261001T070000Z", false),
				run("2026-10-02", "20261002T070000Z", true),
				run("2026-10-03", "20261003T070000Z", false),
				run("2026-10-04", "20261004T070000Z", true),
			),
			keep: 2,
			want: keys(run("2026-10-01", "20261001T070000Z", false)),
		},
		{
			name: "superseded runs within a kept day are deleted, a newer partial is kept",
			keys: keys(
				run("2026-10-04", "20261004T070000Z", true),
				run("2026-10-04", "20261004T150000Z", false),
				run("2026-10-04", "20261004T160000Z", true),
				run("2026-10-04", "20261004T170000Z", false),
			),
			keep: 7,
			want: keys(run("2026-10-04", "20261004T070000Z", true), run("2026-10-04", "20261004T150000Z", false)),
		},
		{
			name: "zero retention deletes nothing",
			keys: keys(run("2026-10-01", "20261001T070000Z", true)),
			keep: 0,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PruneKeys("p/", tt.keys, tt.keep)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PruneKeys() =\n%v\nwant\n%v", got, tt.want)
			}
			for _, k := range got {
				for _, u := range unrelated {
					if k == u {
						t.Errorf("deleted unrelated key %q", k)
					}
				}
			}
		})
	}
}

func TestPruneKeys_EmptyPrefix(t *testing.T) {
	ks := []string{
		"2026-10-01/20261001T070000Z/manifest.json",
		"2026-10-02/20261002T070000Z/manifest.json",
		"unrelated.txt",
	}
	got := PruneKeys("", ks, 1)
	want := []string{"2026-10-01/20261001T070000Z/manifest.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PruneKeys() = %v, want %v", got, want)
	}
}
