package periodic_test

import (
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
)

// Expected names follow the desktop's localeDatePart implementation, which
// formats each named token separately with Intl.DateTimeFormat.
func TestLocationForLocalizedDailyPaths(t *testing.T) {
	date := time.Date(2026, time.March, 4, 16, 45, 0, 0, time.Local)
	for _, tc := range []struct {
		locale, subpath, title string
	}{
		{"pt-BR", "Journal/2026/março", "quarta-feira, 04 mar. (qua.)"},
		{"fr-FR", "Journal/2026/mars", "mercredi, 04 mars (mer.)"},
		{"de-DE", "Journal/2026/März", "Mittwoch, 04 Mär (Mi)"},
		{"en-US", "Journal/2026/March", "Wednesday, 04 Mar (Wed)"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			settings := vault.VaultSettings{DailyNotes: vault.PeriodicNotes{
				Directory:    "'Journal'/yyyy/MMMM",
				TitlePattern: "EEEE, dd MMM (EEE)",
				Locale:       tc.locale,
			}}
			want := periodic.Location{Subpath: tc.subpath, Title: tc.title}
			if got := periodic.LocationFor(periodic.Daily, date, settings, true); got != want {
				t.Errorf("LocationFor() = %#v, want %#v", got, want)
			}

			// Parse the desktop path independently of LocationFor: regenerating an
			// English path must not let an incorrect implementation round-trip.
			got, ok := periodic.DateOf(periodic.Daily, tc.subpath, tc.title, settings, true)
			wantDate := time.Date(2026, time.March, 4, 0, 0, 0, 0, time.Local)
			if !ok || !got.Equal(wantDate) {
				t.Errorf("DateOf() = %v, %v, want %v, true", got, ok, wantDate)
			}
		})
	}
}

func TestLocationForLocalizedMonthlyPaths(t *testing.T) {
	date := time.Date(2026, time.March, 14, 16, 45, 0, 0, time.Local)
	for _, tc := range []struct {
		locale, subpath, title string
	}{
		{"pt-BR", "Months/2026/dom.", "março (mar.) domingo"},
		{"fr-FR", "Months/2026/dim.", "mars (mars) dimanche"},
		{"de-DE", "Months/2026/So", "März (Mär) Sonntag"},
		{"en-US", "Months/2026/Sun", "March (Mar) Sunday"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			settings := vault.VaultSettings{MonthlyNotes: vault.PeriodicNotes{
				Directory:    "'Months'/yyyy/EEE",
				TitlePattern: "MMMM (MMM) EEEE",
				Locale:       tc.locale,
			}}
			want := periodic.Location{Subpath: tc.subpath, Title: tc.title}
			if got := periodic.LocationFor(periodic.Monthly, date, settings, true); got != want {
				t.Errorf("LocationFor() = %#v, want %#v", got, want)
			}
			got, ok := periodic.DateOf(periodic.Monthly, tc.subpath, tc.title, settings, true)
			wantDate := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.Local)
			if !ok || !got.Equal(wantDate) {
				t.Errorf("DateOf() = %v, %v, want %v, true", got, ok, wantDate)
			}
		})
	}
}

func TestLocalizedWeeklyPathsUseISOYearAndMonday(t *testing.T) {
	date := time.Date(2019, time.January, 1, 16, 45, 0, 0, time.Local)
	for _, tc := range []struct {
		locale, subpath, title string
	}{
		{"pt-BR", "Weeks/2019/dezembro", "2019-W01 segunda-feira 31 dez. (seg.)"},
		{"fr-FR", "Weeks/2019/décembre", "2019-W01 lundi 31 déc. (lun.)"},
		{"de-DE", "Weeks/2019/Dezember", "2019-W01 Montag 31 Dez (Mo)"},
		{"en-US", "Weeks/2019/December", "2019-W01 Monday 31 Dec (Mon)"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			settings := vault.VaultSettings{WeeklyNotes: vault.PeriodicNotes{
				Directory:    "'Weeks'/yyyy/MMMM",
				TitlePattern: "yyyy-'W'ww EEEE dd MMM (EEE)",
				Locale:       tc.locale,
			}}
			want := periodic.Location{Subpath: tc.subpath, Title: tc.title}
			if got := periodic.LocationFor(periodic.Weekly, date, settings, true); got != want {
				t.Errorf("LocationFor() = %#v, want %#v", got, want)
			}
			got, ok := periodic.DateOf(periodic.Weekly, tc.subpath, tc.title, settings, true)
			wantDate := time.Date(2018, time.December, 31, 0, 0, 0, 0, time.Local)
			if !ok || !got.Equal(wantDate) {
				t.Errorf("DateOf() = %v, %v, want %v, true", got, ok, wantDate)
			}
		})
	}
}

func TestDateOfUsesLegacyPatternLocale(t *testing.T) {
	for _, tc := range []struct {
		locale, subpath, title string
	}{
		{"pt-BR", "Journal/2026/março", "quarta-feira, 04 mar."},
		{"fr-FR", "Journal/2026/mars", "mercredi, 04 mars"},
		{"de-DE", "Journal/2026/März", "Mittwoch, 04 Mär"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			settings := vault.VaultSettings{DailyNotes: vault.PeriodicNotes{
				Directory:    "Daily Notes",
				TitlePattern: "yyyy-MM-dd",
				Locale:       "en-US",
				LegacyPatterns: []vault.DateNotePattern{{
					Directory:    "inbox/'Journal'/yyyy/MMMM",
					TitlePattern: "EEEE, dd MMM",
					Locale:       tc.locale,
				}},
			}}
			got, ok := periodic.DateOf(periodic.Daily, tc.subpath, tc.title, settings, false)
			want := time.Date(2026, time.March, 4, 0, 0, 0, 0, time.Local)
			if !ok || !got.Equal(want) {
				t.Errorf("DateOf() = %v, %v, want %v, true", got, ok, want)
			}
		})
	}
}

func TestNumericPeriodicPathsRemainIndependentOfLocale(t *testing.T) {
	date := time.Date(2026, time.March, 4, 16, 45, 0, 0, time.Local)
	for _, locale := range []string{"", "system", "en-US", "pt-BR", "fr-FR", "de-DE"} {
		t.Run("locale="+locale, func(t *testing.T) {
			settings := vault.VaultSettings{
				DailyNotes: vault.PeriodicNotes{
					Directory: "Daily Notes", TitlePattern: "yyyy-MM-dd", Locale: locale,
				},
				WeeklyNotes: vault.PeriodicNotes{
					Directory: "'Weeks'/yyyy", TitlePattern: "yyyy-'W'ww", Locale: locale,
				},
				MonthlyNotes: vault.PeriodicNotes{
					Directory: "'Months'/yyyy", TitlePattern: "yyyy-MM", Locale: locale,
				},
			}
			for _, tc := range []struct {
				kind                   periodic.Kind
				subpath, title, dayISO string
			}{
				{periodic.Daily, "Daily Notes", "2026-03-04", "2026-03-04"},
				{periodic.Weekly, "Weeks/2026", "2026-W10", "2026-03-02"},
				{periodic.Monthly, "Months/2026", "2026-03", "2026-03-01"},
			} {
				t.Run(string(tc.kind), func(t *testing.T) {
					want := periodic.Location{Subpath: tc.subpath, Title: tc.title}
					if got := periodic.LocationFor(tc.kind, date, settings, true); got != want {
						t.Errorf("LocationFor() = %#v, want %#v", got, want)
					}
					got, ok := periodic.DateOf(tc.kind, tc.subpath, tc.title, settings, true)
					if !ok || got.Format("2006-01-02") != tc.dayISO {
						t.Errorf("DateOf() = %v, %v, want %s, true", got, ok, tc.dayISO)
					}
				})
			}
		})
	}
}
