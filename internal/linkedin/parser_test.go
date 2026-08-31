package linkedin

import (
	"os"
	"testing"
)

func TestParseProfileView(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("testdata/profile_view.json")
	if err != nil {
		t.Fatal(err)
	}

	result, warnings, err := ParseProfileView("ada-example", body)
	if err != nil {
		t.Fatalf("ParseProfileView() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ParseProfileView() warnings = %v, want none", warnings)
	}
	if result.FullName != "Ada Example" {
		t.Errorf("FullName = %q, want Ada Example", result.FullName)
	}
	if result.ProfileURL != "https://www.linkedin.com/in/ada-example" {
		t.Errorf("ProfileURL = %q", result.ProfileURL)
	}
	if len(result.Images) != 2 || result.Images[1].Width != 400 {
		t.Errorf("Images = %#v, want two image renditions", result.Images)
	}
	if len(result.Experience) != 1 {
		t.Fatalf("Experience count = %d, want grouped company", len(result.Experience))
	}
	if len(result.Experience[0].Positions) != 2 {
		t.Errorf("Position count = %d, want 2", len(result.Experience[0].Positions))
	}
	if !result.Experience[0].Positions[0].DateRange.IsCurrent {
		t.Error("latest position should be current")
	}
	if len(result.Education) != 1 || result.Education[0].FieldOfStudy != "Computer Science" {
		t.Errorf("Education = %#v", result.Education)
	}
	if len(result.Skills) != 2 || result.Skills[0].Endorsements != 12 {
		t.Errorf("Skills = %#v", result.Skills)
	}
	if len(result.Certifications) != 1 || result.Certifications[0].CredentialID != "CERT-123" {
		t.Errorf("Certifications = %#v", result.Certifications)
	}
	if len(result.Languages) != 1 || result.Languages[0].Name != "English" {
		t.Errorf("Languages = %#v", result.Languages)
	}
}

func TestParseDashProfile(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("testdata/dash_profile.json")
	if err != nil {
		t.Fatal(err)
	}

	result, warnings, err := ParseProfileView("ada-example", body)
	if err != nil {
		t.Fatalf("ParseProfileView() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ParseProfileView() warnings = %v, want none", warnings)
	}
	if result.FullName != "Ada Example" || result.Headline != "Distributed Systems Engineer" {
		t.Errorf("identity fields = %#v", result)
	}
	if len(result.Experience) != 1 || len(result.Experience[0].Positions) != 1 {
		t.Fatalf("Experience = %#v", result.Experience)
	}
	position := result.Experience[0].Positions[0]
	if position.Title != "Senior Engineer" || !position.DateRange.IsCurrent {
		t.Errorf("Position = %#v", position)
	}
	if len(result.Education) != 1 || result.Education[0].DegreeName != "BSc" {
		t.Errorf("Education = %#v", result.Education)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "Go" {
		t.Errorf("Skills = %#v", result.Skills)
	}
	if len(result.Certifications) != 1 || result.Certifications[0].IssueDate.Year != 2024 {
		t.Errorf("Certifications = %#v", result.Certifications)
	}
	if len(result.Languages) != 1 || result.Languages[0].Name != "English" {
		t.Errorf("Languages = %#v", result.Languages)
	}
	if len(result.Images) != 1 || result.Images[0].Width != 400 {
		t.Errorf("Images = %#v", result.Images)
	}
}

func TestParseProfileViewMissingSectionsIsPartial(t *testing.T) {
	t.Parallel()
	body := []byte(`{"profile":{"firstName":"Ada","lastName":"Example"}}`)

	_, warnings, err := ParseProfileView("ada-example", body)
	if err != nil {
		t.Fatalf("ParseProfileView() error = %v", err)
	}
	if len(warnings) != 5 {
		t.Fatalf("warning count = %d, want 5", len(warnings))
	}
}

func TestParseProfileViewRejectsUnknownShape(t *testing.T) {
	t.Parallel()
	if _, _, err := ParseProfileView("ada-example", []byte(`{"included":[]}`)); err == nil {
		t.Fatal("ParseProfileView() error = nil, want schema error")
	}
}
